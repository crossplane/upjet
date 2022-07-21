/*
Copyright 2022 Upbound Inc.
*/

package resource

import (
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	xpref "github.com/crossplane/crossplane-runtime/pkg/reference"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
)

// ExtractResourceID extracts the value of `status.atProvider.id`
// from a Terraformed resource. If mr is not a Terraformed
// resource, returns an empty string.
func ExtractResourceID() xpref.ExtractValueFn {
	return func(mr xpresource.Managed) string {
		tr, ok := mr.(Terraformed)
		if !ok {
			return ""
		}
		return tr.GetID()
	}
}

// ExtractParamPath extracts the value of `sourceAttr`
// from `spec.forProvider` allowing nested parameters.
// If `isObservation` is set, then referenced param
// is retrieved from the status, if not, it's extracted
// from the spec.
// An example argument to ExtractParamPath is
// `key`, if `spec.forProvider.key` is to be extracted
// from the referred resource.
func ExtractParamPath(sourceAttr string, isObservation bool) xpref.ExtractValueFn {
	return func(mr xpresource.Managed) string {
		tr, ok := mr.(Terraformed)
		if !ok {
			return ""
		}
		var params map[string]any
		var err error
		if isObservation {
			params, err = tr.GetObservation()
		} else {
			params, err = tr.GetParameters()
		}
		// TODO: we had better log the error
		if err != nil {
			return ""
		}
		paved := fieldpath.Pave(params)
		v, err := paved.GetString(sourceAttr)
		// TODO: we had better log the error
		if err != nil {
			return ""
		}
		return v
	}
}
