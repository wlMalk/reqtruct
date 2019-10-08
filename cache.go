// Copyright 2012 The Gorilla Authors. All rights reserved.
// Copyright 2019 Waleed AlMalki. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reqtruct

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

var invalidPath = errors.New("invalid path")

// newCache returns a new cache.
func newCache() *cache {
	c := cache{
		m:               make(map[reflect.Type]*structInfo),
		regconv:         make(map[reflect.Type]Converter),
		sep:             '.',
		defaultLocation: LocationJSON,
	}
	return &c
}

// cache caches meta-data about a struct.
type cache struct {
	l       sync.RWMutex
	m       map[reflect.Type]*structInfo
	regconv map[reflect.Type]Converter

	sepLeft  rune
	sepRight rune
	sep      rune

	defaultLocation int
	nameFunc        func(string, []int) string
}

// registerConverter registers a converter function for a custom type.
func (c *cache) registerConverter(value interface{}, converterFunc Converter) {
	c.regconv[reflect.TypeOf(value)] = converterFunc
}

// splitPath splits a path according to the separators defined in cache
func (c *cache) splitPath(path string) ([]string, error) {
	if c.sepLeft != 0 && c.sepRight != 0 {
		var parts []string
		var runes []rune
		isFirstLevel := true
		opened := false
		for i, r := range path {
			if unicode.IsSpace(r) {
				return nil, invalidPath
			}
			if r == c.sepLeft || r == c.sepRight || r == c.sep {
				if i == 0 || rune(path[i-1]) == r {
					return nil, invalidPath
				}
				if i == len(path)-1 && rune(path[i]) != c.sepRight {
					return nil, invalidPath
				}
				if r == c.sepLeft {
					if !isFirstLevel && c.sep != 0 && rune(path[i-1]) != c.sep {
						return nil, invalidPath
					}
					if len(runes) == 0 && (i != len(path)-2 || (i == len(path)-2 && rune(path[i+1]) != c.sepRight)) {
						return nil, invalidPath
					}
					if opened {
						return nil, invalidPath
					}
					parts = append(parts, string(runes))
					runes = nil
					isFirstLevel = false
					opened = true
				} else if r == c.sepRight {
					if !opened {
						return nil, invalidPath
					}
					if i == len(path)-1 && len(runes) > 0 {
						parts = append(parts, string(runes))
						runes = nil
					}
					opened = false
				} else if r == c.sep {
					if !opened && !isFirstLevel && rune(path[i-1]) != c.sepRight {
						return nil, invalidPath
					}
					if !opened && rune(path[i+1]) != c.sepLeft {
						return nil, invalidPath
					}
					if opened {
						runes = append(runes, r)
					}
				}
			} else {
				if !isFirstLevel && !opened {
					return nil, invalidPath
				}
				runes = append(runes, r)
			}
		}
		if opened {
			return nil, invalidPath
		}
		if len(parts) == 0 {
			parts = append(parts, path)
		}
		return parts, nil
	} else {
		return strings.Split(path, string(c.sep)), nil
	}
}

// parsePath returns "path parts" which contain indices to fields to be used by
// reflect.Value.FieldByName(). Multiple parts are required for slices of
// structs.
func (c *cache) parsePath(p string, t reflect.Type, location int) ([]pathPart, error) {
	var struc *structInfo
	var field *fieldInfo
	var index64 int64
	parts := make([]pathPart, 0)
	path := make([]string, 0)
	keys, err := c.splitPath(p)
	if err != nil {
		return nil, err
	}

	var lastDefinedLocations []int
	for i := 0; i < len(keys); i++ {
		if t.Kind() != reflect.Struct {
			return nil, invalidPath
		}
		if struc = c.get(t); struc == nil {
			return nil, invalidPath
		}
		if field = struc.get(keys[i]); field == nil {
			return nil, invalidPath
		}
		if field.locationsDefined {
			lastDefinedLocations = field.locations
		}
		// Valid field. Append index.
		path = append(path, field.name)
		if field.isSliceOfStructs && !isFileHeadersPtrs(field.typ) && !isFileHeaders(field.typ) && (!field.unmarshalerInfo.IsValid || (field.unmarshalerInfo.IsValid && field.unmarshalerInfo.IsSliceElement)) {
			// Parse a special case: slices of structs.
			// i+1 must be the slice index.
			//
			// Now that struct can implements TextUnmarshaler interface,
			// we don't need to force the struct's fields to appear in the path.
			// So checking i+2 is not necessary anymore.
			i++
			if i+1 > len(keys) {
				return nil, invalidPath
			}
			if index64, err = strconv.ParseInt(keys[i], 10, 0); err != nil {
				return nil, invalidPath
			}
			parts = append(parts, pathPart{
				path:  path,
				field: field,
				index: int(index64),
			})
			path = make([]string, 0)

			// Get the next struct type, dropping ptrs.
			if field.typ.Kind() == reflect.Ptr {
				t = field.typ.Elem()
			} else {
				t = field.typ
			}
			if t.Kind() == reflect.Slice {
				t = t.Elem()
				if t.Kind() == reflect.Ptr {
					t = t.Elem()
				}
			}
		} else if field.typ.Kind() == reflect.Ptr {
			t = field.typ.Elem()
		} else {
			t = field.typ
		}
	}

	// if there are an locations defined seen then use them
	// this makes the field inherit the locations of its parent
	if len(lastDefinedLocations) > 0 {
		if !containsInt(lastDefinedLocations, location) {
			return nil, LocationError{Key: p, AllowedLocations: lastDefinedLocations, Location: location}
		}
	} else {
		if !containsInt(field.locations, location) {
			return nil, LocationError{Key: p, AllowedLocations: field.locations, Location: location}
		}
	}

	// Add the remaining.
	parts = append(parts, pathPart{
		path:  path,
		field: field,
		index: -1,
	})
	return parts, nil
}

// get returns a cached structInfo, creating it if necessary.
func (c *cache) get(t reflect.Type) *structInfo {
	c.l.RLock()
	info := c.m[t]
	c.l.RUnlock()
	if info == nil {
		info = c.create(t, "", nil)
		c.l.Lock()
		c.m[t] = info
		c.l.Unlock()
	}
	return info
}

// create creates a structInfo with meta-data about a struct.
func (c *cache) create(t reflect.Type, parentAlias string, parentLocations []int) *structInfo {
	info := &structInfo{}
	var anonymousInfos []*structInfo
	for i := 0; i < t.NumField(); i++ {
		if f := c.createField(t.Field(i), parentAlias, parentLocations, hasFiles(t)); f != nil {
			info.fields = append(info.fields, f)
			if ft := indirectType(f.typ); ft.Kind() == reflect.Struct && f.isAnonymous {
				anonymousInfos = append(anonymousInfos, c.create(ft, f.canonicalAlias, f.locations))
			}
		}
	}
	for i, a := range anonymousInfos {
		others := []*structInfo{info}
		others = append(others, anonymousInfos[:i]...)
		others = append(others, anonymousInfos[i+1:]...)
		for _, f := range a.fields {
			if !containsAlias(others, f.alias) {
				info.fields = append(info.fields, f)
			}
		}
	}

	info.containsPath = c.containsLocation(info.fields, LocationPath)
	info.containsQuery = c.containsLocation(info.fields, LocationQuery)
	info.containsForm = c.containsLocation(info.fields, LocationForm)
	info.containsFile = c.containsLocation(info.fields, LocationFile)
	info.containsHeader = c.containsLocation(info.fields, LocationHeader)
	info.containsJSON = c.containsLocation(info.fields, LocationJSON)
	info.fieldsJSON = fieldsAliases(getWithLocation(info.fields, LocationJSON))
	return info
}

// createField creates a fieldInfo for the given field.
func (c *cache) createField(field reflect.StructField, parentAlias string, parentLocations []int, parentContainsFiles bool) *fieldInfo {
	alias, locations, locationsDefined := c.fieldAlias(field, parentLocations, parentContainsFiles)
	if alias == "-" {
		// Ignore this field.
		return nil
	}
	canonicalAlias := alias
	if parentAlias != "" {
		canonicalAlias = parentAlias + "." + alias
	}

	// Check if the type is supported and don't cache it if not.
	// First let's get the basic type.
	isSlice, isStruct := false, false

	ft := field.Type

	m := isTextUnmarshaler(reflect.Zero(ft))
	if ft.Kind() == reflect.Ptr {
		ft = ft.Elem()
	}
	if isSlice = ft.Kind() == reflect.Slice; isSlice {
		ft = ft.Elem()
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
	}
	if ft.Kind() == reflect.Array {
		ft = ft.Elem()
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
	}

	isFile := false
	if ft.Kind() == reflect.Interface && ft.Name() == "File" && ft.PkgPath() == "mime/multipart" {
		isFile = true
	}

	if isStruct = ft.Kind() == reflect.Struct; !isStruct && !isFile {
		if c.converter(ft) == nil && builtinConverters[ft.Kind()] == nil {
			// Type is not supported.
			return nil
		}
	} else if !isFile {
		i := c.create(ft, "", nil)
		c.l.Lock()
		c.m[ft] = i
		c.l.Unlock()
	}

	return &fieldInfo{
		typ:              field.Type,
		name:             field.Name,
		alias:            alias,
		locations:        locations,
		locationsDefined: locationsDefined,
		canonicalAlias:   canonicalAlias,
		unmarshalerInfo:  m,
		isSliceOfStructs: isSlice && isStruct,
		isAnonymous:      field.Anonymous,
	}
}

// converter returns the converter for a type.
func (c *cache) converter(t reflect.Type) Converter {
	return c.regconv[t]
}

type structInfo struct {
	containsPath   bool
	containsQuery  bool
	containsHeader bool
	containsForm   bool
	containsFile   bool
	containsJSON   bool

	fieldsJSON []string

	fields []*fieldInfo
}

func (i *structInfo) get(alias string) *fieldInfo {
	for _, field := range i.fields {
		if strings.EqualFold(field.alias, alias) {
			return field
		}
	}
	return nil
}

func containsAlias(infos []*structInfo, alias string) bool {
	for _, info := range infos {
		if info.get(alias) != nil {
			return true
		}
	}
	return false
}

func (c *cache) containsLocation(fields []*fieldInfo, location int) bool {
	for i := range fields {
		if containsInt(fields[i].locations, location) {
			return true
		}

		t := fields[i].typ
		if fields[i].isSliceOfStructs {
			t = t.Elem()
		}
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		c.l.RLock()
		s := c.m[t]
		c.l.RUnlock()
		if s != nil && c.containsLocation(s.fields, location) {
			return true
		}
	}
	return false
}

func getWithLocation(fields []*fieldInfo, locations ...int) (others []*fieldInfo) {
	for i := range fields {
		for j := range locations {
			if containsInt(fields[i].locations, locations[j]) {
				others = append(others, fields[i])
				break
			}
		}
	}
	return
}

func fieldsAliases(fields []*fieldInfo) (aliases []string) {
	for i := range fields {
		if fields[i].canonicalAlias == fields[i].alias {
			aliases = append(aliases, fields[i].alias)
		}
	}
	return
}

type fieldInfo struct {
	typ              reflect.Type
	locations        []int
	locationsDefined bool
	// name is the field name in the struct.
	name  string
	alias string
	// canonicalAlias is almost the same as the alias, but is prefixed with
	// an embedded struct field alias in dotted notation if this field is
	// promoted from the struct.
	// For instance, if the alias is "N" and this field is an embedded field
	// in a struct "X", canonicalAlias will be "X.N".
	canonicalAlias string
	// unmarshalerInfo contains information regarding the
	// encoding.TextUnmarshaler implementation of the field type.
	unmarshalerInfo unmarshaler
	// isSliceOfStructs indicates if the field type is a slice of structs.
	isSliceOfStructs bool
	// isAnonymous indicates whether the field is embedded in the struct.
	isAnonymous bool
}

type pathPart struct {
	field *fieldInfo
	path  []string // path to the field: walks structs using field names.
	index int      // struct index in slices of structs.
}

func indirectType(typ reflect.Type) reflect.Type {
	if typ.Kind() == reflect.Ptr {
		return typ.Elem()
	}
	return typ
}

const (
	locationNone int = iota
	LocationPath
	LocationQuery
	LocationHeader
	LocationForm
	LocationFile
	LocationJSON
)

var locationTags = map[int]string{LocationPath: "path", LocationQuery: "query", LocationHeader: "header", LocationForm: "form", LocationFile: "file", LocationJSON: "json"}
var locationValues = map[string]int{"path": LocationPath, "query": LocationQuery, "header": LocationHeader, "form": LocationForm, "file": LocationFile, "json": LocationJSON}

const (
	fromTag string = "from"
	nameTag string = "name"
)

func containsInt(in []int, i int) bool {
	for _, n := range in {
		if i == n {
			return true
		}
	}
	return false
}

func nameToLocation(name string) int {
	return locationValues[name]
}

func locationToName(location int) string {
	return locationTags[location]
}

func locationsToNames(locations []int) (names []string) {
	names = make([]string, len(locations))
	for i := range locations {
		names[i] = locationToName(locations[i])
	}
	return
}

func (c *cache) getAlias(name string, locations []int) string {
	if c.nameFunc != nil {
		return c.nameFunc(name, locations)
	}
	return name
}

func (c *cache) fieldAlias(field reflect.StructField, parentLocations []int, parentContainsFiles bool) (alias string, locations []int, locationsDefined bool) {

	jsonAllowed := true
	locationsDefined = true

	for _, tagName := range locationTags {
		if tag := parseTag(field.Tag.Get(tagName)); tag != "" && tag != "-" {
			alias = tag
			locations = append(locations, nameToLocation(tagName))
			break
		} else if tag == "-" && tagName == "json" {
			jsonAllowed = false
		} else if tag == "-" {
			return "-", nil, false
		}
	}

	if alias == "" {
		if tag, lTag := field.Tag.Get(nameTag), field.Tag.Get(fromTag); tag != "-" && lTag != "" {
			locs := clean(strings.Split(lTag, ","))
			if len(locs) == 0 && len(parentLocations) > 0 {
				locations = parentLocations
			} else if len(locs) > 0 {
				for _, loc := range locs {
					location := nameToLocation(loc)
					if location != locationNone {
						locations = append(locations, location)
					}
				}
			}
			if len(locations) == 0 && len(parentLocations) == 0 {
				locations = []int{LocationJSON}
			} else if len(locations) == 0 {
				locations = parentLocations
			}
			if tag == "" {
				tag = c.getAlias(field.Name, locations)
			}
			alias = tag
		} else if tag != "-" && lTag == "" {
			if len(parentLocations) > 0 {
				locations = parentLocations
			} else if isFileType(underlyingElem(field.Type)) {
				locations = []int{LocationFile}
			} else if parentContainsFiles {
				locations = []int{LocationForm}
			} else if c.defaultLocation != locationNone {
				if c.defaultLocation == LocationJSON && !jsonAllowed {
					return "-", nil, false
				}
				locations = []int{c.defaultLocation}
				locationsDefined = false
			} else if jsonAllowed {
				locations = []int{LocationJSON}
				locationsDefined = false
			}
			if tag == "" {
				tag = c.getAlias(field.Name, locations)
			}
			alias = tag
		} else if tag == "-" {
			return "-", nil, false
		}
	}

	if field.Anonymous {
		alias = ""
	}

	return
}

func parseTag(tag string) string {
	s := strings.Split(tag, ",")
	return s[0]
}

func hasFiles(t reflect.Type) bool {
	t = underlyingElem(t)
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		f := underlyingElem(t.Field(i).Type)
		if isFileType(f) {
			return true
		}
		if f.Kind() == reflect.Struct && hasFiles(f) {
			return true
		}
	}
	return false
}

func isFileType(t reflect.Type) bool {
	if isFile(t) {
		return true
	}
	if isFileHeader(t) {
		return true
	}
	return false
}

func underlyingElem(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice {
		t = t.Elem()
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
	}
	if t.Kind() == reflect.Array {
		t = t.Elem()
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
	}
	return t
}

func clean(s []string) []string {
	for i := range s {
		s[i] = strings.TrimSpace(s[i])
	}
	return s
}
