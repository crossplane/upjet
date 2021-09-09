/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lateinit

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/pkg/errors"
)

const (
	// CNameWildcard can be used as the canonical name of a value filter option
	// that will apply to all fields of a struct
	CNameWildcard = ""

	fmtCanonical = "%s.%s"
)

// Option Interface for options that affect late-initialization behavior of a managed resource
type Option interface {
	apply(options *lateInitOptions)
}

// lateInitOptions Contains options for late-initialization processing of a managed resource.
// Initialized in a managed resource's setup method to customize late-initialization behavior for the resource.
type lateInitOptions struct {
	nameMappers  mapperArr
	nameFilters  filterArr
	valueFilters valueFilterArr
}

// apply Store the specified list of `Option`s in the receiver `lateInitOptions`
func (opts *lateInitOptions) apply(opt ...Option) {
	for _, o := range opt {
		o.apply(opts)
	}
}

// ValueFilter defines a filter on CR filed values and types as an `Option`.
// Fields with matching canonical names will not be processed
// during late-initialization.
type valueFilter func(string, reflect.StructField, reflect.Value) bool

// apply Applies the receiver `nameFilter` to the specified `lateInitOptions`
func (filter valueFilter) apply(options *lateInitOptions) {
	options.valueFilters = append(options.valueFilters, filter)
}

func (filter valueFilter) filter(cName string, f reflect.StructField, val reflect.Value) bool {
	if filter == nil {
		return false
	}

	return filter(cName, f, val)
}

func isZeroValueOmitted(tag string) bool {
	for _, p := range strings.Split(tag, ",") {
		if p == "omitempty" {
			return true
		}
	}
	return false
}

// ZeroValueJSONOmitEmptyFilter is a late-initialization option that
// skips initialization of a zero-valued field that has omitempty JSON tag
func ZeroValueJSONOmitEmptyFilter(cName string) Option {
	return valueFilter(func(cn string, f reflect.StructField, v reflect.Value) bool {
		if cName != CNameWildcard && cName != cn {
			return false
		}

		if !isZeroValueOmitted(f.Tag.Get("json")) {
			return false
		}

		k := v.Kind()
		switch {
		case !v.IsValid():
			return false
		case v.IsZero():
			return true
		case k == reflect.Slice && v.Len() == 0:
			return true
		default:
			return false
		}
	})
}

// ZeroElemPtrFilter is a late-initialization option that
// skips initialization of a pointer field with a zero-valued element
func ZeroElemPtrFilter(cName string) Option {
	return valueFilter(func(cn string, f reflect.StructField, v reflect.Value) bool {
		if cName != CNameWildcard && cName != cn {
			return false
		}

		t := v.Type()
		if t.Kind() != reflect.Ptr || v.IsNil() {
			return false
		}
		if v.Elem().IsZero() {
			return true
		}
		return false
	})
}

type valueFilterArr []valueFilter

func (vArr valueFilterArr) filter(name string, sf reflect.StructField, val reflect.Value) bool {
	for _, f := range vArr {
		if f.filter(name, sf, val) {
			return true
		}
	}

	return false
}

// nameFilter defines a filter on CR filed names as an `Option`.
// Fields with matching canonical names will not be processed
// during late-initialization.
type nameFilter func(string) bool

// apply Applies the receiver `nameFilter` to the specified `lateInitOptions`
func (filter nameFilter) apply(options *lateInitOptions) {
	options.nameFilters = append(options.nameFilters, filter)
}

func (filter nameFilter) filter(name string) bool {
	if filter == nil {
		return false
	}

	return filter(name)
}

type filterArr []nameFilter

func (fArr filterArr) filter(name string) bool {
	for _, f := range fArr {
		if f.filter(name) {
			return true
		}
	}

	return false
}

// canonicalNameFilter returns a `nameFilter` option that filters all specified canonical CR field names.
//   Example: `canonicalNameFilter("a.b.c", "a.b.d", "a.b.e")`
func canonicalNameFilter(cNames ...string) nameFilter {
	return func(name string) bool {
		for _, n := range cNames {
			if name == n {
				return true
			}
		}

		return false
	}
}

// NameMapper defines a transformation from CR field names to response field names
type NameMapper func(string) string

// apply applies the receiver `NameMapper` to the specified `lateInitOptions`
func (mapper NameMapper) apply(options *lateInitOptions) {
	options.nameMappers = append(options.nameMappers, mapper)
}

func (mapper NameMapper) getName(name string) string {
	if mapper == nil {
		return name
	}

	return mapper(name)
}

type mapperArr []NameMapper

func (mArr mapperArr) getName(name string) string {
	result := name

	for _, m := range mArr {
		result = m.getName(result)
	}

	return result
}

// suffixReplacer returns a `NameMapper` as a `Option` that
//   can be used to replace the specified `suffix` on a CR field name
//   with the specified `replace` string to obtain the source
//   response field name.
//   Example: `suffixReplacer("ID", "Id")` tells
//   `lateInitializeFromResponse` to fill a target CR field with name `FieldID`
//   from a corresponding response field with name `FieldId`.
func suffixReplacer(suffix, replace string) NameMapper {
	return func(s string) string {
		trimmed := strings.TrimSuffix(s, suffix)

		if trimmed != s {
			return trimmed + replace
		}

		return s
	}
}

// Replacer returns a `NameMapper` as a `Option` that
//   that replaces all occurrences of string `old` to `new` in a
//   target CR field name to obtain the corresponding
//   source response field name.
func Replacer(old, new string) NameMapper {
	return func(s string) string {
		return strings.ReplaceAll(s, old, new)
	}
}

// MapReplacer returns a `NameMapper` as a `Option` that
//   uses the specified `map[string]string` to map from
//   target CR field names to corresponding source response field names.
func MapReplacer(m map[string]string) NameMapper {
	return func(s string) string {
		if result, ok := m[s]; ok {
			return result
		}

		return s
	}
}

// LateInitializeFromResponse Copy unset (nil) values from responseObject to crObject
//   Both crObject and responseObject must be pointers to structs.
//	 Otherwise, an error will be returned. Returns `true` if at least one field has been stored
//   from source `responseObject` into a corresponding field of target `crObject`.
// nolint:gocyclo
func LateInitializeFromResponse(parentName string, crObject interface{}, responseObject interface{},
	opts ...Option) (bool, error) {
	if crObject == nil || reflect.ValueOf(crObject).IsNil() ||
		responseObject == nil || reflect.ValueOf(responseObject).IsNil() {
		return false, nil
	}

	options := &lateInitOptions{}
	// initialize options used during processing of CR and response fields
	options.apply(opts...)

	if options.nameFilters.filter(parentName) {
		return false, nil
	}

	typeOfCrObject, typeOfResponseObject := reflect.TypeOf(crObject), reflect.TypeOf(responseObject)

	if typeOfCrObject.Kind() != reflect.Ptr || typeOfCrObject.Elem().Kind() != reflect.Struct {
		return false, errors.Errorf("crObject must be of a pointer to struct type: %#v", crObject)
	}

	if typeOfResponseObject.Kind() != reflect.Ptr || typeOfResponseObject.Elem().Kind() != reflect.Struct {
		return false, errors.Errorf("responseObject must be of a pointer to struct type: %#v", responseObject)
	}

	valueOfCrObject, valueOfResponseObject := reflect.ValueOf(crObject), reflect.ValueOf(responseObject).Elem()
	fieldAssigned := false

	typeOfCrObject, typeOfResponseObject = typeOfCrObject.Elem(), typeOfResponseObject.Elem()
	valueOfCrObject = valueOfCrObject.Elem()

	for f := 0; f < typeOfCrObject.NumField(); f++ {
		crStructField := typeOfCrObject.Field(f)
		crFieldValue := valueOfCrObject.FieldByName(crStructField.Name)
		cName := getCanonicalName(parentName, crStructField.Name)
		mappedResponseFieldName := options.nameMappers.getName(crStructField.Name)
		responseStructField, ok := typeOfResponseObject.FieldByName(mappedResponseFieldName)

		// check the corresponding field in response struct
		if !ok || options.nameFilters.filter(cName) {
			// corresponding field not found in response
			continue
		}

		responseFieldValue := valueOfResponseObject.FieldByName(mappedResponseFieldName)
		var crFieldInitialized, crKeepField bool
		var err error

		if options.valueFilters.filter(cName, responseStructField, responseFieldValue) {
			// corresponding field value is filtered
			continue
		}

		switch crStructField.Type.Kind() { // nolint:exhaustive
		// handle pointer struct field
		case reflect.Ptr:
			crFieldInitialized = allocate(crFieldValue, func(store, allocTypeValue reflect.Value) {
				store.Set(reflect.New(allocTypeValue.Elem().Type().Elem()))
			})
			crKeepField, err = handlePtr(cName, crFieldInitialized, crFieldValue, responseFieldValue,
				&responseStructField, opts...)

		case reflect.Slice:
			crFieldInitialized = allocate(crFieldValue, func(store, allocTypeValue reflect.Value) {
				store.Set(reflect.MakeSlice(allocTypeValue.Elem().Type(), 0, 0))
			})
			crKeepField, err = handleSlice(cName, crFieldInitialized, crFieldValue,
				responseFieldValue, &responseStructField, opts...)
		}

		if err != nil {
			return false, err
		}

		fieldAssigned = fieldAssigned || crKeepField

		// if no field has been set, de-initialize cr field
		if crFieldInitialized && !crKeepField {
			crFieldValue.Set(reflect.Zero(crStructField.Type))
		}
	}

	return fieldAssigned, nil
}

type allocator func(store, allocTypeValue reflect.Value)

func allocate(crFieldValue reflect.Value, alloc allocator) bool {
	// if nil, initialize with an empty struct
	if crFieldValue.IsNil() {
		v := crFieldValue.Interface()
		pt := reflect.ValueOf(&v).Elem()
		// allocate from heap
		alloc(crFieldValue, pt)

		return true
	}

	return false
}

// nolint:gocyclo
func handlePtr(cName string, crFieldInitialized bool, crFieldValue, responseFieldValue reflect.Value,
	responseStructField *reflect.StructField, opts ...Option) (bool, error) {
	crKeepField := false

	switch {
	// we need the initialization above to be able to check cr field's element type
	case (responseStructField != nil && responseStructField.Type.Kind() != reflect.Ptr) || responseFieldValue.IsNil():
	// noop
	case crFieldValue.Type().Elem().Kind() != responseFieldValue.Type().Elem().Kind():
		return false, errors.Errorf("response field kind %q does not match CR field kind %q",
			responseFieldValue.Type().Elem().Kind().String(), crFieldValue.Type().Elem().Kind().String())

	// if we are dealing with a struct type, recursively check fields
	case responseFieldValue.Elem().Kind() == reflect.Struct:
		if crFieldValue.Kind() == reflect.Ptr && crFieldValue.IsNil() {
			crFieldValue.Set(reflect.New(crFieldValue.Type().Elem()))
		}

		nestedFieldAssigned, err := LateInitializeFromResponse(cName, crFieldValue.Interface(),
			responseFieldValue.Interface(), opts...)

		if err != nil {
			return false, err
		}

		crKeepField = nestedFieldAssigned

	case crFieldInitialized: // then cr object's field is not set but response object contains a value, carry it
		if crFieldValue.Kind() == reflect.Ptr && crFieldValue.IsNil() {
			crFieldValue.Set(reflect.New(crFieldValue.Type().Elem()))
		}

		// initialize new copy from response field
		crFieldValue.Elem().Set(responseFieldValue.Elem())
		crKeepField = true
	}

	return crKeepField, nil
}

// nolint:gocyclo
func handleSlice(cName string, crFieldInitialized bool, crFieldValue, responseFieldValue reflect.Value,
	responseStructField *reflect.StructField, opts ...Option) (bool, error) {
	crKeepField := false

	switch {
	// we need the initialization above to be able to check cr field's slice type
	case responseStructField != nil && responseStructField.Type.Kind() != reflect.Slice:
		return false, errors.Errorf("response field kind %q is not slice for canonical field name: %s",
			responseFieldValue.Type().Kind().String(), cName)

	case responseFieldValue.IsNil():
	// noop
	case crFieldValue.Type().Kind() != responseFieldValue.Type().Kind():
		return false, errors.Errorf("response field kind %q does not match CR field kind %q for canonical field name: %s",
			responseFieldValue.Type().Kind().String(), crFieldValue.Type().Kind().String(), cName)

	case crFieldInitialized || // then cr object's field is not set but response object contains a value, carry it
		(crFieldValue.Type().Elem().Kind() == reflect.Ptr &&
			crFieldValue.Type().Elem().Elem().Kind() == reflect.Struct): // or CR field is a slice of pointer to structs
		// copy slice items from response field
		for i := 0; i < responseFieldValue.Len(); i++ {
			var item reflect.Value
			newItem := false
			// merge existing items with the items in response object at the same indices,
			// otherwise allocate new items for the CR
			if i >= crFieldValue.Len() {
				item = reflect.New(crFieldValue.Type().Elem())
				newItem = true
			} else {
				item = crFieldValue.Index(i).Addr()
			}
			// error from processing the next element of the slice
			var err error
			// check slice item's kind (not slice type)
			switch item.Elem().Kind() { // nolint:exhaustive
			// if dealing with a slice of pointers
			case reflect.Ptr:
				_, err = handlePtr(cName, crFieldInitialized, item.Elem(), responseFieldValue.Index(i), nil,
					opts...)
			// else if dealing with a slice of slices
			case reflect.Slice:
				_, err = handleSlice(cName, crFieldInitialized, item.Elem(), responseFieldValue.Index(i), nil,
					opts...)
			// other slice item types are not supported
			default:
				return false, errors.Errorf("slice items of kind %q is not supported for canonical name: %s",
					item.Elem().Kind().String(), cName)
			}
			// if a type is used at different paths, be sure to define separate filters on corresponding canonical names
			if err != nil {
				return false, err
			}
			// if a new item has been allocated, expand the slice with it
			if newItem {
				crFieldValue.Set(reflect.Append(crFieldValue, item.Elem()))
			}
		}

		crKeepField = true
	}

	return crKeepField, nil
}

func getCanonicalName(parent, child string) string {
	if parent == "" {
		return child
	}

	return fmt.Sprintf(fmtCanonical, parent, child)
}
