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

package resource

import (
	"context"
	"testing"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/crossplane-contrib/terrajet/pkg/resource/fake/mocks"
	"github.com/crossplane-contrib/terrajet/pkg/resource/json"
)

var testData = []byte(`
{
  "top_level_secret": "sensitive-data-top-level-secret",
  "top_config_secretmap": {
	"inner_config_secretmap.first": "sensitive-data-inner-first",
	"inner_config_secretmap_second": "sensitive-data-inner-second",
	"inner_config_secretmap_third": "sensitive-data-inner-third"
  },
  "top_object_with_number": { "key1": 1, "key2": 2, "key3": 3},
  "top_config_array": [
    {
      "inner_some_field": "non-sensitive-data-1",
      "inner_config_array": [
        {
          "bottom_some_field": "non-sensitive-data-1",
          "bottom_level_secret": "sensitive-data-bottom-level-1"
        }
      ]
    },
    {
      "inner_some_field": "non-sensitive-data-2"
    },
    {
      "inner_some_field": "non-sensitive-data-3",
      "inner_config_array": [
        {
          "bottom_some_field": "non-sensitive-data-3a",
          "bottom_level_secret": "sensitive-data-bottom-level-3a"
        },
        {
          "bottom_some_field": "non-sensitive-data-3a",
          "bottom_level_secret": "sensitive-data-bottom-level-3b"
        }
      ]
    }
  ]
}
`)

func TestGetConnectionDetails(t *testing.T) {
	testInput := map[string]interface{}{}
	if err := json.JSParser.Unmarshal(testData, &testInput); err != nil {
		t.Fatalf("cannot unmarshall test data: %v", err)
	}
	type args struct {
		paths map[string]string
		data  map[string]interface{}
	}
	type want struct {
		out map[string][]byte
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"Single": {
			args: args{
				paths: map[string]string{"top_level_secret": ""},
				data:  testInput,
			},
			want: want{
				out: map[string][]byte{
					"top_level_secret": []byte("sensitive-data-top-level-secret"),
				},
			},
		},
		"SingleNonExisting": {
			args: args{
				paths: map[string]string{"missing_field": ""},
				data:  testInput,
			},
			want: want{
				out: map[string][]byte{},
			},
		},
		"SingleGettingNumber": {
			args: args{
				paths: map[string]string{"top_object_with_number[key1]": ""},
				data:  testInput,
			},
			want: want{
				err: errors.Wrapf(
					errors.Errorf("%s: not a string", "top_object_with_number.key1"),
					errFmtCannotGetStringForFieldPath, "top_object_with_number.key1"),
			},
		},
		"WildcardMultipleFromMap": {
			args: args{
				paths: map[string]string{"top_config_secretmap[*]": ""},
				data:  testInput,
			},
			want: want{
				out: map[string][]byte{
					"top_config_secretmap[inner_config_secretmap.first]": []byte("sensitive-data-inner-first"),
					"top_config_secretmap.inner_config_secretmap_second": []byte("sensitive-data-inner-second"),
					"top_config_secretmap.inner_config_secretmap_third":  []byte("sensitive-data-inner-third"),
				},
			},
		},
		"WildcardMultipleFromArray": {
			args: args{
				paths: map[string]string{"top_config_array[*].inner_some_field": ""},
				data:  testInput,
			},
			want: want{
				out: map[string][]byte{
					"top_config_array[0].inner_some_field": []byte("non-sensitive-data-1"),
					"top_config_array[1].inner_some_field": []byte("non-sensitive-data-2"),
					"top_config_array[2].inner_some_field": []byte("non-sensitive-data-3"),
				},
			},
		},
		"WildcardMultipleFromArrayMultipleLevel": {
			args: args{
				paths: map[string]string{"top_config_array[*].inner_config_array[*].bottom_level_secret": ""},
				data:  testInput,
			},
			want: want{
				out: map[string][]byte{
					"top_config_array[0].inner_config_array[0].bottom_level_secret": []byte("sensitive-data-bottom-level-1"),
					"top_config_array[2].inner_config_array[0].bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					"top_config_array[2].inner_config_array[1].bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
				},
			},
		},
		"WildcardMixedWithNumbers": {
			args: args{
				paths: map[string]string{"top_config_array[2].inner_config_array[*].bottom_level_secret": ""},
				data:  testInput,
			},
			want: want{
				out: map[string][]byte{
					"top_config_array[2].inner_config_array[0].bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					"top_config_array[2].inner_config_array[1].bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
				},
			},
		},
		"MultipleFieldPaths": {
			args: args{
				paths: map[string]string{"top_level_secret": "", "top_config_secretmap.*": "", "top_config_array[2].inner_config_array[*].bottom_level_secret": ""},
				data:  testInput,
			},
			want: want{
				out: map[string][]byte{
					"top_level_secret": []byte("sensitive-data-top-level-secret"),
					"top_config_secretmap[inner_config_secretmap.first]":            []byte("sensitive-data-inner-first"),
					"top_config_secretmap.inner_config_secretmap_second":            []byte("sensitive-data-inner-second"),
					"top_config_secretmap.inner_config_secretmap_third":             []byte("sensitive-data-inner-third"),
					"top_config_array[2].inner_config_array[0].bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					"top_config_array[2].inner_config_array[1].bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
				},
			},
		},
		"NotAValue": {
			args: args{
				paths: map[string]string{"top_config_secretmap": ""},
				data:  testInput,
			},
			want: want{
				err: errors.Wrapf(
					errors.Errorf("%s: not a string", "top_config_secretmap"),
					errFmtCannotGetStringForFieldPath, "top_config_secretmap"),
			},
		},
		"UnexpectedWildcard": {
			args: args{
				paths: map[string]string{"top_level_secret.*": ""},
				data:  testInput,
			},
			want: want{
				err: errors.Wrapf(errors.Wrapf(
					errors.Errorf("%q: unexpected wildcard usage", "top_level_secret"),
					"cannot expand wildcards for segments: %q", "top_level_secret[*]"),
					errCannotExpandWildcards),
			},
		},
		"NoData": {
			args: args{
				paths: map[string]string{"top_level_secret": ""},
				data:  nil,
			},
			want: want{
				out: map[string][]byte{},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := GetConnectionDetails(tc.data, tc.paths)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("GetFields(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("GetFields(...) out = %v, want %v", got, tc.want.out)
			}
		})
	}
}

func TestGetSensitiveParameters(t *testing.T) {
	type args struct {
		clientFn func(client *mocks.MockSecretClient)
		from     runtime.Object
		into     map[string]interface{}
		mapping  map[string]string
	}
	type want struct {
		out map[string]interface{}
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"SingleNoWildcard": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "admin-password",
							Namespace: "crossplane-system",
						},
						Key: "pass",
					})).Return([]byte("foo"), nil)
				},
				from: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"forProvider": map[string]interface{}{
								"adminPasswordSecretRef": map[string]interface{}{
									"name":      "admin-password",
									"namespace": "crossplane-system",
									"key":       "pass",
								},
							},
						},
					},
				},
				into: map[string]interface{}{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "adminPasswordSecretRef",
				},
			},
			want: want{
				out: map[string]interface{}{
					"some_other_key": "some_other_value",
					"admin_password": "foo",
				},
			},
		},
		"MultipleNoWildcard": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "admin-password",
							Namespace: "crossplane-system",
						},
						Key: "pass",
					})).Return([]byte("foo"), nil)
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "admin-key",
							Namespace: "crossplane-system",
						},
						Key: "key",
					})).Return([]byte("bar"), nil)
				},
				from: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"forProvider": map[string]interface{}{
								"adminPasswordSecretRef": map[string]interface{}{
									"name":      "admin-password",
									"namespace": "crossplane-system",
									"key":       "pass",
								},
								"adminKeySecretRef": map[string]interface{}{
									"name":      "admin-key",
									"namespace": "crossplane-system",
									"key":       "key",
								},
							},
						},
					},
				},
				into: map[string]interface{}{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "adminPasswordSecretRef",
					"admin_key":      "adminKeySecretRef",
				},
			},
			want: want{
				out: map[string]interface{}{
					"some_other_key": "some_other_value",
					"admin_password": "foo",
					"admin_key":      "bar",
				},
			},
		},
		"MultipleWithWildcard": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "admin-password",
							Namespace: "crossplane-system",
						},
						Key: "pass",
					})).Return([]byte("foo"), nil)
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "system-password",
							Namespace: "crossplane-system",
						},
						Key: "pass",
					})).Return([]byte("bar"), nil)
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "maintenance-password",
							Namespace: "crossplane-system",
						},
						Key: "pass",
					})).Return([]byte("baz"), nil)
				},
				from: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"forProvider": map[string]interface{}{
								"databaseUsers": []interface{}{
									map[string]interface{}{
										"name": "admin",
										"passwordSecretRef": map[string]interface{}{
											"name":      "admin-password",
											"namespace": "crossplane-system",
											"key":       "pass",
										},
										"displayName": "Administrator",
									},
									map[string]interface{}{
										"name": "system",
										"passwordSecretRef": map[string]interface{}{
											"name":      "system-password",
											"namespace": "crossplane-system",
											"key":       "pass",
										},
										"displayName": "System",
									},
									map[string]interface{}{
										"name": "maintenance",
										"passwordSecretRef": map[string]interface{}{
											"name":      "maintenance-password",
											"namespace": "crossplane-system",
											"key":       "pass",
										},
										"displayName": "Maintenance",
									},
								},
							},
						},
					},
				},
				into: map[string]interface{}{
					"some_other_key": "some_other_value",
					"database_users": []interface{}{
						map[string]interface{}{
							"name":         "admin",
							"display_name": "Administrator",
						},
						map[string]interface{}{
							"name":         "system",
							"display_name": "System",
						},
						map[string]interface{}{
							"name":         "maintenance",
							"display_name": "Maintenance",
						},
					},
				},
				mapping: map[string]string{
					"database_users[*].password": "databaseUsers[*].passwordSecretRef",
				},
			},
			want: want{
				out: map[string]interface{}{
					"some_other_key": "some_other_value",
					"database_users": []interface{}{
						map[string]interface{}{
							"name":         "admin",
							"password":     "foo",
							"display_name": "Administrator",
						},
						map[string]interface{}{
							"name":         "system",
							"password":     "bar",
							"display_name": "System",
						},
						map[string]interface{}{
							"name":         "maintenance",
							"password":     "baz",
							"display_name": "Maintenance",
						},
					},
				},
			},
		},
	}
	for name, tc := range cases {
		ctrl := gomock.NewController(t)
		m := mocks.NewMockSecretClient(ctrl)

		tc.args.clientFn(m)
		t.Run(name, func(t *testing.T) {
			gotErr := GetSensitiveParameters(context.Background(), m, tc.args.from, tc.args.into, tc.args.mapping)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("GetSensitiveParameters(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, tc.args.into); diff != "" {
				t.Errorf("GetSensitiveParameters(...) out = %v, want %v", tc.args.into, tc.want.out)
			}
		})
	}
}

func TestGetSensitiveObservation(t *testing.T) {
	connSecretRef := &xpv1.SecretReference{
		Name:      "connection-details",
		Namespace: "crossplane-system",
	}
	type args struct {
		clientFn func(client *mocks.MockSecretClient)
		into     map[string]interface{}
	}
	type want struct {
		out map[string]interface{}
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"SingleNoWildcard": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretData(gomock.Any(), connSecretRef).
						Return(map[string][]byte{
							"admin_password": []byte("foo"),
						}, nil)
				},
				into: map[string]interface{}{
					"some_other_key": "some_other_value",
				},
			},
			want: want{
				out: map[string]interface{}{
					"some_other_key": "some_other_value",
					"admin_password": "foo",
				},
			},
		},
		"MultipleNoWildcard": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().
						GetSecretData(gomock.Any(), connSecretRef).
						Return(map[string][]byte{
							"admin_password":    []byte("foo"),
							"admin_private_key": []byte("bar"),
						}, nil)
				},
				into: map[string]interface{}{
					"some_other_key": "some_other_value",
				},
			},
			want: want{
				out: map[string]interface{}{
					"some_other_key":    "some_other_value",
					"admin_password":    "foo",
					"admin_private_key": "bar",
				},
			},
		},
		"MultipleWithWildcard": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretData(gomock.Any(), connSecretRef).
						Return(map[string][]byte{
							"database_users[0].password": []byte("foo"),
							"database_users[1].password": []byte("bar"),
							"database_users[2].password": []byte("baz"),
						}, nil)
				},
				into: map[string]interface{}{
					"some_other_key": "some_other_value",
					"database_users": []interface{}{
						map[string]interface{}{
							"name":         "admin",
							"display_name": "Administrator",
						},
						map[string]interface{}{
							"name":         "system",
							"display_name": "System",
						},
						map[string]interface{}{
							"name":         "maintenance",
							"display_name": "Maintenance",
						},
					},
				},
			},
			want: want{
				out: map[string]interface{}{
					"some_other_key": "some_other_value",
					"database_users": []interface{}{
						map[string]interface{}{
							"name":         "admin",
							"password":     "foo",
							"display_name": "Administrator",
						},
						map[string]interface{}{
							"name":         "system",
							"password":     "bar",
							"display_name": "System",
						},
						map[string]interface{}{
							"name":         "maintenance",
							"password":     "baz",
							"display_name": "Maintenance",
						},
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			m := mocks.NewMockSecretClient(ctrl)

			tc.args.clientFn(m)
			gotErr := GetSensitiveObservation(context.Background(), m, connSecretRef, tc.args.into)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("GetSensitiveObservation(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, tc.args.into); diff != "" {
				t.Errorf("GetSensitiveObservation(...) out = %v, want %v", tc.args.into, tc.want.out)
			}
		})
	}
}
