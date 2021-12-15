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

package main

import (
	"fmt"
	"io/ioutil"

	"github.com/crossplane-contrib/terrajet/pkg/types/conversion"

	tfjson "github.com/hashicorp/terraform-json"
)

func main() {
	b, err := ioutil.ReadFile("/Users/hasanturken/Workspace/task/terraform-provider-schema/provider-schema.json")
	if err != nil {
		panic(err)
	}

	pss := tfjson.ProviderSchemas{}
	if err = pss.UnmarshalJSON(b); err != nil {
		panic(err)
	}

	v2schemaMap := conversion.GetV2ResourceMapFromTFJSONSchemaMap(pss.Schemas["registry.terraform.io/hashicorp/aws"].ResourceSchemas)
	fmt.Println(len(v2schemaMap))
	return
}
