package conversion

import jsoniter "github.com/json-iterator/go"

var TFParser = jsoniter.Config{TagKey: "tf"}.Froze()
