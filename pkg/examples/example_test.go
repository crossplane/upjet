// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"bytes"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"sigs.k8s.io/yaml"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/registry/reference"
	"github.com/crossplane/upjet/v2/pkg/types/conversion/tfjson"
)

func TestFlattenSchemaTypeObjects(t *testing.T) {
	objectSchema := func(fields map[string]*schema.Schema) *schema.Schema {
		return &schema.Schema{
			Type:     tfjson.SchemaTypeObject,
			MaxItems: 1,
			Elem:     &schema.Resource{Schema: fields},
		}
	}
	r := config.DefaultResource("test_resource", &schema.Resource{Schema: map[string]*schema.Schema{
		"object_block": objectSchema(map[string]*schema.Schema{
			"inner_object": objectSchema(map[string]*schema.Schema{
				"value_field": {Type: schema.TypeString},
			}),
		}),
		"parent_list": {
			Type: schema.TypeList,
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"object_block": objectSchema(map[string]*schema.Schema{
					"value_field": {Type: schema.TypeString},
				}),
			}},
		},
	}}, nil, nil)

	tests := map[string]struct {
		params map[string]any
		want   map[string]any
	}{
		"NestedObjects": {
			params: map[string]any{
				"objectBlock": []any{map[string]any{
					"innerObject": []any{map[string]any{"valueField": "value"}},
				}},
			},
			want: map[string]any{
				"objectBlock": map[string]any{
					"innerObject": map[string]any{"valueField": "value"},
				},
			},
		},
		"ObjectInsideList": {
			params: map[string]any{
				"parentList": []any{
					map[string]any{"objectBlock": []any{map[string]any{"valueField": "one"}}},
					map[string]any{"objectBlock": []any{map[string]any{"valueField": "two"}}},
				},
			},
			want: map[string]any{
				"parentList": []any{
					map[string]any{"objectBlock": map[string]any{"valueField": "one"}},
					map[string]any{"objectBlock": map[string]any{"valueField": "two"}},
				},
			},
		},
		"AlreadyObject": {
			params: map[string]any{
				"objectBlock": map[string]any{
					"innerObject": map[string]any{"valueField": "value"},
				},
			},
			want: map[string]any{
				"objectBlock": map[string]any{
					"innerObject": map[string]any{"valueField": "value"},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			flattenSchemaTypeObjects(tt.params, r.TerraformResource)
			if diff := cmp.Diff(tt.want, tt.params); diff != "" {
				t.Errorf("flattenSchemaTypeObjects(...): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestWriteManifestPreservesReferenceShape(t *testing.T) {
	r := config.DefaultResource("test_resource", &schema.Resource{Schema: map[string]*schema.Schema{
		"metadata": {
			Type: tfjson.SchemaTypeObject,
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"uid": {Type: schema.TypeString},
			}},
		},
	}}, nil, nil)
	pm := &reference.PavedWithManifest{
		Paved: fieldpath.Pave(map[string]any{
			"metadata": map[string]any{
				"labels": map[string]string{labelExampleName: "example"},
			},
			"spec": map[string]any{
				"forProvider": map[string]any{
					"metadata": []any{map[string]any{"uid": "test-uid"}},
				},
			},
		}),
		ParamsPrefix: []string{"spec", "forProvider"},
		Config:       r,
	}

	var output bytes.Buffer
	eg := &Generator{}
	if err := eg.writeManifest(&output, pm, &reference.ResolutionContext{}); err != nil {
		t.Fatalf("writeManifest(...): %v", err)
	}
	if _, err := pm.Paved.GetString("spec.forProvider.metadata[0].uid"); err != nil {
		t.Errorf("writeManifest(...) mutated the reference source shape: %v", err)
	}
	var manifest map[string]any
	if err := yaml.Unmarshal(output.Bytes(), &manifest); err != nil {
		t.Fatalf("cannot unmarshal generated manifest: %v", err)
	}
	if _, err := fieldpath.Pave(manifest).GetString("spec.forProvider.metadata.uid"); err != nil {
		t.Errorf("writeManifest(...) did not flatten output: %v\n%s", err, output.String())
	}
}
