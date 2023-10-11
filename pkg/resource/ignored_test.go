// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package resource

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	_ "embed"
)

func TestGetIgnoredFields(t *testing.T) {
	type args struct {
		initProvider map[string]any
		forProvider  map[string]any
	}
	type want struct {
		ignored []string
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"InitProviderEmpty": {
			reason: "Should return the empty ignored fields when initProvider is empty",
			args: args{
				forProvider: map[string]any{
					"singleField": "bob",
				},
			},
			want: want{
				ignored: []string{},
			},
		},
		"ForProviderEmpty": {
			reason: "Should return the ignored fields when forProvider is empty",
			args: args{
				initProvider: map[string]any{
					"singleField": "bob",
				},
			},
			want: want{
				ignored: []string{"singleField"},
			},
		},
		"SingleField": {
			reason: "Should return the single ignored field when forProvider has a different field",
			args: args{
				initProvider: map[string]any{
					"singleField": "bob",
				},
				forProvider: map[string]any{
					"otherField": "bob",
				},
			},
			want: want{
				ignored: []string{"singleField"},
			},
		},
		"SingleFieldSame": {
			reason: "Should not return the single ignored field when forProvider has same fields",
			args: args{
				initProvider: map[string]any{
					"singleField": "bob",
				},
				forProvider: map[string]any{
					"singleField": "bob",
				},
			},
			want: want{
				ignored: []string{},
			},
		},
		"NestedArrayField": {
			reason: "Should return the ignored field when init and for provider have nested arrays with different fields",
			args: args{
				initProvider: map[string]any{
					"array": []any{
						map[string]any{
							"singleField": "bob",
							"same":        "same",
						},
					},
				},
				forProvider: map[string]any{
					"array": []any{
						map[string]any{
							"otherField": "bob",
							"same":       "not same value",
						},
					},
				},
			},
			want: want{
				ignored: []string{"array[0].singleField"},
			},
		},
		"NestedArrayFieldMultiple": {
			reason: "Should return the multiple ignored fields when init and for provider have nested arrays with different fields",
			args: args{
				initProvider: map[string]any{
					"array": []any{
						map[string]any{
							"singleField": "bob",
							"same":        "same",
						},
						map[string]any{
							"nextField": "bob",
						},
					},
				},
				forProvider: map[string]any{
					"array": []any{
						map[string]any{
							"otherField": "bob",
							"same":       "not same value",
						},
					},
				},
			},
			want: want{
				ignored: []string{"array[0].singleField", "array[1]"},
			},
		},
		"NestedArraySameField": {
			reason: "Should not the ignored field when init and for provider have nested arrays with same fields",
			args: args{
				initProvider: map[string]any{
					"array": []any{
						map[string]any{
							"same": "same",
						},
					},
				},
				forProvider: map[string]any{
					"array": []any{
						map[string]any{
							"same": "not same value",
						},
					},
				},
			},
			want: want{
				ignored: []string{},
			},
		},
		"NestedMapKey": {
			reason: "Should return the ignored field when init and for provider have nested maps with different keys",
			args: args{
				initProvider: map[string]any{
					"map": map[string]any{
						"key":  "bob",
						"same": "same",
					},
				},
				forProvider: map[string]any{
					"map": map[string]any{
						"otherKey": "bob",
						"same":     "not same value",
					},
				},
			},
			want: want{
				ignored: []string{"map[\"key\"]"},
			},
		},
		"NestedMapSameKey": {
			reason: "Should not return the ignored field when init and for provider have nested maps with same keys",
			args: args{
				initProvider: map[string]any{
					"map": map[string]any{
						"same": "same",
					},
				},
				forProvider: map[string]any{
					"map": map[string]any{
						"same": "not same value",
					},
				},
			},
			want: want{
				ignored: []string{},
			},
		},
		"MissingMapField": {
			reason: "Should return the ignored field when init and for provider have different nested maps",
			args: args{
				initProvider: map[string]any{
					"map": map[string]any{
						"key": "bob",
					},
				},
				forProvider: map[string]any{
					"othermap": map[string]any{
						"key": "bob",
					},
				},
			},
			want: want{
				ignored: []string{"map"},
			},
		},
		"MissingArrayField": {
			reason: "Should return the ignored field when init and for provider have different nested arrays",
			args: args{
				initProvider: map[string]any{
					"array": []any{
						"bob",
					},
				},
				forProvider: map[string]any{
					"otherArray": []any{
						"bob",
					},
				},
			},
			want: want{
				ignored: []string{"array"},
			},
		},
		"MissingMultipleFields": {
			reason: "Should return the ignored fields when init and for provider have different multiple fields and nested fields",
			args: args{
				initProvider: map[string]any{
					"singleField": "bob",
					"array": []any{
						map[string]any{
							"singleField": "bob",
							"secondField": "secondBob",
							"sameKey":     "same",
						},
					},
					"map": map[string]any{
						"key":      "bob",
						"otherKey": "otherBob",
						"sameKey":  "same",
					},
				},
				forProvider: map[string]any{
					"otherField": "bob",
					"array": []any{
						map[string]any{
							"otherField": "bob",
							"sameKey":    "not same value",
						},
					},
					"map": map[string]any{
						"sameKey": "not same value",
					},
				},
			},
			want: want{
				ignored: []string{"array[0].secondField", "array[0].singleField", "map[\"key\"]", "map[\"otherKey\"]", "singleField"},
			},
		},
		"DoubleNestedArrayField": {
			reason: "Should return the ignored field when init and for provider have deep nested arrays with different fields",
			args: args{
				initProvider: map[string]any{
					"array": []any{
						map[string]any{
							"deepArray": []any{
								map[string]any{
									"singleField": "bob",
								},
							},
						},
					},
				},
				forProvider: map[string]any{
					"array": []any{
						map[string]any{
							"deepArray": []any{
								map[string]any{
									"otherField": "bob",
								},
							},
						},
					},
				},
			},
			want: want{
				ignored: []string{"array[0].deepArray[0].singleField"},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := GetTerraformIgnoreChanges(tc.args.forProvider, tc.args.initProvider)
			if diff := cmp.Diff(tc.want.ignored, got); diff != "" {
				t.Errorf("GetIgnorableFields() got = %v, want %v", got, tc.want.ignored)
			}
		})
	}
}
