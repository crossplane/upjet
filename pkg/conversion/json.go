package conversion

import jsoniter "github.com/json-iterator/go"

// TFParser is a json parser to marshal/unmarshal using "tf" tag.
var TFParser = jsoniter.Config{TagKey: "tf"}.Froze()
