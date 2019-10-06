// Copyright 2012 The Gorilla Authors. All rights reserved.
// Copyright 2019 Waleed AlMalki. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reqtruct

import (
	"fmt"
	"reflect"
)

type LocationError struct {
	Key              string
	AllowedLocations []int
	Location         int
}

func (e LocationError) Error() string {
	return fmt.Sprintf("%q param sent in %s instead of %s", e.Key, locationToName(e.Location), locationsToNames(e.AllowedLocations))
}

type ContentTypeError struct {
	ContentType        string
	RequestContentType string
}

func (e ContentTypeError) Error() string {
	return fmt.Sprintf("Content-Type should be %q instead of %q", e.ContentType, e.RequestContentType)
}

type ParsingError struct {
	WrappedErr error
	Err        error
}

func (e ParsingError) Unwrap() error {
	return e.Err
}

func (e ParsingError) Error() string {
	if e.WrappedErr != nil {
		return fmt.Sprintf("%s. Details: %s", e.Err, e.WrappedErr)
	}
	return e.Err.Error()

}

// ConversionError stores information about a failed conversion.
type ConversionError struct {
	Key   string       // key from the source map.
	Type  reflect.Type // expected type of elem
	Index int          // index for multi-value fields; -1 for single-value fields.
	Err   error        // low-level error (when it exists)
}

func (e ConversionError) Unwrap() error {
	return e.Err
}

func (e ConversionError) Error() string {
	var output string

	if e.Index < 0 {
		output = fmt.Sprintf("error converting value for %q", e.Key)
	} else {
		output = fmt.Sprintf("error converting value for index %d of %q",
			e.Index, e.Key)
	}

	if e.Err != nil {
		output = fmt.Sprintf("%s. Details: %s", output, e.Err)
	}

	return output
}

// UnknownKeyError stores information about an unknown key in the source map.
type UnknownKeyError struct {
	Key string // key from the source map.
}

func (e UnknownKeyError) Error() string {
	return fmt.Sprintf("invalid param %q", e.Key)
}

// MultiError stores multiple decoding errors.
//
// Borrowed from the App Engine SDK.
type MultiError map[string]error

func (e MultiError) Error() string {
	s := ""
	for _, err := range e {
		s = err.Error()
		break
	}
	switch len(e) {
	case 0:
		return "(0 errors)"
	case 1:
		return s
	case 2:
		return s + " (and 1 other error)"
	}
	return fmt.Sprintf("%s (and %d other errors)", s, len(e)-1)
}
