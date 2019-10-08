// Copyright 2012 The Gorilla Authors. All rights reserved.
// Copyright 2019 Waleed AlMalki. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reqtruct

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"
)

// NewDecoder returns a new Decoder.
func NewDecoder() *Decoder {
	return &Decoder{cache: newCache(), ignoreUnknownKeys: true, maxMemory: 10 << 20}
}

// Decoder decodes params from a *http.Request to a struct.
type Decoder struct {
	cache             *cache
	zeroEmpty         bool
	ignoreUnknownKeys bool
	maxMemory         int64
	collectErrors     bool
	pathExtractor     func(r *http.Request) map[string]string
}

// ZeroEmpty controls the behaviour when the decoder encounters empty values
// in a map.
// If z is true and a key in the map has the empty string as a value
// then the corresponding struct field is set to the zero value.
// If z is false then empty strings are ignored.
//
// The default value is false, that is empty values do not change
// the value of the struct field.
func (d *Decoder) ZeroEmpty(z bool) {
	d.zeroEmpty = z
}

// DefaultLocation sets the default location to look for params.
// It is only applied if a field does not have location tags.
func (d *Decoder) DefaultLocation(l int) {
	d.cache.defaultLocation = l
}

// IgnoreUnknownKeys controls the behaviour when the decoder encounters unknown
// keys in the map.
// If i is true and an unknown field is encountered, it is ignored. This is
// similar to how unknown keys are handled by encoding/json.
// If i is false then Decode will return an error. Note that any valid keys
// will still be decoded in to the target struct.
func (d *Decoder) IgnoreUnknownKeys(i bool) {
	d.ignoreUnknownKeys = i
}

// Max memory sets the max memory used when parsing multipart forms
func (d *Decoder) MaxMemory(m int64) {
	d.maxMemory = m
}

// PathExtractor defines the mechanism to extract path params from URIs.
// It takes a function that takes a *http.Request and returns map[string]string
func (d *Decoder) PathExtractor(p func(r *http.Request) map[string]string) {
	d.pathExtractor = p
}

// NameFunc allows settings a special function for getting fields aliases
func (d *Decoder) NameFunc(n func(field string, locations []int) string) {
	d.cache.nameFunc = n
}

// Separator defines runes to be used as separators.
// If given '[', ']', '.' for example, then paths should be like a[b].[0].[c]
// This is provided to make it possible to accept serialized objects from jQuery for example.
// Both left and right can be set to 0 if only changing the separator
func (d *Decoder) Separator(left rune, right rune, sep rune) {
	d.cache.sepLeft = left
	d.cache.sepRight = right
	d.cache.sep = sep
}

// RegisterConverter registers a converter function for a custom type.
func (d *Decoder) RegisterConverter(value interface{}, converterFunc Converter) {
	d.cache.registerConverter(value, converterFunc)
}

// CollectErrors specifies whether to return on the first error or accumulate errors.
func (d *Decoder) CollectErrors(c bool) {
	d.collectErrors = c
}

// Decode decodes a *http.Request to a struct.
//
// The first parameter must be a pointer to a struct.
// The second parameter is a pointer to http.Request.
func (d *Decoder) Decode(dst interface{}, r *http.Request) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("interface must be a pointer to struct")
	}
	v = v.Elem()
	t := v.Type()
	var err error
	errors := MultiError{}
	lens := map[reflect.Value]map[int]int{}
	ps := map[string][]pathPart{}
	info := d.cache.get(t)
	var fs map[string][]*multipart.FileHeader
	if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
		if !info.containsPath && !info.containsQuery && !info.containsHeader && !info.containsFile && !info.containsForm && info.containsJSON {
			if err = json.NewDecoder(r.Body).Decode(dst); err != nil {
				return ParsingError{Err: fmt.Errorf("cannot unmarshal JSON"), WrappedErr: err}
			}
			return nil
		}
		if info.containsFile {
			if !isMultipartForm(r) {
				return ContentTypeError{RequestContentType: r.Header.Get("Content-Type"), ContentType: "multipart/form-data"}
			}
			err = r.ParseMultipartForm(d.maxMemory)
			if err != nil {
				return ParsingError{Err: fmt.Errorf("cannot parse multipart form"), WrappedErr: err}
			}
			fs = r.MultipartForm.File
			d.checkFiles(fs, t, ps, errors)
			if !d.collectErrors && len(errors) > 0 {
				return errors
			}
		}
	}

	m, err := d.extractMap(info, t, r, ps, errors)
	if err != nil {
		return err
	}
	if !d.collectErrors && len(errors) > 0 {
		return errors
	}
	d.decodeMaps(t, v, m, fs, ps, lens, errors)
	if len(errors) > 0 {
		return errors
	}
	return nil
}

func isMultipartForm(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
}

func isURLEncodedForm(r *http.Request) bool {
	return r.Header.Get("Content-Type") == "application/x-www-form-urlencoded"
}

func isTextUnmarshaler(v reflect.Value) unmarshaler {
	m := unmarshaler{}
	if m.Unmarshaler, m.IsValid = v.Interface().(encoding.TextUnmarshaler); m.IsValid {
		return m
	}

	if m.Unmarshaler, m.IsValid = reflect.New(v.Type()).Interface().(encoding.TextUnmarshaler); m.IsValid {
		m.IsPtr = true
		return m
	}

	t := v.Type()
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice {
		if m.Unmarshaler, m.IsValid = v.Interface().(encoding.TextUnmarshaler); m.IsValid {
			return m
		}
		m.IsSliceElement = true
		if t = t.Elem(); t.Kind() == reflect.Ptr {
			t = reflect.PtrTo(t.Elem())
			v = reflect.Zero(t)
			m.IsSliceElementPtr = true
			m.Unmarshaler, m.IsValid = v.Interface().(encoding.TextUnmarshaler)
			return m
		}
	}

	v = reflect.New(t)
	m.Unmarshaler, m.IsValid = v.Interface().(encoding.TextUnmarshaler)
	return m
}

type unmarshaler struct {
	Unmarshaler       encoding.TextUnmarshaler
	IsValid           bool
	IsPtr             bool
	IsSliceElement    bool
	IsSliceElementPtr bool
}
