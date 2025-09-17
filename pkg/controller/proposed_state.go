// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// adapted from https://github.com/hashicorp/terraform/blob/v1.5.5/internal/plans/objchange/objchange.go

type BlockNestingMode uint8

var (
	// BlockNestingModeList is for attributes that represent a list of objects,
	// with multiple instances of those attributes nested inside a list
	// under another attribute.
	BlockNestingModeList = rschema.ListNestedBlock{}.GetNestingMode()

	// BlockNestingModeSet is for attributes that represent a set of objects,
	// with multiple, unique instances of those attributes nested inside a
	// set under another attribute.
	BlockNestingModeSet = rschema.SetNestedBlock{}.GetNestingMode()

	// BlockNestingModeSingle is for attributes that represent a single object.
	// The object cannot be repeated in the practitioner configuration.
	//
	// While the framework implements support for this block nesting mode, it
	// is not thoroughly tested in production Terraform environments beyond the
	// resource timeouts block from the older Terraform Plugin SDK. Use single
	// nested attributes for new implementations instead.
	BlockNestingModeSingle = rschema.SingleNestedBlock{}.GetNestingMode()
)

// NestingMode is an enum type of the ways nested attributes can be nested in
// an attribute. They can be a list, a set, a map (with string
// keys), or they can be nested directly, like an object.

var (
	// NestingModeSingle is for attributes that represent a struct or
	// object, a single instance of those attributes directly nested under
	// another attribute.
	NestingModeSingle = rschema.SingleNestedAttribute{}.GetNestingMode()

	// NestingModeList is for attributes that represent a list of objects,
	// with multiple instances of those attributes nested inside a list
	// under another attribute.
	NestingModeList = rschema.ListNestedAttribute{}.GetNestingMode()

	// NestingModeSet is for attributes that represent a set of objects,
	// with multiple, unique instances of those attributes nested inside a
	// set under another attribute.
	NestingModeSet = rschema.SetNestedAttribute{}.GetNestingMode()

	// NestingModeMap is for attributes that represent a map of objects,
	// with multiple instances of those attributes, each associated with a
	// unique string key, nested inside a map under another attribute.
	NestingModeMap = rschema.MapNestedAttribute{}.GetNestingMode()
)

func proposedState(schema rschema.Schema, prior, config *tfprotov6.DynamicValue) (tftypes.Value, error) {
	priorTFVal, err := prior.Unmarshal(schema.Type().TerraformType(context.TODO()))
	if err != nil {
		return priorTFVal, errors.Wrap(err, "cannot unmarshal TF prior state value")
	}
	configTFVal, err := config.Unmarshal(schema.Type().TerraformType(context.TODO()))
	if err != nil {
		return configTFVal, errors.Wrap(err, "cannot unmarshal TF Config value")
	}
	return proposedNew(schema, priorTFVal, configTFVal), nil
}

// proposedNew constructs a proposed new object value by combining the
// computed attribute values from "prior" with the configured attribute values
// from "config".
//
// Both value must conform to the given schema's implied type, or this function
// will panic.
//
// The prior value must be wholly known, but the config value may be unknown
// or have nested unknown values.
//
// The merging of the two objects includes the attributes of any nested blocks,
// which will be correlated in a manner appropriate for their nesting mode.
// Note in particular that the correlation for blocks backed by sets is a
// heuristic based on matching non-computed attribute values and so it may
// produce strange results with more "extreme" cases, such as a nested set
// block where _all_ attributes are computed.
func proposedNew(schema rschema.Schema, prior, config tftypes.Value) tftypes.Value {
	// If the config and prior are both null, return early here before
	// populating the prior block. The prevents non-null blocks from appearing
	// the proposed state value.
	if config.IsNull() && prior.IsNull() {
		return prior
	}

	if prior.IsNull() {
		// In this case, we will construct a synthetic prior value that is
		// similar to the result of decoding an empty configuration block,
		// which simplifies our handling of the top-level attributes/blocks
		// below by giving us one non-null level of object to pull values from.
		//
		// "All attributes null" happens to be the definition of emptyValue for
		// a Block, so we can just delegate to that
		prior = emptyValue(schema)
	}
	if config.IsNull() || !config.IsKnown() {
		// A block config should never be null at this point. The only nullable
		// block type is NestingSingle, which will return early before coming
		// back here. We'll allow the null here anyway to free callers from
		// needing to specifically check for these cases, and any mismatch will
		// be caught in validation, so just take the prior value rather than
		// the invalid null.
		return prior
	}

	if (!prior.Type().Is(tftypes.Object{})) || (!config.Type().Is(tftypes.Object{})) {
		panic("proposedNew only supports object-typed values")
	}

	// From this point onwards, we can assume that both values are non-null
	// object types, and that the config value itself is known (though it
	// may contain nested values that are unknown.)
	priorMap := make(map[string]tftypes.Value)
	configValueMap := make(map[string]tftypes.Value)
	_ = prior.As(&priorMap)
	_ = config.As(&configValueMap)

	newAttrs := proposedNewAttributes(schema.Attributes, prior, config)

	// Merging nested blocks is a little more complex, since we need to
	// correlate blocks between both objects and then recursively propose
	// a new object for each. The correlation logic depends on the nesting
	// mode for each block type.
	for name, blockType := range schema.Blocks {

		priorV := priorMap[name]
		configV := configValueMap[name]
		newAttrs[name] = proposedNewNestedBlock(blockType, priorV, configV)
	}

	return tftypes.NewValue(schema.Type().TerraformType(context.TODO()), newAttrs)

}

func emptyValue(sc rschema.Schema) tftypes.Value {
	vals := make(map[string]tftypes.Value)
	for name, attrS := range sc.Attributes {
		vals[name] = emptyAttribute(attrS)
	}
	for name, blockS := range sc.Blocks {
		vals[name] = emptyBlock(blockS)

	}
	return tftypes.NewValue(sc.Type().TerraformType(context.TODO()), vals)
}

func emptyAttribute(attr rschema.Attribute) tftypes.Value {
	switch attr := attr.(type) {
	case rschema.NestedAttribute:
		switch attr.GetNestingMode() { //nolint:exhaustive
		case NestingModeSingle:
			vals := make(map[string]tftypes.Value)
			for key, na := range attr.GetNestedObject().GetAttributes() {
				vals[key] = emptyAttribute(na)
			}
			return tftypes.NewValue(attr.GetType().TerraformType(context.TODO()), vals)
		case NestingModeList:
			return tftypes.NewValue(attr.GetType().TerraformType(context.TODO()), []tftypes.Value{})
		case NestingModeSet:
			return tftypes.NewValue(attr.GetType().TerraformType(context.TODO()), []tftypes.Value{})
		case NestingModeMap:
			return tftypes.NewValue(attr.GetType().TerraformType(context.TODO()), map[string]tftypes.Value{})
		default:
			return tftypes.NewValue(attr.GetType().TerraformType(context.TODO()), nil)
		}

	default:
		return tftypes.NewValue(attr.GetType().TerraformType(context.TODO()), nil)
	}
}

func emptyBlock(blockS rschema.Block) tftypes.Value {
	switch blockS.GetNestingMode() { //nolint:exhaustive
	case BlockNestingModeList:
		return tftypes.NewValue(blockS.Type().TerraformType(context.TODO()), []tftypes.Value{})
	case BlockNestingModeSet:
		return tftypes.NewValue(blockS.Type().TerraformType(context.TODO()), []tftypes.Value{})
	case BlockNestingModeSingle:
		return tftypes.NewValue(blockS.Type().TerraformType(context.TODO()), nil)
	default:
		return tftypes.NewValue(blockS.Type().TerraformType(context.TODO()), nil)
	}
}

// proposedNewBlockOrObject dispatched the schema to either proposedNew or
// proposedNewObjectAttributes depending on the given type.
func proposedNewBlockOrObject(schema nestedSchema, prior, config tftypes.Value) tftypes.Value {
	switch schema := schema.(type) {
	case rschema.Block:
		resAttrs := make(map[string]rschema.Attribute)
		for k, v := range schema.GetNestedObject().GetAttributes() {
			resAttrs[k] = v
		}
		resBlocks := make(map[string]rschema.Block)
		for k, v := range schema.GetNestedObject().GetBlocks() {
			resBlocks[k] = v
		}
		pseudoSchema := rschema.Schema{
			Attributes: resAttrs,
			Blocks:     resBlocks,
		}
		return proposedNew(pseudoSchema, prior, config)
	case rschema.Schema:
		return proposedNew(schema, prior, config)
	case rschema.NestedAttribute:
		return proposedNewObjectAttributes(schema, prior, config)
	default:
		panic(fmt.Sprintf("unexpected schema type %T", schema))
	}
}

func proposedNewNestedBlock(schema rschema.Block, prior, config tftypes.Value) tftypes.Value {
	// The only time we should encounter an entirely unknown block is from the
	// use of dynamic with an unknown for_each expression.
	if !config.IsKnown() {
		return config
	}

	newV := config

	resAttrs := make(map[string]rschema.Attribute)
	for k, v := range schema.GetNestedObject().GetAttributes() {
		resAttrs[k] = v
	}
	resBlocks := make(map[string]rschema.Block)
	for k, v := range schema.GetNestedObject().GetBlocks() {
		resBlocks[k] = v
	}
	pseudoSchema := rschema.Schema{
		Attributes: resAttrs,
		Blocks:     resBlocks,
	}

	switch schema.GetNestingMode() { //nolint:exhaustive
	case BlockNestingModeSingle:
		// A NestingSingle configuration block value can be null, and since it
		// cannot be computed we can always take the configuration value.
		if config.IsNull() {
			break
		}
		newV = proposedNew(pseudoSchema, prior, config)
	case BlockNestingModeList:
		newV = proposedNewNestingList(pseudoSchema, prior, config)
	case BlockNestingModeSet:
		newV = proposedNewNestingSet(pseudoSchema, prior, config)
	default:
		// Should never happen, since the above cases are comprehensive.
		panic(fmt.Sprintf("unsupported block nesting mode %v", schema.GetNestingMode()))
	}

	return newV
}

func proposedNewNestedType(schema rschema.NestedAttribute, prior, config tftypes.Value) tftypes.Value {
	// if the config isn't known at all, then we must use that value
	if !config.IsKnown() {
		return config
	}

	// Even if the config is null or empty, we will be using this default value.
	newV := config
	switch schema.GetNestingMode() { //nolint:exhaustive
	case NestingModeSingle:
		// If the config is null, we already have our value. If the attribute
		// is optional+computed, we won't reach this branch with a null value
		// since the computed case would have been taken.
		if config.IsNull() {
			break
		}
		newV = proposedNewObjectAttributes(schema, prior, config)
	case NestingModeList:
		newV = proposedNewNestingList(schema, prior, config)
	case NestingModeMap:
		newV = proposedNewNestingMap(schema, prior, config)
	case NestingModeSet:
		newV = proposedNewNestingSet(schema, prior, config)
	default:
		// Should never happen, since the above cases are comprehensive.
		panic(fmt.Sprintf("unsupported attribute nesting mode %v", schema.GetNestingMode()))
	}

	return newV
}

func proposedNewNestingList(schema nestedSchema, prior, config tftypes.Value) tftypes.Value { //nolint:gocyclo // logic is adapted, and easier to follow as a unit
	newV := config

	var configList []tftypes.Value
	var priorList []tftypes.Value
	_ = config.As(&configList)
	_ = prior.As(&priorList)
	// Nested blocks are correlated by index.
	configVLen := 0
	if !config.IsNull() {
		configVLen = len(configList)
	}
	if configVLen > 0 {
		newVals := make([]tftypes.Value, 0, configVLen)
		for idx, configEV := range configList {
			if prior.IsKnown() && (prior.IsNull() || len(priorList) <= idx) {
				// If there is no corresponding prior element then
				// we just take the config value as-is.
				newVals = append(newVals, configEV)
				continue
			}
			priorEV := priorList[idx]

			newVals = append(newVals, proposedNewBlockOrObject(schema, priorEV, configEV))
		}

		switch schema := schema.(type) {
		case rschema.Block:
			if config.Type().Is(tftypes.Tuple{}) {
				var elementTypes []tftypes.Type
				for _, val := range newVals {
					elementTypes = append(elementTypes, val.Type())
				}
				tt := tftypes.Tuple{ElementTypes: elementTypes}
				newV = tftypes.NewValue(tt, newVals)
			} else {
				tt := tftypes.List{ElementType: schema.Type().TerraformType(context.TODO())}
				newV = tftypes.NewValue(tt, newVals)
			}

		case rschema.Schema:
			if config.Type().Is(tftypes.Tuple{}) {
				var elementTypes []tftypes.Type
				for _, val := range newVals {
					elementTypes = append(elementTypes, val.Type())
				}
				tt := tftypes.Tuple{ElementTypes: elementTypes}
				newV = tftypes.NewValue(tt, newVals)
			} else {
				tt := tftypes.List{ElementType: schema.Type().TerraformType(context.TODO())}
				newV = tftypes.NewValue(tt, newVals)
			}
		case rschema.Attribute:
			newV = tftypes.NewValue(schema.GetType().TerraformType(context.TODO()), newVals)
		default:
			panic(fmt.Sprintf("unexpected schema type %T", schema))
		}

	}

	return newV
}

func proposedNewNestingMap(schema nestedSchema, prior, config tftypes.Value) tftypes.Value {
	newV := config

	newVals := map[string]tftypes.Value{}

	cfgMap := map[string]tftypes.Value{}
	priorMap := map[string]tftypes.Value{}
	_ = config.As(&cfgMap)
	_ = prior.As(&priorMap)

	if config.IsNull() || !config.IsKnown() || len(cfgMap) == 0 {
		// We already assigned newVal and there's nothing to compare in
		// config.
		return newV
	}

	for name, configEV := range cfgMap {
		priorEV, inPrior := priorMap[name]
		if !inPrior {
			// If there is no corresponding prior element then
			// we just take the config value as-is.
			newVals[name] = configEV
			continue
		}

		newVals[name] = proposedNewBlockOrObject(schema, priorEV, configEV)
	}

	switch schema := schema.(type) {
	case rschema.Block:
		tt := tftypes.Map{ElementType: schema.Type().TerraformType(context.TODO())}
		newV = tftypes.NewValue(tt, newVals)
	case rschema.Schema:
		tt := tftypes.Map{ElementType: schema.Type().TerraformType(context.TODO())}
		newV = tftypes.NewValue(tt, newVals)
	case rschema.Attribute:
		newV = tftypes.NewValue(schema.GetType().TerraformType(context.TODO()), newVals)
	default:
		panic(fmt.Sprintf("unexpected schema type %T", schema))
	}
	return newV
}

func proposedNewNestingSet(schema nestedSchema, prior, config tftypes.Value) tftypes.Value { //nolint:gocyclo // logic is adapted, and easier to follow as a unit
	if !config.Type().Is(tftypes.Set{}) {
		panic("configschema.NestingSet value is not a set as expected")
	}

	var configSet []tftypes.Value
	_ = config.As(&configSet)

	newV := config
	if !config.IsKnown() || config.IsNull() || len(configSet) == 0 {
		return newV
	}

	var priorVals []tftypes.Value
	if prior.IsKnown() && !prior.IsNull() {
		_ = prior.As(&priorVals)
	}

	var newVals []tftypes.Value //nolint:prealloc // we want to have it nil when not appended
	// track which prior elements have been used
	used := make([]bool, len(priorVals))

	for _, configEV := range configSet {
		var priorEV tftypes.Value
		for i, priorCmp := range priorVals {
			if used[i] {
				continue
			}

			// It is possible that multiple prior elements could be valid
			// matches for a configuration value, in which case we will end up
			// picking the first match encountered (but it will always be
			// consistent due to cty's iteration order). Because configured set
			// elements must also be entirely unique in order to be included in
			// the set, these matches either will not matter because they only
			// differ by computed values, or could not have come from a valid
			// config with all unique set elements.
			if validPriorFromConfig(schema, priorCmp, configEV) {
				priorEV = priorCmp
				used[i] = true
				break
			}
		}

		if priorEV.IsNull() {
			priorEV = tftypes.NewValue(config.Type().(tftypes.Set).ElementType, nil)
		}

		newVals = append(newVals, proposedNewBlockOrObject(schema, priorEV, configEV))
	}

	switch schema := schema.(type) {
	case rschema.Block:
		tt := tftypes.Set{ElementType: schema.Type().TerraformType(context.TODO())}
		newV = tftypes.NewValue(tt, newVals)
	case rschema.Schema:
		tt := tftypes.Set{ElementType: schema.Type().TerraformType(context.TODO())}
		newV = tftypes.NewValue(tt, newVals)
	case rschema.Attribute:
		newV = tftypes.NewValue(schema.GetType().TerraformType(context.TODO()), newVals)
	default:
		panic(fmt.Sprintf("unexpected schema type %T", schema))
	}

	return newV
}

func proposedNewObjectAttributes(schema rschema.NestedAttribute, prior, config tftypes.Value) tftypes.Value {
	if config.IsNull() {
		return config
	}

	rs := make(map[string]rschema.Attribute)
	for k, v := range schema.GetNestedObject().GetAttributes() {
		rat, ok := v.(rschema.Attribute)
		if !ok {
			panic("expected rschema.Attribute")
		}
		rs[k] = rat
	}

	return tftypes.NewValue(schema.GetType().TerraformType(context.TODO()), proposedNewAttributes(rs, prior, config))
}

func proposedNewAttributes(attrs map[string]rschema.Attribute, prior, config tftypes.Value) map[string]tftypes.Value {
	priorMap := make(map[string]tftypes.Value)
	configValueMap := make(map[string]tftypes.Value)
	_ = prior.As(&priorMap)
	_ = config.As(&configValueMap)
	newAttrs := make(map[string]tftypes.Value, len(attrs))
	for name, attr := range attrs {
		var priorV tftypes.Value
		if prior.IsNull() {
			priorV = tftypes.NewValue(priorMap[name].Type(), nil)
		} else {
			priorV = priorMap[name]
		}

		configV := configValueMap[name]

		var newV tftypes.Value
		// required isn't considered when constructing the plan, so attributes
		// are essentially either computed or not computed. In the case of
		// optional+computed, they are only computed when there is no
		// configuration.
		if attr.IsComputed() && configV.IsNull() {
			// configV will always be null in this case, by definition.
			// priorV may also be null, but that's okay.
			newV = priorV

			// the exception to the above is that if the config is optional and
			// the _prior_ value contains non-computed values, we can infer
			// that the config must have been non-null previously.
			if optionalValueNotComputable(attr, priorV) {
				newV = configV
			}
		} else if nestedAttr, ok := attr.(rschema.NestedAttribute); ok {
			// For non-computed NestedType attributes, we need to descend
			// into the individual nested attributes to build the final
			// value, unless the entire nested attribute is unknown.
			newV = proposedNewNestedType(nestedAttr, priorV, configV)
		} else {
			// For non-computed attributes, we always take the config value,
			// even if it is null. If it's _required_ then null values
			// should've been caught during an earlier validation step, and
			// so we don't really care about that here.
			newV = configV
		}

		newAttrs[name] = newV
	}
	return newAttrs
}

// nestedSchema is used as a generic container for either a
// schema.Object or schema.Block.
type nestedSchema interface {
	tftypes.AttributePathStepper
}

// optionalValueNotComputable is used to check if an object in state must
// have at least partially come from configuration. If the prior value has any
// non-null attributes which are not computed in the schema, then we know there
// was previously a configuration value which set those.
//
// This is used when the configuration contains a null optional+computed value,
// and we want to know if we should plan to send the null value or the prior
// state.
func optionalValueNotComputable(schema rschema.Attribute, val tftypes.Value) bool {
	if !schema.IsOptional() {
		return false
	}

	// We must have a NestedType for complex nested attributes in order
	// to find nested computed values in the first place.
	nestedAttr, ok := schema.(rschema.NestedAttribute)
	if !ok {
		return false
	}

	foundNonComputedAttr := false
	_ = tftypes.Walk(val, func(path *tftypes.AttributePath, v tftypes.Value) (bool, error) {
		if v.IsNull() {
			return true, nil
		}

		attr, _, _ := tftypes.WalkAttributePath(nestedAttr.GetNestedObject(), path)
		if attr == nil {
			return true, nil
		}
		rAttr, ok := attr.(rschema.Attribute)
		if !ok {
			return true, nil
		}

		if !rAttr.IsComputed() {
			foundNonComputedAttr = true
			return false, nil
		}
		return true, nil
	})

	return foundNonComputedAttr
}

// validPriorFromConfig returns true if the prior object could have been
// derived from the configuration. We do this by walking the prior value to
// determine if it is a valid superset of the config, and only computable
// values have been added. This function is only used to correlated
// configuration with possible valid prior values within sets.
func validPriorFromConfig(schema nestedSchema, prior, config tftypes.Value) bool { //nolint:gocyclo // logic is adapted, and easier to follow as a unit
	if config.Equal(prior) {
		return true
	}

	// error value to halt the walk
	stop := errors.New("stop")

	valid := true
	_ = tftypes.Walk(prior, func(path *tftypes.AttributePath, priorV tftypes.Value) (bool, error) {
		configValUntyped, _, err := tftypes.WalkAttributePath(config, path)
		if err != nil {
			valid = false
			return false, err
		}
		configV, ok := configValUntyped.(tftypes.Value)
		if !ok {
			valid = false
			return false, nil
		}

		// we don't need to know the schema if both are equal
		if configV.Equal(priorV) {
			// we know they are equal, so no need to descend further
			return false, nil
		}

		// We can't descend into nested sets to correlate configuration, so the
		// overall values must be equal.
		if configV.Type().Is(tftypes.Set{}) {
			valid = false
			return false, stop
		}

		var attr rschema.Attribute

		attrI, _, err := tftypes.WalkAttributePath(schema, path)
		if attrI == nil || err != nil {
			// Not at a schema attribute, so we can continue until we find leaf
			// attributes.
			return true, nil //nolint:nilerr // intentional
		}
		// If we have nested object attributes we'll be descending into those
		// to compare the individual values and determine why this level is not
		// equal
		if _, ok := attrI.(rschema.NestedAttribute); ok {
			return true, nil
		}
		if _, ok := attrI.(rschema.Block); ok {
			return true, nil
		}
		if _, ok := attrI.(rschema.Schema); ok {
			return true, nil
		}

		attr = attrI.(rschema.Attribute)

		// This is a leaf attribute, so it must be computed in order to differ
		// from config.
		if !attr.IsComputed() {
			valid = false
			return false, stop
		}

		// And if it is computed, the config must be null to allow a change.
		if !configV.IsNull() {
			valid = false
			return false, stop
		}

		// We sill stop here. The cty value could be far larger, but this was
		// the last level of prescribed schema.
		return false, nil
	})

	return valid
}
