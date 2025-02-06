// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"reflect"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
)

func TestConvert(t *testing.T) {
	type args struct {
		params map[string]any
		paths  []string
		mode   ListConversionMode
		opts   *ConvertOptions
	}
	type want struct {
		err    error
		params map[string]any
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"NilParamsAndPaths": {
			reason: "Conversion on an nil map should not fail.",
			args:   args{},
		},
		"EmptyPaths": {
			reason: "Empty conversion on a map should be an identity function.",
			args: args{
				params: map[string]any{"a": "b"},
			},
			want: want{
				params: map[string]any{"a": "b"},
			},
		},
		"SingletonListToEmbeddedObject": {
			reason: "Should successfully convert a singleton list at the root level to an embedded object.",
			args: args{
				params: map[string]any{
					"l": []map[string]any{
						{
							"k": "v",
						},
					},
				},
				paths: []string{"l"},
				mode:  ToEmbeddedObject,
			},
			want: want{
				params: map[string]any{
					"l": map[string]any{
						"k": "v",
					},
				},
			},
		},
		"NestedSingletonListsToEmbeddedObjectsPathsInLexicalOrder": {
			reason: "Should successfully convert the parent & nested singleton lists to embedded objects. Paths specified in lexical order.",
			args: args{
				params: map[string]any{
					"parent": []map[string]any{
						{
							"child": []map[string]any{
								{
									"k": "v",
								},
							},
						},
					},
				},
				paths: []string{"parent", "parent[*].child"},
				mode:  ToEmbeddedObject,
			},
			want: want{
				params: map[string]any{
					"parent": map[string]any{
						"child": map[string]any{
							"k": "v",
						},
					},
				},
			},
		},
		"NestedSingletonListsToEmbeddedObjectsPathsInReverseLexicalOrder": {
			reason: "Should successfully convert the parent & nested singleton lists to embedded objects. Paths specified in reverse-lexical order.",
			args: args{
				params: map[string]any{
					"parent": []map[string]any{
						{
							"child": []map[string]any{
								{
									"k": "v",
								},
							},
						},
					},
				},
				paths: []string{"parent[*].child", "parent"},
				mode:  ToEmbeddedObject,
			},
			want: want{
				params: map[string]any{
					"parent": map[string]any{
						"child": map[string]any{
							"k": "v",
						},
					},
				},
			},
		},
		"EmbeddedObjectToSingletonList": {
			reason: "Should successfully convert an embedded object at the root level to a singleton list.",
			args: args{
				params: map[string]any{
					"l": map[string]any{
						"k": "v",
					},
				},
				paths: []string{"l"},
				mode:  ToSingletonList,
			},
			want: want{
				params: map[string]any{
					"l": []map[string]any{
						{
							"k": "v",
						},
					},
				},
			},
		},
		"NestedEmbeddedObjectsToSingletonListInLexicalOrder": {
			reason: "Should successfully convert the parent & nested embedded objects to singleton lists. Paths are specified in lexical order.",
			args: args{
				params: map[string]any{
					"parent": map[string]any{
						"child": map[string]any{
							"k": "v",
						},
					},
				},
				paths: []string{"parent", "parent[*].child"},
				mode:  ToSingletonList,
			},
			want: want{
				params: map[string]any{
					"parent": []map[string]any{
						{
							"child": []map[string]any{
								{
									"k": "v",
								},
							},
						},
					},
				},
			},
		},
		"NestedEmbeddedObjectsToSingletonListInReverseLexicalOrder": {
			reason: "Should successfully convert the parent & nested embedded objects to singleton lists. Paths are specified in reverse-lexical order.",
			args: args{
				params: map[string]any{
					"parent": map[string]any{
						"child": map[string]any{
							"k": "v",
						},
					},
				},
				paths: []string{"parent[*].child", "parent"},
				mode:  ToSingletonList,
			},
			want: want{
				params: map[string]any{
					"parent": []map[string]any{
						{
							"child": []map[string]any{
								{
									"k": "v",
								},
							},
						},
					},
				},
			},
		},
		"FailConversionOfAMultiItemList": {
			reason: `Conversion of a multi-item list in mode "ToEmbeddedObject" should fail.`,
			args: args{
				params: map[string]any{
					"l": []map[string]any{
						{
							"k1": "v1",
						},
						{
							"k2": "v2",
						},
					},
				},
				paths: []string{"l"},
				mode:  ToEmbeddedObject,
			},
			want: want{
				err: errors.Errorf(errFmtMultiItemList, "l", 2),
			},
		},
		"FailConversionOfNonSlice": {
			reason: `Conversion of a non-slice value in mode "ToEmbeddedObject" should fail.`,
			args: args{
				params: map[string]any{
					"l": map[string]any{
						"k": "v",
					},
				},
				paths: []string{"l"},
				mode:  ToEmbeddedObject,
			},
			want: want{
				err: errors.Errorf(errFmtNonSlice, "l", reflect.TypeOf(map[string]any{})),
			},
		},
		"ToSingletonListWithNonExistentPath": {
			reason: `"ToSingletonList" mode conversions specifying only non-existent paths should be identity functions.`,
			args: args{
				params: map[string]any{
					"l": map[string]any{
						"k": "v",
					},
				},
				paths: []string{"nonexistent"},
				mode:  ToSingletonList,
			},
			want: want{
				params: map[string]any{
					"l": map[string]any{
						"k": "v",
					},
				},
			},
		},
		"ToEmbeddedObjectWithNonExistentPath": {
			reason: `"ToEmbeddedObject" mode conversions specifying only non-existent paths should be identity functions.`,
			args: args{
				params: map[string]any{
					"l": []map[string]any{
						{
							"k": "v",
						},
					},
				},
				paths: []string{"nonexistent"},
				mode:  ToEmbeddedObject,
			},
			want: want{
				params: map[string]any{
					"l": []map[string]any{
						{
							"k": "v",
						},
					},
				},
			},
		},
		"WithInjectedKeySingletonListToEmbeddedObject": {
			reason: "Should successfully convert a singleton list at the root level to an embedded object.",
			args: args{
				params: map[string]any{
					"l": []map[string]any{
						{
							"k":     "v",
							"index": "0",
						},
					},
				},
				paths: []string{"l"},
				mode:  ToEmbeddedObject,
				opts: &ConvertOptions{
					ListInjectKeys: map[string]SingletonListInjectKey{
						"l": {
							Key:   "index",
							Value: "0",
						},
					},
				}},
			want: want{
				params: map[string]any{
					"l": map[string]any{
						"k": "v",
					},
				},
			},
		},
		"WithInjectedKeyEmbeddedObjectToSingletonList": {
			reason: "Should successfully convert an embedded object at the root level to a singleton list.",
			args: args{
				params: map[string]any{
					"l": map[string]any{
						"k": "v",
					},
				},
				paths: []string{"l"},
				mode:  ToSingletonList,
				opts: &ConvertOptions{
					ListInjectKeys: map[string]SingletonListInjectKey{
						"l": {
							Key:   "index",
							Value: "0",
						},
					},
				},
			},
			want: want{
				params: map[string]any{
					"l": []map[string]any{
						{
							"k":     "v",
							"index": "0",
						},
					},
				},
			},
		},
		"WithInjectedKeyNestedEmbeddedObjectsToSingletonListInLexicalOrder": {
			reason: "Should successfully convert the parent & nested embedded objects to singleton lists. Paths are specified in lexical order.",
			args: args{
				params: map[string]any{
					"parent": map[string]any{
						"child": map[string]any{
							"k": "v",
						},
					},
				},
				paths: []string{"parent", "parent[*].child"},
				mode:  ToSingletonList,
				opts: &ConvertOptions{
					ListInjectKeys: map[string]SingletonListInjectKey{
						"parent": {
							Key:   "index",
							Value: "0",
						},
						"parent[*].child": {
							Key:   "another",
							Value: "0",
						},
					},
				},
			},
			want: want{
				params: map[string]any{
					"parent": []map[string]any{
						{
							"index": "0",
							"child": []map[string]any{
								{
									"k":       "v",
									"another": "0",
								},
							},
						},
					},
				},
			},
		},
		"WithInjectedKeyNestedSingletonListsToEmbeddedObjectsPathsInLexicalOrder": {
			reason: "Should successfully convert the parent & nested singleton lists to embedded objects. Paths specified in lexical order.",
			args: args{
				params: map[string]any{
					"parent": []map[string]any{
						{
							"index": "0",
							"child": []map[string]any{
								{
									"k":       "v",
									"another": "0",
								},
							},
						},
					},
				},
				paths: []string{"parent", "parent[*].child"},
				mode:  ToEmbeddedObject,
				opts: &ConvertOptions{
					ListInjectKeys: map[string]SingletonListInjectKey{
						"parent": {
							Key:   "index",
							Value: "0",
						},
						"parent[*].child": {
							Key:   "another",
							Value: "0",
						},
					},
				},
			},
			want: want{
				params: map[string]any{
					"parent": map[string]any{
						"child": map[string]any{
							"k": "v",
						},
					},
				},
			},
		},
	}

	for n, tt := range tests {
		t.Run(n, func(t *testing.T) {
			params, err := roundTrip(tt.args.params)
			if err != nil {
				t.Fatalf("Failed to preprocess tt.args.params: %v", err)
			}
			wantParams, err := roundTrip(tt.want.params)
			if err != nil {
				t.Fatalf("Failed to preprocess tt.want.params: %v", err)
			}
			got, err := Convert(params, tt.args.paths, tt.args.mode, tt.args.opts)
			if diff := cmp.Diff(tt.want.err, err, test.EquateErrors()); diff != "" {
				t.Fatalf("\n%s\nConvert(tt.args.params, tt.args.paths): -wantErr, +gotErr:\n%s", tt.reason, diff)
			}
			if diff := cmp.Diff(wantParams, got); diff != "" {
				t.Errorf("\n%s\nConvert(tt.args.params, tt.args.paths): -wantConverted, +gotConverted:\n%s", tt.reason, diff)
			}
		})
	}
}

func TestModeString(t *testing.T) {
	tests := map[string]struct {
		m    ListConversionMode
		want string
	}{
		"ToSingletonList": {
			m:    ToSingletonList,
			want: "toSingletonList",
		},
		"ToEmbeddedObject": {
			m:    ToEmbeddedObject,
			want: "toEmbeddedObject",
		},
		"Unknown": {
			m:    ToSingletonList + 1,
			want: "unknown",
		},
	}
	for n, tt := range tests {
		t.Run(n, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, tt.m.String()); diff != "" {
				t.Errorf("String(): -want, +got:\n%s", diff)
			}
		})
	}
}

func roundTrip(m map[string]any) (map[string]any, error) {
	if len(m) == 0 {
		return m, nil
	}
	buff, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(m)
	if err != nil {
		return nil, err
	}
	var r map[string]any
	return r, jsoniter.ConfigCompatibleWithStandardLibrary.Unmarshal(buff, &r)
}
