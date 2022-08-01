/*
Copyright 2021 Upbound Inc.
*/

package resource

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"

	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/upbound/upjet/pkg/config"
)

const (
	// AnnotationKeyPrivateRawAttribute is the key that points to private attribute
	// of the Terraform State. It's non-sensitive and used by provider to store
	// arbitrary metadata, usually details about schema version.
	AnnotationKeyPrivateRawAttribute = "upjet.crossplane.io/provider-meta"

	// AnnotationKeyTestResource is used for marking an MR as test for automated tests
	AnnotationKeyTestResource = "upjet.upbound.io/test"

	// CNameWildcard can be used as the canonical name of a value filter option
	// that will apply to all fields of a struct
	CNameWildcard = ""
)
const (
	// error messages
	errFmtTypeMismatch        = "observed object's type %q does not match desired object's type %q"
	errFmtPanic               = "recovered from panic: %v\n%s"
	errFmtMapElemNotSupported = "map items of kind %q is not supported for canonical name: %s"
	errFmtNotPtrToStruct      = "%s must be of a pointer to struct type: %#v"

	fmtCanonical = "%s.%s"
)

// GenericLateInitializer performs late-initialization of a Terraformed resource.
type GenericLateInitializer struct {
	valueFilters []ValueFilter
	nameFilters  []NameFilter
}

// SetCriticalAnnotations sets the critical annotations of the resource and reports
// whether there has been a change.
func SetCriticalAnnotations(tr metav1.Object, cfg *config.Resource, tfstate map[string]any, privateRaw string) (bool, error) {
	name, err := cfg.ExternalName.GetExternalNameFn(tfstate)
	if err != nil {
		return false, errors.Wrap(err, "cannot get external name")
	}
	if tr.GetAnnotations()[AnnotationKeyPrivateRawAttribute] == privateRaw &&
		tr.GetAnnotations()[xpmeta.AnnotationKeyExternalName] == name {
		return false, nil
	}
	xpmeta.AddAnnotations(tr, map[string]string{
		AnnotationKeyPrivateRawAttribute: privateRaw,
		xpmeta.AnnotationKeyExternalName: name,
	})
	return true, nil
}

// GenericLateInitializerOption are options that control the late-initialization
// behavior of a Terraformed resource.
type GenericLateInitializerOption func(l *GenericLateInitializer)

// NewGenericLateInitializer constructs a new GenericLateInitializer
// with the supplied options
func NewGenericLateInitializer(opts ...GenericLateInitializerOption) *GenericLateInitializer {
	l := &GenericLateInitializer{}
	for _, o := range opts {
		o(l)
	}
	return l
}

// NameFilter defines a late-initialization filter on CR field canonical names.
// Fields with matching cnames will not be processed during late-initialization
type NameFilter func(string) bool

// WithNameFilter returns a GenericLateInitializer that causes to
// skip initialization of the field with the specified canonical name
func WithNameFilter(cname string) GenericLateInitializerOption {
	return func(l *GenericLateInitializer) {
		l.nameFilters = append(l.nameFilters, nameFilter(cname))
	}
}

func nameFilter(cname string) NameFilter {
	return func(s string) bool {
		return cname == CNameWildcard || s == cname
	}
}

// ValueFilter defines a late-initialization filter on CR field values.
// Fields with matching values will not be processed during late-initialization
type ValueFilter func(string, reflect.StructField, reflect.Value) bool

// WithZeroValueJSONOmitEmptyFilter returns a GenericLateInitializerOption that causes to
// skip initialization of a zero-valued field that has omitempty JSON tag
func WithZeroValueJSONOmitEmptyFilter(cName string) GenericLateInitializerOption {
	return func(l *GenericLateInitializer) {
		l.valueFilters = append(l.valueFilters, zeroValueJSONOmitEmptyFilter(cName))
	}
}

// zeroValueJSONOmitEmptyFilter is a late-initialization ValueFilter that
// skips initialization of a zero-valued field that has omitempty JSON tag
// nolint:gocyclo
func zeroValueJSONOmitEmptyFilter(cName string) ValueFilter {
	return func(cn string, f reflect.StructField, v reflect.Value) bool {
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
		case (k == reflect.Slice || k == reflect.Map) && v.Len() == 0:
			return true
		case k == reflect.Ptr && v.Elem().IsZero():
			return true
		default:
			return false
		}
	}
}

// WithZeroElemPtrFilter returns a GenericLateInitializerOption that causes to
// skip initialization of a pointer field with a zero-valued element
func WithZeroElemPtrFilter(cName string) GenericLateInitializerOption {
	return func(l *GenericLateInitializer) {
		l.valueFilters = append(l.valueFilters, zeroElemPtrFilter(cName))
	}
}

// zeroElemPtrFilter is a late-initialization ValueFilter that
// skips initialization of a pointer field with a zero-valued element
func zeroElemPtrFilter(cName string) ValueFilter {
	return func(cn string, f reflect.StructField, v reflect.Value) bool {
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
	}
}

func isZeroValueOmitted(tag string) bool {
	for _, p := range strings.Split(tag, ",") {
		if p == "omitempty" {
			return true
		}
	}
	return false
}

// LateInitialize Copy unset (nil) values from responseObject to crObject
// Both crObject and responseObject must be pointers to structs.
// Otherwise, an error will be returned. Returns `true` if at least one field has been stored
// from source `responseObject` into a corresponding field of target `crObject`.
// nolint:gocyclo
func (li *GenericLateInitializer) LateInitialize(desiredObject, observedObject any) (changed bool, err error) {
	if desiredObject == nil || reflect.ValueOf(desiredObject).IsNil() ||
		observedObject == nil || reflect.ValueOf(observedObject).IsNil() {
		return false, nil
	}

	typeOfDesiredObject, typeOfObservedObject := reflect.TypeOf(desiredObject), reflect.TypeOf(observedObject)
	if typeOfDesiredObject.Kind() != reflect.Ptr || typeOfDesiredObject.Elem().Kind() != reflect.Struct {
		return false, errors.Errorf(errFmtNotPtrToStruct, "desiredObject", desiredObject)
	}
	if typeOfObservedObject.Kind() != reflect.Ptr || typeOfObservedObject.Elem().Kind() != reflect.Struct {
		return false, errors.Errorf(errFmtNotPtrToStruct, "observedObject", observedObject)
	}
	if reflect.TypeOf(desiredObject) != reflect.TypeOf(observedObject) {
		return false, errors.Errorf(errFmtTypeMismatch, reflect.TypeOf(desiredObject).String(), reflect.TypeOf(observedObject).String())
	}
	defer func() {
		if r := recover(); r != nil {
			err = errors.Errorf(errFmtPanic, r, debug.Stack())
		}
	}()
	changed, err = li.handleStruct("", desiredObject, observedObject)
	return
}

// nolint:gocyclo
func (li *GenericLateInitializer) handleStruct(parentName string, desiredObject any, observedObject any) (bool, error) {
	typeOfDesiredObject, typeOfObservedObject := reflect.TypeOf(desiredObject), reflect.TypeOf(observedObject)
	valueOfDesiredObject, valueOfObservedObject := reflect.ValueOf(desiredObject), reflect.ValueOf(observedObject).Elem()
	typeOfDesiredObject, typeOfObservedObject = typeOfDesiredObject.Elem(), typeOfObservedObject.Elem()
	valueOfDesiredObject = valueOfDesiredObject.Elem()
	fieldAssigned := false

	for f := 0; f < typeOfDesiredObject.NumField(); f++ {
		desiredStructField := typeOfDesiredObject.Field(f)
		desiredFieldValue := valueOfDesiredObject.FieldByName(desiredStructField.Name)
		cName := getCanonicalName(parentName, desiredStructField.Name)
		filtered := false

		for _, f := range li.nameFilters {
			if f(cName) {
				filtered = true
				break
			}
		}
		if filtered {
			continue
		}

		observedStructField, _ := typeOfObservedObject.FieldByName(desiredStructField.Name)
		observedFieldValue := valueOfObservedObject.FieldByName(desiredStructField.Name)
		desiredKeepField := false
		var err error

		if !desiredFieldValue.IsZero() {
			continue
		}

		for _, f := range li.valueFilters {
			if f(cName, observedStructField, observedFieldValue) {
				// corresponding field value is filtered
				filtered = true
				break
			}
		}
		if filtered {
			continue
		}

		switch desiredStructField.Type.Kind() { // nolint:exhaustive
		// handle pointer struct field
		case reflect.Ptr:
			desiredKeepField, err = li.handlePtr(cName, desiredFieldValue, observedFieldValue)

		case reflect.Slice:
			desiredKeepField, err = li.handleSlice(cName, desiredFieldValue, observedFieldValue)

		case reflect.Map:
			desiredKeepField, err = li.handleMap(cName, desiredFieldValue, observedFieldValue)
		}

		if err != nil {
			return false, err
		}

		fieldAssigned = fieldAssigned || desiredKeepField
	}

	return fieldAssigned, nil
}

func (li *GenericLateInitializer) handlePtr(cName string, desiredFieldValue, observedFieldValue reflect.Value) (bool, error) {
	if observedFieldValue.IsNil() || !desiredFieldValue.IsNil() {
		return false, nil
	}
	// initialize with a nil pointer
	v := desiredFieldValue.Interface()
	desiredFieldValue.Set(reflect.New(reflect.ValueOf(&v).Elem().Elem().Type().Elem()))
	desiredKeepField := false

	switch {
	// if we are dealing with a struct type, recursively check fields
	case observedFieldValue.Elem().Kind() == reflect.Struct:
		desiredFieldValue.Set(reflect.New(desiredFieldValue.Type().Elem()))
		nestedFieldAssigned, err := li.handleStruct(cName, desiredFieldValue.Interface(), observedFieldValue.Interface())
		if err != nil {
			return false, err
		}
		desiredKeepField = nestedFieldAssigned

	default: // then cr object's field is not set but response object contains a value, carry it
		if desiredFieldValue.Kind() == reflect.Ptr && desiredFieldValue.IsNil() {
			desiredFieldValue.Set(reflect.New(desiredFieldValue.Type().Elem()))
		}

		// initialize new copy from response field
		desiredFieldValue.Elem().Set(observedFieldValue.Elem())
		desiredKeepField = true
	}

	return desiredKeepField, nil
}

func (li *GenericLateInitializer) handleSlice(cName string, desiredFieldValue, observedFieldValue reflect.Value) (bool, error) {
	if observedFieldValue.IsNil() || !desiredFieldValue.IsNil() {
		return false, nil
	}
	// initialize with an empty slice
	v := desiredFieldValue.Interface()
	desiredFieldValue.Set(reflect.MakeSlice(reflect.ValueOf(&v).Elem().Elem().Type(), 0, observedFieldValue.Len()))

	// then cr object's field is not set but response object contains a value, carry it
	// copy slice items from response field
	for i := 0; i < observedFieldValue.Len(); i++ {
		// allocate new items for the CR
		item := reflect.New(desiredFieldValue.Type().Elem())
		// error from processing the next element of the slice
		var err error
		// check slice item's kind (not slice type)
		switch item.Elem().Kind() { // nolint:exhaustive
		// if dealing with a slice of pointers
		case reflect.Ptr:
			_, err = li.handlePtr(cName, item.Elem(), observedFieldValue.Index(i))
		case reflect.Struct:
			_, err = li.handleStruct(cName, item.Interface(), observedFieldValue.Index(i).Addr().Interface())
		case reflect.String, reflect.Bool, reflect.Int, reflect.Uint,
			reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			// set primitive type
			item.Elem().Set(observedFieldValue.Index(i))
		// other slice item types are not supported
		default:
			return false, errors.Errorf("slice items of kind %q is not supported for canonical name: %s",
				item.Elem().Kind().String(), cName)
		}
		// if a type is used at different paths, be sure to define separate filters on corresponding canonical names
		if err != nil {
			return false, err
		}
		// a new item has been allocated, expand the slice with it
		desiredFieldValue.Set(reflect.Append(desiredFieldValue, item.Elem()))
	}
	return true, nil
}

func (li *GenericLateInitializer) handleMap(cName string, desiredFieldValue, observedFieldValue reflect.Value) (bool, error) {
	if observedFieldValue.IsNil() || !desiredFieldValue.IsNil() {
		return false, nil
	}
	// initialize with an empty map
	v := desiredFieldValue.Interface()
	desiredFieldValue.Set(reflect.MakeMap(reflect.ValueOf(&v).Elem().Elem().Type()))

	// then cr object's field is not set but response object contains a value, carry it
	// copy map items from response field
	for _, k := range observedFieldValue.MapKeys() {
		// allocate a new item for the CR
		item := reflect.New(desiredFieldValue.Type().Elem())
		// error from processing the next element of the map
		var err error
		// check map item's kind (not map type)
		switch item.Elem().Kind() { // nolint:exhaustive
		// if dealing with a slice of pointers
		case reflect.Ptr:
			_, err = li.handlePtr(cName, item.Elem(), observedFieldValue.MapIndex(k))
		// else if dealing with a slice of slices
		case reflect.Slice:
			_, err = li.handleSlice(cName, item.Elem(), observedFieldValue.MapIndex(k))
		case reflect.String, reflect.Bool, reflect.Int, reflect.Uint,
			reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			// set primitive type
			item.Elem().Set(observedFieldValue.MapIndex(k))
		// other slice item types are not supported
		default:
			return false, errors.Errorf(errFmtMapElemNotSupported, item.Elem().Kind().String(), cName)
		}
		if err != nil {
			return false, err
		}
		// set value at current key
		desiredFieldValue.SetMapIndex(k, item.Elem())
	}

	return true, nil
}

func getCanonicalName(parent, child string) string {
	if parent == "" {
		return child
	}

	return fmt.Sprintf(fmtCanonical, parent, child)
}

// IsTest returns true if the managed resource has upjet.upbound.io/test= "true" annotation
func IsTest(mg xpresource.Managed) bool {
	return mg.GetAnnotations()[AnnotationKeyTestResource] == "true"
}
