// Copyright 2012 The Gorilla Authors. All rights reserved.
// Copyright 2019 Waleed AlMalki. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reqtruct

import (
	"encoding"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"

	"github.com/facette/natsort"
)

func (d *Decoder) decodeMaps(t reflect.Type, v reflect.Value, srcM map[string][]string, srcF map[string][]*multipart.FileHeader, ps map[string][]pathPart, lens map[reflect.Value]map[int]int, errors MultiError) {
	keys := make([]string, len(srcM)+len(srcF))
	i := 0
	for k := range srcM {
		keys[i] = k
		i++
	}
	for k := range srcF {
		keys[i] = k
		i++
	}
	natsort.Sort(keys)
	var err error
	for _, path := range keys {
		parts := ps[path]
		if parts == nil {
			continue
		}
		if m, ok := srcM[path]; ok {
			if err = d.decode(v, path, parts, m, nil, lens); err != nil {
				errors[path] = err
				if !d.collectErrors {
					return
				}
			}
		} else if fs, ok := srcF[path]; ok {
			if err = d.decode(v, path, parts, nil, fs, lens); err != nil {
				errors[path] = err
				if !d.collectErrors {
					return
				}
			}
		}
	}
}

func (d *Decoder) extractMap(info *structInfo, t reflect.Type, r *http.Request, ps map[string][]pathPart, errors MultiError) (map[string][]string, error) {
	m := map[string][]string{}
	var err error
	if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
		if info.containsForm && (isURLEncodedForm(r) || isMultipartForm(r)) {
			if info.containsFile {
				d.merge(m, r.MultipartForm.Value, t, LocationForm, ps, errors)
				if !d.collectErrors && len(errors) > 0 {
					return nil, nil
				}
			} else {
				if !isURLEncodedForm(r) {
					return nil, ContentTypeError{RequestContentType: r.Header.Get("Content-Type"), ContentType: "application/x-www-form-urlencoded"}
				}
				err = r.ParseForm()
				if err != nil {
					return nil, ParsingError{Err: fmt.Errorf("cannot parse form"), WrappedErr: err}
				}
				d.merge(m, r.PostForm, t, LocationForm, ps, errors)
				if !d.collectErrors && len(errors) > 0 {
					return nil, nil
				}
			}
		} else if info.containsJSON && !isURLEncodedForm(r) && !isMultipartForm(r) {
			mm := map[string]interface{}{}
			dec := json.NewDecoder(r.Body)
			err = dec.Decode(&mm)
			if err != nil {
				return nil, ParsingError{Err: fmt.Errorf("cannot unmarshal JSON"), WrappedErr: err}
			}
		loop:
			for k := range mm {
				for _, alias := range info.fieldsJSON {
					if k == alias {
						continue loop
					}
				}
				if !d.ignoreUnknownKeys {
					errors[k] = UnknownKeyError{Key: k}
					if !d.collectErrors {
						return nil, nil
					}
				}
				delete(mm, k)
			}
			d.merge(m, flatten(mm), t, LocationJSON, ps, errors)
			if !d.collectErrors && len(errors) > 0 {
				return nil, nil
			}
		}
	}
	if info.containsHeader {
		d.merge(m, r.Header, t, LocationHeader, ps, errors)
		if !d.collectErrors && len(errors) > 0 {
			return nil, nil
		}
	}
	if info.containsQuery {
		d.merge(m, r.URL.Query(), t, LocationQuery, ps, errors)
		if !d.collectErrors && len(errors) > 0 {
			return nil, nil
		}
	}
	if info.containsPath && d.pathExtractor != nil {
		pathParams := d.pathExtractor(r)
		mm := map[string][]string{}
		for k, v := range pathParams {
			mm[k] = []string{v}
		}
		d.merge(m, mm, t, LocationPath, ps, errors)
		if !d.collectErrors && len(errors) > 0 {
			return nil, nil
		}
	}

	return m, nil
}

func (d *Decoder) checkFiles(m map[string][]*multipart.FileHeader, t reflect.Type, ps map[string][]pathPart, errors MultiError) {
	var parts []pathPart
	var err error
	for k := range m {
		parts, err = d.cache.parsePath(k, t, LocationFile)
		if err == nil {
			ps[k] = parts
		} else if err == invalidPath {
			if !d.ignoreUnknownKeys {
				errors[k] = UnknownKeyError{Key: k}
				if !d.collectErrors {
					return
				}
			}
		} else {
			errors[k] = err
			if !d.collectErrors {
				return
			}
		}
	}
}

func (d *Decoder) merge(m map[string][]string, mm map[string][]string, t reflect.Type, location int, ps map[string][]pathPart, errors MultiError) {
	var parts []pathPart
	var ok bool
	var err error
	var pk string
	for k, v := range mm {
		pk = k
		if rune(k[len(k)-2]) == d.cache.sepLeft && rune(k[len(k)-1]) == d.cache.sepRight {
			k = k[:len(k)-2]
		}
		if _, ok = m[k]; ok {
			m[k] = append(m[k], mm[pk]...)
			continue
		}
		_, ok = ps[k]
		if !ok {
			parts, err = d.cache.parsePath(k, t, location)
			if err == nil {
				if !ok {
					ps[k] = parts
				}
				m[k] = v
			} else if err == invalidPath {
				if !d.ignoreUnknownKeys {
					errors[k] = UnknownKeyError{Key: k}
					if !d.collectErrors {
						return
					}
				}
			} else {
				errors[k] = err
				if !d.collectErrors {
					return
				}
			}
		}
	}
}

func flatten(m map[string]interface{}) map[string][]string {
	mm := make(map[string][]string)
	for k, v := range m {
		switch reflect.TypeOf(v).Kind() {
		case reflect.Map:
			mv := flatten(v.(map[string]interface{}))
			for kk, vv := range mv {
				mm[k+"."+kk] = vv
			}
		case reflect.Array, reflect.Slice:
			for kk, vv := range v.([]interface{}) {
				if reflect.TypeOf(vv).Kind() == reflect.Map {
					mv := flatten(vv.(map[string]interface{}))
					for kkk, vvv := range mv {
						mm[k+"."+fmt.Sprint(kk)+"."+kkk] = vvv
					}
				} else {
					mm[k] = append(mm[k], fmt.Sprint(vv))
				}
			}
		default:
			mm[k] = []string{fmt.Sprint(v)}
		}
	}
	return mm
}

func (d *Decoder) decode(v reflect.Value, path string, parts []pathPart, values []string, fs []*multipart.FileHeader, lens map[reflect.Value]map[int]int) error {
	for _, name := range parts[0].path {
		if v.Type().Kind() == reflect.Ptr {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.FieldByName(name)
	}

	if !v.CanSet() {
		return nil
	}

	t := v.Type()
	if t.Kind() == reflect.Ptr && len(fs) == 0 {
		t = t.Elem()
		if v.IsNil() {
			v.Set(reflect.New(t))
		}
		v = v.Elem()
	}

	if len(parts) > 1 {
		var ok bool
		var idx int
		if _, ok = lens[v]; !ok {
			lens[v] = map[int]int{}
		}
		if idx, ok = lens[v][parts[0].index]; !ok {
			idx = v.Len()
			lens[v][parts[0].index] = idx
			value := reflect.MakeSlice(t, v.Len()+1, v.Len()+1)
			reflect.Copy(value, v)
			v.Set(value)
		}
		return d.decode(v.Index(idx), path, parts[1:], values, fs, lens)
	}

	if len(fs) > 0 {
		if isFileHeadersPtrs(t) {
			v.Set(reflect.ValueOf(fs))
		} else if isFileHeaderPtr(t) {
			fmt.Println(44)
			v.Set(reflect.ValueOf(fs[0]))
		} else if isFileHeaders(t) {
			var files []multipart.FileHeader
			for _, ff := range fs {
				files = append(files, *ff)
			}
			v.Set(reflect.ValueOf(files))
		} else if isFileHeader(t) {
			v.Set(reflect.ValueOf(*fs[0]))
		} else if isFiles(t) {
			var files []multipart.File
			for _, ff := range fs {
				file, err := ff.Open()
				if err != nil {
					for _, cf := range files {
						cf.Close()
					}
					file.Close()
					return err
				}
				files = append(files, file)
			}
			v.Set(reflect.ValueOf(files))
		} else if isFile(t) {
			file, err := fs[0].Open()
			if err != nil {
				file.Close()
				return err
			}
			v.Set(reflect.ValueOf(file))
		}
	} else if len(values) > 0 {
		conv := d.cache.converter(t)
		m := isTextUnmarshaler(v)
		if conv == nil && t.Kind() == reflect.Slice && m.IsSliceElement {
			var items []reflect.Value
			elemT := t.Elem()
			isPtrElem := elemT.Kind() == reflect.Ptr
			if isPtrElem {
				elemT = elemT.Elem()
			}

			conv := d.cache.converter(elemT)
			if conv == nil {
				conv = builtinConverters[elemT.Kind()]
				if conv == nil {
					return fmt.Errorf("converter not found for %v", elemT)
				}
			}

			for key, value := range values {
				if value == "" {
					if d.zeroEmpty {
						items = append(items, reflect.Zero(elemT))
					}
				} else if m.IsValid {
					u := reflect.New(elemT)
					if m.IsSliceElementPtr {
						u = reflect.New(reflect.PtrTo(elemT).Elem())
					}
					if err := u.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(value)); err != nil {
						return ConversionError{
							Key:   path,
							Type:  t,
							Index: key,
							Err:   err,
						}
					}
					if m.IsSliceElementPtr {
						items = append(items, u.Elem().Addr())
					} else if u.Kind() == reflect.Ptr {
						items = append(items, u.Elem())
					} else {
						items = append(items, u)
					}
				} else if item := conv(value); item.IsValid() {
					if isPtrElem {
						ptr := reflect.New(elemT)
						ptr.Elem().Set(item)
						item = ptr
					}
					if item.Type() != elemT && !isPtrElem {
						item = item.Convert(elemT)
					}
					items = append(items, item)
				} else {
					if strings.Contains(value, ",") {
						values := strings.Split(value, ",")
						for _, value := range values {
							if value == "" {
								if d.zeroEmpty {
									items = append(items, reflect.Zero(elemT))
								}
							} else if item := conv(value); item.IsValid() {
								if isPtrElem {
									ptr := reflect.New(elemT)
									ptr.Elem().Set(item)
									item = ptr
								}
								if item.Type() != elemT && !isPtrElem {
									item = item.Convert(elemT)
								}
								items = append(items, item)
							} else {
								return ConversionError{
									Key:   path,
									Type:  elemT,
									Index: key,
								}
							}
						}
					} else {
						return ConversionError{
							Key:   path,
							Type:  elemT,
							Index: key,
						}
					}
				}
			}
			value := reflect.Append(reflect.MakeSlice(t, 0, 0), items...)
			v.Set(value)
		} else {
			val := ""

			if len(values) > 0 {
				val = values[len(values)-1]
			}

			if conv != nil {
				if value := conv(val); value.IsValid() {
					v.Set(value.Convert(t))
				} else {
					return ConversionError{
						Key:   path,
						Type:  t,
						Index: -1,
					}
				}
			} else if m.IsValid {
				if m.IsPtr {
					u := reflect.New(v.Type())
					if err := u.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(val)); err != nil {
						return ConversionError{
							Key:   path,
							Type:  t,
							Index: -1,
							Err:   err,
						}
					}
					v.Set(reflect.Indirect(u))
				} else {
					if err := m.Unmarshaler.UnmarshalText([]byte(val)); err != nil {
						return ConversionError{
							Key:   path,
							Type:  t,
							Index: -1,
							Err:   err,
						}
					}
				}
			} else if val == "" {
				if d.zeroEmpty {
					v.Set(reflect.Zero(t))
				}
			} else if conv := builtinConverters[t.Kind()]; conv != nil {
				if value := conv(val); value.IsValid() {
					v.Set(value.Convert(t))
				} else {
					return ConversionError{
						Key:   path,
						Type:  t,
						Index: -1,
					}
				}
			} else {
				return fmt.Errorf("converter not found for %v", t)
			}
		}
	}
	return nil
}
