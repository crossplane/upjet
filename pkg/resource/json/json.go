/*
Copyright 2021 Upbound Inc.
*/

package json

import jsoniter "github.com/json-iterator/go"

// TFParser is a json parser to marshal/unmarshal using "tf" tag.
var TFParser = jsoniter.Config{TagKey: "tf"}.Froze()

// JSParser is a json parser to marshal/unmarshal using "json" tag.
var JSParser = jsoniter.Config{
	TagKey: "json",
	// We need to sort the map keys to get consistent output in tests.
	SortMapKeys: true,
}.Froze()
