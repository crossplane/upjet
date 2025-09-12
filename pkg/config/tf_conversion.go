// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"math/big"
	"reflect"
	"slices"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/config/conversion"
)

// Mode denotes the mode of the runtime Terraform conversion, e.g.,
// conversion from Crossplane parameters to Terraform arguments, or
// conversion from Terraform state to Crossplane state.
type Mode int

const (
	ToTerraform Mode = iota
	FromTerraform
)

// String returns a string representation of the conversion mode.
func (m Mode) String() string {
	switch m {
	case ToTerraform:
		return "toTerraform"
	case FromTerraform:
		return "fromTerraform"
	default:
		return "unknown"
	}
}

type TerraformConversion interface {
	Convert(params map[string]any, r *Resource, mode Mode) (map[string]any, error)
}

// ApplyTFConversions applies the configured Terraform conversions on the
// specified params map in the given mode, i.e., from Crossplane layer to the
// Terraform layer or vice versa.
func (r *Resource) ApplyTFConversions(params map[string]any, mode Mode) (map[string]any, error) {
	var err error
	for _, c := range r.TerraformConversions {
		params, err = c.Convert(params, r, mode)
		if err != nil {
			return nil, err
		}
	}
	return params, nil
}

type singletonListConversion struct{}

// NewTFSingletonConversion initializes a new TerraformConversion to convert
// between singleton lists and embedded objects in the exchanged data
// at runtime between the Crossplane & Terraform layers.
func NewTFSingletonConversion() TerraformConversion {
	return singletonListConversion{}
}

func (s singletonListConversion) Convert(params map[string]any, r *Resource, mode Mode) (map[string]any, error) {
	var err error
	var m map[string]any
	switch mode {
	case FromTerraform:
		m, err = conversion.Convert(params, r.TFListConversionPaths(), conversion.ToEmbeddedObject, nil)
	case ToTerraform:
		m, err = conversion.Convert(params, r.TFListConversionPaths(), conversion.ToSingletonList, nil)
	}
	return m, errors.Wrapf(err, "failed to convert between Crossplane and Terraform layers in mode %q", mode)
}

type dynamicValueConversion struct{}

// NewTFDynamicValueConversion initializes a new TerraformConversion to convert
// MR parameters that are DynamicPseudoType to their correct representation in
// the Terraform layer at runtime
func NewTFDynamicValueConversion() TerraformConversion {
	return dynamicValueConversion{}
}

func (s dynamicValueConversion) Convert(params map[string]any, r *Resource, mode Mode) (map[string]any, error) {
	if mode == FromTerraform {
		// Terraform does not return dynamic pseudo-types in state.
		// No conversion needed.
		return params, nil
	} else if mode != ToTerraform {
		return nil, errors.Errorf("invalid conversion mode %s", mode.String())
	}
	paths := r.TFDynamicAttributeConversionPaths()
	slices.Sort(paths)
	pv := fieldpath.Pave(params)
	for _, fp := range paths {
		exp, err := pv.ExpandWildcards(fp)
		if err != nil && !fieldpath.IsNotFound(err) {
			return nil, errors.Wrapf(err, "cannot expand wildcards for the field path expression %s", fp)
		}
		for _, e := range exp {
			val, err := pv.GetValue(e)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot get the value at the field path %s with the conversion mode set to %q", e, mode)
			}
			tt, err := inferTFTypeFromValue(val)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot infer dynamic value type at the field path %s with the conversion mode set to %q", e, mode)
			}
			newVal := map[string]any{"value": val, "type": tt}
			if err := pv.SetValue(e, newVal); err != nil {
				return nil, errors.Wrapf(err, "cannot set the value at the field path %s with the conversion mode set to %q", e, mode)
			}
		}
	}
	return params, nil
}

// inferTFTypeFromValue infers a tftypes.Type for a given MR parameter.
func inferTFTypeFromValue(value interface{}) (tftypes.Type, error) { //nolint:gocyclo // easier to follow as a unit
	switch v := value.(type) {
	case nil:
		// We can't infer null's type, so just return empty Object for default
		return tftypes.Object{}, nil
	case string, *string:
		return tftypes.String, nil
	case bool, *bool:
		return tftypes.Bool, nil
	case *big.Float, float64, *float64, int, *int, int8, *int8, int16, *int16, int32, *int32, int64, *int64, uint, *uint, uint8, *uint8, uint16, *uint16, uint32, *uint32, uint64, *uint64:
		return tftypes.Number, nil
	case []interface{}:
		if len(v) == 0 {
			// Default to list of empty Object if empty
			return tftypes.List{ElementType: tftypes.Object{}}, nil
		}
		elemType, err := inferTFTypeFromValue(v[0])
		if err != nil {
			return nil, err
		}
		return tftypes.List{ElementType: elemType}, nil
	case map[string]interface{}:
		attrTypes := make(map[string]tftypes.Type)
		for k, val := range v {
			inferred, err := inferTFTypeFromValue(val)
			if err != nil {
				return nil, err
			}
			attrTypes[k] = inferred
		}
		return tftypes.Object{AttributeTypes: attrTypes}, nil
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() { //nolint:exhaustive
		case reflect.Slice:
			if rv.Len() == 0 {
				return tftypes.List{ElementType: tftypes.String}, nil
			}
			elemType, err := inferTFTypeFromValue(rv.Index(0).Interface())
			if err != nil {
				return nil, err
			}
			return tftypes.List{ElementType: elemType}, nil
		case reflect.Map:
			if rv.Type().Key().Kind() != reflect.String {
				return nil, fmt.Errorf("only map[string] keys are supported")
			}
			attrTypes := make(map[string]tftypes.Type)
			for _, k := range rv.MapKeys() {
				val := rv.MapIndex(k).Interface()
				inferred, err := inferTFTypeFromValue(val)
				if err != nil {
					return nil, err
				}
				attrTypes[k.String()] = inferred
			}
			return tftypes.Object{AttributeTypes: attrTypes}, nil
		default:
			return nil, fmt.Errorf("unsupported type: %T", v)
		}
	}
}
