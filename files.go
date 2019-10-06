// Copyright 2019 Waleed AlMalki. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reqtruct

import (
	"reflect"
)

func isFileHeadersPtrs(t reflect.Type) bool {
	return t.Kind() == reflect.Slice &&
		t.Elem().Kind() == reflect.Ptr &&
		t.Elem().Elem().Kind() == reflect.Struct &&
		t.Elem().Elem().PkgPath() == "mime/multipart" &&
		t.Elem().Elem().Name() == "FileHeader"
}

func isFileHeaderPtr(t reflect.Type) bool {
	return t.Kind() == reflect.Ptr &&
		t.Elem().Kind() == reflect.Struct &&
		t.Elem().PkgPath() == "mime/multipart" &&
		t.Elem().Name() == "FileHeader"
}

func isFileHeaders(t reflect.Type) bool {
	return t.Kind() == reflect.Slice &&
		t.Elem().Kind() == reflect.Struct &&
		t.Elem().PkgPath() == "mime/multipart" &&
		t.Elem().Name() == "FileHeader"
}

func isFileHeader(t reflect.Type) bool {
	return t.Kind() == reflect.Struct &&
		t.PkgPath() == "mime/multipart" &&
		t.Name() == "FileHeader"
}

func isFiles(t reflect.Type) bool {
	return t.Kind() == reflect.Slice &&
		t.Elem().Kind() == reflect.Interface &&
		t.Elem().PkgPath() == "mime/multipart" &&
		t.Elem().Name() == "File"
}

func isFile(t reflect.Type) bool {
	return t.Kind() == reflect.Interface &&
		t.PkgPath() == "mime/multipart" &&
		t.Name() == "File"
}
