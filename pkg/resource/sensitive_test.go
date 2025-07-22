// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package resource

import (
	"context"
	"testing"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/resource/fake"
	"github.com/crossplane/upjet/pkg/resource/fake/mocks"
	"github.com/crossplane/upjet/pkg/resource/json"
)

var (
	testData = []byte(`
{
  "top_level_optional": null,
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
    },
    {
        "inner_optional": null
    }
  ]
}
`)
	errBoom = errors.New("boom")
)

type secretKeySelectorModifier func(s *xpv1.SecretKeySelector)

func secretKeySelectorWithKey(v string) secretKeySelectorModifier {
	return func(s *xpv1.SecretKeySelector) {
		s.Key = v
	}
}

func secretKeySelectorWithSecretReference(v xpv1.SecretReference) secretKeySelectorModifier {
	return func(s *xpv1.SecretKeySelector) {
		s.SecretReference = v
	}
}

func secretKeySelector(sm ...secretKeySelectorModifier) *xpv1.SecretKeySelector {
	s := &xpv1.SecretKeySelector{}
	for _, m := range sm {
		m(s)
	}
	return s
}

type fakeManaged struct {
	*unstructured.Unstructured
	xpfake.Manageable
	xpv1.ConditionedStatus
}

func TestGetConnectionDetails(t *testing.T) {
	type args struct {
		tr   Terraformed
		cfg  *config.Resource
		data map[string]any
	}
	type want struct {
		out managed.ConnectionDetails
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"NoConnectionDetails": {
			args: args{
				tr:  &fake.Terraformed{},
				cfg: config.DefaultResource("upjet_resource", nil, nil, nil),
			},
		},
		"OnlyDefaultConnectionDetails": {
			args: args{
				tr: &fake.Terraformed{
					MetadataProvider: fake.MetadataProvider{
						ConnectionDetailsMapping: map[string]string{
							"top_level_secret": "some.field",
						},
					},
				},
				cfg: config.DefaultResource("upjet_resource", nil, nil, nil),
				data: map[string]any{
					"top_level_secret": "sensitive-data-top-level-secret",
				},
			},
			want: want{
				out: map[string][]byte{
					"attribute.top_level_secret": []byte("sensitive-data-top-level-secret"),
				},
			},
		},
		"SecretList": {
			args: args{
				tr: &fake.Terraformed{
					MetadataProvider: fake.MetadataProvider{
						ConnectionDetailsMapping: map[string]string{
							"top_level_secrets": "status.atProvider.topLevelSecrets",
						},
					},
				},
				cfg: config.DefaultResource("upjet_resource", nil, nil, nil),
				data: map[string]any{
					"top_level_secrets": []any{
						"val1",
						"val2",
						"val3",
					},
				},
			},
			want: want{
				out: map[string][]byte{
					"attribute.top_level_secret.0": []byte("val1"),
					"attribute.top_level_secret.1": []byte("val2"),
					"attribute.top_level_secret.2": []byte("val3"),
				},
			},
		},
		"Map": {
			args: args{
				tr: &fake.Terraformed{
					MetadataProvider: fake.MetadataProvider{
						ConnectionDetailsMapping: map[string]string{
							"top_level_secrets": "status.atProvider.topLevelSecrets",
						},
					},
				},
				cfg: config.DefaultResource("upjet_resource", nil, nil, nil),
				data: map[string]any{
					"top_level_secrets": map[string]any{
						"key1": "val1",
						"key2": "val2",
						"key3": "val3",
					},
				},
			},
			want: want{
				out: map[string][]byte{
					"attribute.top_level_secret.key1": []byte("val1"),
					"attribute.top_level_secret.key2": []byte("val2"),
					"attribute.top_level_secret.key3": []byte("val3"),
				},
			},
		},
		"OnlyAdditionalConnectionDetails": {
			args: args{
				tr: &fake.Terraformed{},
				cfg: &config.Resource{
					Sensitive: config.Sensitive{
						AdditionalConnectionDetailsFn: func(attr map[string]any) (map[string][]byte, error) {
							return map[string][]byte{
								"top_level_secret_custom": []byte(attr["top_level_secret"].(string)),
							}, nil
						},
					},
				},
				data: map[string]any{
					"top_level_secret": "sensitive-data-top-level-secret",
				},
			},
			want: want{
				out: map[string][]byte{
					"top_level_secret_custom": []byte("sensitive-data-top-level-secret"),
				},
			},
		},
		"AdditionalConnectionDetailsFailed": {
			args: args{
				tr: &fake.Terraformed{},
				cfg: &config.Resource{
					Sensitive: config.Sensitive{
						AdditionalConnectionDetailsFn: func(attr map[string]any) (map[string][]byte, error) {
							return nil, errBoom
						},
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errGetAdditionalConnectionDetails),
			},
		},
		"CannotOverrideExistingKey": {
			args: args{
				tr: &fake.Terraformed{
					MetadataProvider: fake.MetadataProvider{
						ConnectionDetailsMapping: map[string]string{
							"top_level_secret": "some.field",
						},
					},
				},
				cfg: &config.Resource{
					Sensitive: config.Sensitive{
						AdditionalConnectionDetailsFn: func(attr map[string]any) (map[string][]byte, error) {
							return map[string][]byte{
								"attribute.top_level_secret": []byte(""),
							}, nil
						},
					},
				},
				data: map[string]any{
					"id":               "secret-id",
					"top_level_secret": "sensitive-data-top-level-secret",
				},
			},
			want: want{
				err: errors.Errorf(errFmtCannotOverrideExistingKey, "attribute.top_level_secret"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := GetConnectionDetails(tc.data, tc.tr, tc.cfg)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("GetConnectionDetails(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("\nGetConnectionDetails(...): -want error, +got error:\n%s", diff)
			}
		})
	}
}

func TestGetSensitiveAttributes(t *testing.T) {
	testInput := map[string]any{}
	if err := json.JSParser.Unmarshal(testData, &testInput); err != nil {
		t.Fatalf("cannot unmarshall test data: %v", err)
	}
	type args struct {
		paths map[string]string
		data  map[string]any
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
					prefixAttribute + "top_level_secret": []byte("sensitive-data-top-level-secret"),
				},
			},
		},
		"Optional": {
			args: args{
				paths: map[string]string{"top_level_optional": ""},
				data:  testInput,
			},
			want: want{
				out: nil,
			},
		},
		"SingleNonExisting": {
			args: args{
				paths: map[string]string{"missing_field": ""},
				data:  testInput,
			},
		},
		"SingleGettingNumber": {
			args: args{
				paths: map[string]string{"top_object_with_number[key1]": ""},
				data:  testInput,
			},
			want: want{
				err: errors.Errorf(errFmtCannotGetStringForFieldPath, "top_object_with_number.key1"),
			},
		},
		"WildcardMultipleFromMap": {
			args: args{
				paths: map[string]string{"top_config_secretmap[*]": ""},
				data:  testInput,
			},
			want: want{
				out: map[string][]byte{
					prefixAttribute + "top_config_secretmap...inner_config_secretmap.first...": []byte("sensitive-data-inner-first"),
					prefixAttribute + "top_config_secretmap.inner_config_secretmap_second":     []byte("sensitive-data-inner-second"),
					prefixAttribute + "top_config_secretmap.inner_config_secretmap_third":      []byte("sensitive-data-inner-third"),
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
					prefixAttribute + "top_config_array.0.inner_some_field": []byte("non-sensitive-data-1"),
					prefixAttribute + "top_config_array.1.inner_some_field": []byte("non-sensitive-data-2"),
					prefixAttribute + "top_config_array.2.inner_some_field": []byte("non-sensitive-data-3"),
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
					prefixAttribute + "top_config_array.0.inner_config_array.0.bottom_level_secret": []byte("sensitive-data-bottom-level-1"),
					prefixAttribute + "top_config_array.2.inner_config_array.0.bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					prefixAttribute + "top_config_array.2.inner_config_array.1.bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
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
					prefixAttribute + "top_config_array.2.inner_config_array.0.bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					prefixAttribute + "top_config_array.2.inner_config_array.1.bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
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
					prefixAttribute + "top_level_secret":                                            []byte("sensitive-data-top-level-secret"),
					prefixAttribute + "top_config_secretmap...inner_config_secretmap.first...":      []byte("sensitive-data-inner-first"),
					prefixAttribute + "top_config_secretmap.inner_config_secretmap_second":          []byte("sensitive-data-inner-second"),
					prefixAttribute + "top_config_secretmap.inner_config_secretmap_third":           []byte("sensitive-data-inner-third"),
					prefixAttribute + "top_config_array.2.inner_config_array.0.bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					prefixAttribute + "top_config_array.2.inner_config_array.1.bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
				},
			},
		},
		"NotAValue": {
			args: args{
				paths: map[string]string{"inner_optional": ""},
				data:  testInput,
			},
			want: want{
				out: nil,
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
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := GetSensitiveAttributes(tc.data, tc.paths)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("GetFields(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("\nGetSensitiveAttributes(...): -want error, +got error:\n%s", diff)
			}
		})
	}
}

func TestGetSensitiveParameters(t *testing.T) {
	type args struct {
		clientFn func(client *mocks.MockSecretClient)
		from     resource.Managed
		into     map[string]any
		mapping  map[string]string
	}
	type want struct {
		out map[string]any
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"NoSensitiveData": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"forProvider": map[string]any{
									"adminPasswordSecretRef": nil,
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "spec.forProvider.adminPasswordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
				},
			},
		},
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
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"forProvider": map[string]any{
									"adminPasswordSecretRef": map[string]any{
										"key":       "pass",
										"name":      "admin-password",
										"namespace": "crossplane-system",
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "spec.forProvider.adminPasswordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"admin_password": "foo",
				},
			},
		},
		"SingleNoWildcardWithNoSecret": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "admin-password",
							Namespace: "crossplane-system",
						},
						Key: "pass",
					})).Return([]byte(""), kerrors.NewNotFound(v1.Resource("secret"), "admin-password"))
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"forProvider": map[string]any{
									"adminPasswordSecretRef": map[string]any{
										"key":       "pass",
										"name":      "admin-password",
										"namespace": "crossplane-system",
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "spec.forProvider.adminPasswordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"admin_password": "",
				},
			},
		},
		"SingleNoWildcardWithSlice": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "db-passwords",
							Namespace: "crossplane-system",
						},
						Key: "admin",
					})).Return([]byte("admin_pwd"), nil)
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "db-passwords",
							Namespace: "crossplane-system",
						},
						Key: "system",
					})).Return([]byte("system_pwd"), nil)
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"forProvider": map[string]any{
									"passwordsSecretRef": []any{
										secretKeySelector(
											secretKeySelectorWithKey("admin"),
											secretKeySelectorWithSecretReference(xpv1.SecretReference{
												Name:      "db-passwords",
												Namespace: "crossplane-system",
											}),
										),
										secretKeySelector(
											secretKeySelectorWithKey("system"),
											secretKeySelectorWithSecretReference(xpv1.SecretReference{
												Name:      "db-passwords",
												Namespace: "crossplane-system",
											}),
										),
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"db_passwords": "spec.forProvider.passwordsSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"db_passwords": []any{
						"admin_pwd",
						"system_pwd",
					},
				},
			},
		},
		"SingleNoWildcardWithSecretReference": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretData(gomock.Any(), gomock.Eq(&xpv1.SecretReference{
						Name:      "db-passwords",
						Namespace: "crossplane-system",
					})).Return(map[string][]byte{"admin": []byte("admin_pwd"), "system": []byte("system_pwd")}, nil)
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"forProvider": map[string]any{
									"dbPasswordsSecretRef": map[string]any{
										"name":      "db-passwords",
										"namespace": "crossplane-system",
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"db_passwords": "spec.forProvider.dbPasswordsSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"db_passwords": map[string]any{
						"admin":  "admin_pwd",
						"system": "system_pwd",
					},
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
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"forProvider": map[string]any{
									"adminPasswordSecretRef": map[string]any{
										"key":       "pass",
										"name":      "admin-password",
										"namespace": "crossplane-system",
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "spec.forProvider.adminPasswordSecretRef",
					"admin_key":      "spec.forProvider.adminKeySecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"admin_password": "foo",
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
							Name:      "maintenance-password",
							Namespace: "crossplane-system",
						},
						Key: "pass",
					})).Return([]byte("baz"), nil)
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"forProvider": map[string]any{
									"databaseUsers": []any{
										map[string]any{
											"name": "admin",
											"passwordSecretRef": map[string]any{
												"key":       "pass",
												"name":      "admin-password",
												"namespace": "crossplane-system",
											},
											"displayName": "Administrator",
										},
										map[string]any{
											"name": "system",
											// Intentionally skip providing this optional parameter
											// to test the behaviour when an optional parameter
											// not provided.
											/*"passwordSecretRef": map[string]any{
												"name":      "system-password",
												"namespace": "crossplane-system",
												"key":       "pass",
											},*/
											"displayName": "System",
										},
										map[string]any{
											"name": "maintenance",
											"passwordSecretRef": map[string]any{
												"key":       "pass",
												"name":      "maintenance-password",
												"namespace": "crossplane-system",
											},
											"displayName": "Maintenance",
										},
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
					"database_users": []any{
						map[string]any{
							"name":         "admin",
							"display_name": "Administrator",
						},
						map[string]any{
							"name":         "system",
							"display_name": "System",
						},
						map[string]any{
							"name":         "maintenance",
							"display_name": "Maintenance",
						},
					},
				},
				mapping: map[string]string{
					"database_users[*].password": "spec.forProvider.databaseUsers[*].passwordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"database_users": []any{
						map[string]any{
							"name":         "admin",
							"password":     "foo",
							"display_name": "Administrator",
						},
						map[string]any{
							"name":         "system",
							"display_name": "System",
						},
						map[string]any{
							"name":         "maintenance",
							"password":     "baz",
							"display_name": "Maintenance",
						},
					},
				},
			},
		},
		// spec.initProvider tests
		"NoSensitiveDataInitProvider": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"initProvider": map[string]any{
									"adminPasswordSecretRef": nil,
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "spec.forProvider.adminPasswordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
				},
			},
		},
		"SingleNoWildcardInitProvider": {
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
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"initProvider": map[string]any{
									"adminPasswordSecretRef": map[string]any{
										"key":       "pass",
										"name":      "admin-password",
										"namespace": "crossplane-system",
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "spec.forProvider.adminPasswordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"admin_password": "foo",
				},
			},
		},
		"SingleNoWildcardWithNoSecretInitProvider": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "admin-password",
							Namespace: "crossplane-system",
						},
						Key: "pass",
					})).Return([]byte(""), kerrors.NewNotFound(v1.Resource("secret"), "admin-password"))
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"initProvider": map[string]any{
									"adminPasswordSecretRef": map[string]any{
										"key":       "pass",
										"name":      "admin-password",
										"namespace": "crossplane-system",
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "spec.forProvider.adminPasswordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"admin_password": "",
				},
			},
		},
		"SingleNoWildcardWithSliceInitProvider": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "db-passwords",
							Namespace: "crossplane-system",
						},
						Key: "admin",
					})).Return([]byte("admin_pwd"), nil)
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "db-passwords",
							Namespace: "crossplane-system",
						},
						Key: "system",
					})).Return([]byte("system_pwd"), nil)
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"initProvider": map[string]any{
									"passwordsSecretRef": []any{
										secretKeySelector(
											secretKeySelectorWithKey("admin"),
											secretKeySelectorWithSecretReference(xpv1.SecretReference{
												Name:      "db-passwords",
												Namespace: "crossplane-system",
											}),
										),
										secretKeySelector(
											secretKeySelectorWithKey("system"),
											secretKeySelectorWithSecretReference(xpv1.SecretReference{
												Name:      "db-passwords",
												Namespace: "crossplane-system",
											}),
										),
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"db_passwords": "spec.forProvider.passwordsSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"db_passwords": []any{
						"admin_pwd",
						"system_pwd",
					},
				},
			},
		},
		"SingleNoWildcardWithSecretReferenceInitProvider": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretData(gomock.Any(), gomock.Eq(&xpv1.SecretReference{
						Name:      "db-passwords",
						Namespace: "crossplane-system",
					})).Return(map[string][]byte{"admin": []byte("admin_pwd"), "system": []byte("system_pwd")}, nil)
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"initProvider": map[string]any{
									"dbPasswordsSecretRef": map[string]any{
										"name":      "db-passwords",
										"namespace": "crossplane-system",
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"db_passwords": "spec.forProvider.dbPasswordsSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"db_passwords": map[string]any{
						"admin":  "admin_pwd",
						"system": "system_pwd",
					},
				},
			},
		},
		"MultipleNoWildcardInitProvider": {
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
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"initProvider": map[string]any{
									"adminPasswordSecretRef": map[string]any{
										"key":       "pass",
										"name":      "admin-password",
										"namespace": "crossplane-system",
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "spec.forProvider.adminPasswordSecretRef",
					"admin_key":      "spec.forProvider.adminKeySecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"admin_password": "foo",
				},
			},
		},
		"MultipleWithWildcardInitProvider": {
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
							Name:      "maintenance-password",
							Namespace: "crossplane-system",
						},
						Key: "pass",
					})).Return([]byte("baz"), nil)
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"initProvider": map[string]any{
									"databaseUsers": []any{
										map[string]any{
											"name": "admin",
											"passwordSecretRef": map[string]any{
												"key":       "pass",
												"name":      "admin-password",
												"namespace": "crossplane-system",
											},
											"displayName": "Administrator",
										},
										map[string]any{
											"name": "system",
											// Intentionally skip providing this optional parameter
											// to test the behaviour when an optional parameter
											// not provided.
											/*"passwordSecretRef": map[string]any{
												"name":      "system-password",
												"namespace": "crossplane-system",
												"key":       "pass",
											},*/
											"displayName": "System",
										},
										map[string]any{
											"name": "maintenance",
											"passwordSecretRef": map[string]any{
												"key":       "pass",
												"name":      "maintenance-password",
												"namespace": "crossplane-system",
											},
											"displayName": "Maintenance",
										},
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
					"database_users": []any{
						map[string]any{
							"name":         "admin",
							"display_name": "Administrator",
						},
						map[string]any{
							"name":         "system",
							"display_name": "System",
						},
						map[string]any{
							"name":         "maintenance",
							"display_name": "Maintenance",
						},
					},
				},
				mapping: map[string]string{
					"database_users[*].password": "spec.forProvider.databaseUsers[*].passwordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"database_users": []any{
						map[string]any{
							"name":         "admin",
							"password":     "foo",
							"display_name": "Administrator",
						},
						map[string]any{
							"name":         "system",
							"display_name": "System",
						},
						map[string]any{
							"name":         "maintenance",
							"password":     "baz",
							"display_name": "Maintenance",
						},
					},
				},
			},
		},
		"ForProviderRefOverridesInitProviderRef": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "admin-password",
							Namespace: "crossplane-system",
						},
						Key: "pass-forprovider",
					})).Return([]byte("sensitive-forprovider"), nil)
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "admin-password",
							Namespace: "crossplane-system",
						},
						Key: "pass-initprovider",
					})).Return([]byte("sensitive-initprovider"), nil)
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"spec": map[string]any{
								"forProvider": map[string]any{
									"adminPasswordSecretRef": map[string]any{
										"key":       "pass-forprovider",
										"name":      "admin-password",
										"namespace": "crossplane-system",
									},
								},
								"initProvider": map[string]any{
									"adminPasswordSecretRef": map[string]any{
										"key":       "pass-initprovider",
										"name":      "admin-password",
										"namespace": "crossplane-system",
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "adminPasswordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"admin_password": "sensitive-forprovider",
				},
			},
		},
		// namespaced MRs test
		"NoSensitiveData_NamespacedMR": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"metadata": map[string]any{
								"name":      "some-mr",
								"namespace": "foo-ns",
							},
							"spec": map[string]any{
								"forProvider": map[string]any{
									"adminPasswordSecretRef": nil,
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"admin_password": "spec.forProvider.adminPasswordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
				},
			},
		},
		"SingleNoWildcardWithSecretReference_NamespacedMR": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretData(gomock.Any(), gomock.Eq(&xpv1.SecretReference{
						Name:      "db-passwords",
						Namespace: "foo-ns",
					})).Return(map[string][]byte{"admin": []byte("admin_pwd"), "system": []byte("system_pwd")}, nil)
					// other namespaces should return not found
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Not(gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "admin-password",
							Namespace: "crossplane-system",
						},
						Key: "pass",
					}))).Return([]byte(""), kerrors.NewNotFound(v1.Resource("secret"), "admin-password")).AnyTimes()
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"metadata": map[string]any{
								"name":      "some-mr",
								"namespace": "foo-ns",
							},
							"spec": map[string]any{
								"forProvider": map[string]any{
									"dbPasswordsSecretRef": map[string]any{
										"name": "db-passwords",
										// no namespace ref
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"db_passwords": "spec.forProvider.dbPasswordsSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"db_passwords": map[string]any{
						"admin":  "admin_pwd",
						"system": "system_pwd",
					},
				},
			},
		},
		"MultipleWithWildcardNamespacedMR": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "admin-password",
							Namespace: "foo-ns",
						},
						Key: "pass",
					})).Return([]byte("foo"), nil)
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "maintenance-password",
							Namespace: "foo-ns",
						},
						Key: "pass",
					})).Return([]byte("baz"), nil)
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"metadata": map[string]any{
								"name":      "some-mr",
								"namespace": "foo-ns",
							},
							"spec": map[string]any{
								"forProvider": map[string]any{
									"databaseUsers": []any{
										map[string]any{
											"name": "admin",
											"passwordSecretRef": map[string]any{
												"key":  "pass",
												"name": "admin-password",
											},
											"displayName": "Administrator",
										},
										map[string]any{
											"name": "system",
											// Intentionally skip providing this optional parameter
											// to test the behaviour when an optional parameter
											// not provided.
											/*"passwordSecretRef": map[string]any{
												"name":      "system-password",
												"namespace": "crossplane-system",
												"key":       "pass",
											},*/
											"displayName": "System",
										},
										map[string]any{
											"name": "maintenance",
											"passwordSecretRef": map[string]any{
												"key":  "pass",
												"name": "maintenance-password",
											},
											"displayName": "Maintenance",
										},
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
					"database_users": []any{
						map[string]any{
							"name":         "admin",
							"display_name": "Administrator",
						},
						map[string]any{
							"name":         "system",
							"display_name": "System",
						},
						map[string]any{
							"name":         "maintenance",
							"display_name": "Maintenance",
						},
					},
				},
				mapping: map[string]string{
					"database_users[*].password": "spec.forProvider.databaseUsers[*].passwordSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"database_users": []any{
						map[string]any{
							"name":         "admin",
							"password":     "foo",
							"display_name": "Administrator",
						},
						map[string]any{
							"name":         "system",
							"display_name": "System",
						},
						map[string]any{
							"name":         "maintenance",
							"password":     "baz",
							"display_name": "Maintenance",
						},
					},
				},
			},
		},
		"SingleNoWildcardWithSliceNamespacedMR": {
			args: args{
				clientFn: func(client *mocks.MockSecretClient) {
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "db-passwords",
							Namespace: "foo-ns",
						},
						Key: "admin",
					})).Return([]byte("admin_pwd"), nil)
					client.EXPECT().GetSecretValue(gomock.Any(), gomock.Eq(xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "db-passwords",
							Namespace: "foo-ns",
						},
						Key: "system",
					})).Return([]byte("system_pwd"), nil)
				},
				from: &fakeManaged{
					Unstructured: &unstructured.Unstructured{
						Object: map[string]any{
							"metadata": map[string]any{
								"name":      "some-mr",
								"namespace": "foo-ns",
							},
							"spec": map[string]any{
								"forProvider": map[string]any{
									"passwordsSecretRef": []any{
										map[string]any{
											"name": "db-passwords",
											"key":  "admin",
										},
										map[string]any{
											"name": "db-passwords",
											"key":  "system",
										},
									},
								},
							},
						},
					},
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
				mapping: map[string]string{
					"db_passwords": "spec.forProvider.passwordsSecretRef",
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"db_passwords": []any{
						"admin_pwd",
						"system_pwd",
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
		into     map[string]any
	}
	type want struct {
		out map[string]any
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
							prefixAttribute + "admin_password": []byte("foo"),
							"a_custom_key":                     []byte("t0p-s3cr3t"),
						}, nil)
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
			},
			want: want{
				out: map[string]any{
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
							prefixAttribute + "admin_password":    []byte("foo"),
							prefixAttribute + "admin_private_key": []byte("bar"),
						}, nil)
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
				},
			},
			want: want{
				out: map[string]any{
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
							prefixAttribute + "database_users.0.password": []byte("foo"),
							prefixAttribute + "database_users.1.password": []byte("bar"),
							prefixAttribute + "database_users.2.password": []byte("baz"),
							"a_custom_key": []byte("t0p-s3cr3t"),
						}, nil)
				},
				into: map[string]any{
					"some_other_key": "some_other_value",
					"database_users": []any{
						map[string]any{
							"name":         "admin",
							"display_name": "Administrator",
						},
						map[string]any{
							"name":         "system",
							"display_name": "System",
						},
						map[string]any{
							"name":         "maintenance",
							"display_name": "Maintenance",
						},
					},
				},
			},
			want: want{
				out: map[string]any{
					"some_other_key": "some_other_value",
					"database_users": []any{
						map[string]any{
							"name":         "admin",
							"password":     "foo",
							"display_name": "Administrator",
						},
						map[string]any{
							"name":         "system",
							"password":     "bar",
							"display_name": "System",
						},
						map[string]any{
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

func Test_secretKeyToFieldPath(t *testing.T) {
	type args struct {
		s string
	}
	type want struct {
		out string
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"EndIndex": {
			args{
				s: "kube_config.0",
			},
			want{
				out: "kube_config[0]",
				err: nil,
			},
		},
		"MiddleIndex": {
			args{
				s: "kube_config.0.password",
			},
			want{
				out: "kube_config[0].password",
				err: nil,
			},
		},
		"MultipleIndexes": {
			args{
				s: "kube_config.0.users.1.keys.0",
			},
			want{
				out: "kube_config[0].users[1].keys[0]",
				err: nil,
			},
		},
		"EndsKeyWithDots": {
			args{
				s: "metadata.annotations...crossplane.io/external-name...",
			},
			want{
				out: "metadata.annotations[crossplane.io/external-name]",
				err: nil,
			},
		},
		"MiddleKeyWithDots": {
			args{
				s: "users...crossplane.io/test-user....test",
			},
			want{
				out: "users[crossplane.io/test-user].test",
				err: nil,
			},
		},
		"MultipleKeysWithDots": {
			args{
				s: "users...crossplane.io/test-user....test...abc.xyz...",
			},
			want{
				out: "users[crossplane.io/test-user].test[abc.xyz]",
				err: nil,
			},
		},
		"MixedDotsAndIndexes": {
			args{
				s: "users...crossplane.io/test-user....test.0.users.3",
			},
			want{
				out: "users[crossplane.io/test-user].test[0].users[3]",
				err: nil,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := secretKeyToFieldPath(tc.args.s)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("secretKeyToFieldPath(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("secretKeyToFieldPath(...) out = %v, want %v", got, tc.want.out)
			}
		})
	}
}

func Test_fieldPathToSecretKey(t *testing.T) {
	type args struct {
		s string
	}
	type want struct {
		out string
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"EndIndex": {
			args{
				s: "kube_config[0]",
			},
			want{
				out: "kube_config.0",
				err: nil,
			},
		},
		"MiddleIndex": {
			args{
				s: "kube_config[0].password",
			},
			want{
				out: "kube_config.0.password",
				err: nil,
			},
		},
		"MultipleIndexes": {
			args{
				s: "kube_config[0].users[1].keys[0]",
			},
			want{
				out: "kube_config.0.users.1.keys.0",
				err: nil,
			},
		},
		"EndsKeyWithDots": {
			args{
				s: "metadata.annotations[crossplane.io/external-name]",
			},
			want{
				out: "metadata.annotations...crossplane.io/external-name...",
				err: nil,
			},
		},
		"MiddleKeyWithDots": {
			args{
				s: "users[crossplane.io/test-user].test",
			},
			want{
				out: "users...crossplane.io/test-user....test",
				err: nil,
			},
		},
		"MixedDotsAndIndexes": {
			args{
				s: "users[crossplane.io/test-user].test[0].users[3]",
			},
			want{
				out: "users...crossplane.io/test-user....test.0.users.3",
				err: nil,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := fieldPathToSecretKey(tc.args.s)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("secretKeyToFieldPath(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("secretKeyToFieldPath(...) out = %v, want %v", got, tc.want.out)
			}
		})
	}
}

func TestExpandedTFPath(t *testing.T) {
	type args struct {
		expandedXP string
		mapping    map[string]string
	}
	type want struct {
		expandedTF string
		err        error
	}
	cases := map[string]struct {
		args
		want
	}{
		"SameLengthWithAllWildcards": {
			args: args{
				expandedXP: "jobCluster[0].newCluster[0].dockerImage[0].basicAuth[0].passwordSecretRef",
				mapping: map[string]string{
					"job_cluster[*].new_cluster[*].docker_image[*].basic_auth[*].password": "jobCluster[*].newCluster[*].dockerImage[*].basicAuth[*].passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "job_cluster[0].new_cluster[0].docker_image[0].basic_auth[0].password",
				err:        nil,
			},
		},
		"SameLengthWithSameMiddleWildcards": {
			args: args{
				expandedXP: "jobCluster.newCluster[0].dockerImage.basicAuth[0].passwordSecretRef",
				mapping: map[string]string{
					"job_cluster.new_cluster[*].docker_image.basic_auth[*].password": "jobCluster.newCluster[*].dockerImage.basicAuth[*].passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "job_cluster.new_cluster[0].docker_image.basic_auth[0].password",
				err:        nil,
			},
		},
		"DifferentLengthWithStartWildcard": {
			args: args{
				expandedXP: "jobCluster[0].newCluster.dockerImage.basicAuth.passwordSecretRef",
				mapping: map[string]string{
					"job_cluster[*].new_cluster[*].docker_image[*].basic_auth[*].password": "jobCluster[*].newCluster.dockerImage.basicAuth.passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "job_cluster[0].new_cluster[0].docker_image[0].basic_auth[0].password",
				err:        nil,
			},
		},
		"DifferentLengthWithMiddleWildcard-1": {
			args: args{
				expandedXP: "jobCluster[0].newCluster.dockerImage.basicAuth.passwordSecretRef",
				mapping: map[string]string{
					"job_cluster[*].new_cluster.docker_image[*].basic_auth[*].password": "jobCluster[*].newCluster.dockerImage.basicAuth.passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "job_cluster[0].new_cluster.docker_image[0].basic_auth[0].password",
				err:        nil,
			},
		},
		"DifferentLengthWithMiddleWildcard-2": {
			args: args{
				expandedXP: "jobCluster.newCluster.dockerImage.basicAuth.passwordSecretRef",
				mapping: map[string]string{
					"job_cluster.new_cluster.docker_image[*].basic_auth.password": "jobCluster.newCluster.dockerImage.basicAuth.passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "job_cluster.new_cluster.docker_image[0].basic_auth.password",
				err:        nil,
			},
		},
		"DifferentLengthWithMiddleWildcard-3": {
			args: args{
				expandedXP: "jobCluster.newCluster.dockerImage[0].basicAuth.passwordSecretRef",
				mapping: map[string]string{
					"job_cluster[*].new_cluster.docker_image[*].basic_auth.password": "jobCluster.newCluster.dockerImage[*].basicAuth.passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "job_cluster[0].new_cluster.docker_image[0].basic_auth.password",
				err:        nil,
			},
		},
		"DifferentLengthWithMiddleWildcard-4": {
			args: args{
				expandedXP: "jobCluster.newCluster[0].dockerImage.basicAuth[0].passwordSecretRef",
				mapping: map[string]string{
					"job_cluster[*].new_cluster[*].docker_image.basic_auth[*].password": "jobCluster.newCluster[*].dockerImage.basicAuth[*].passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "job_cluster[0].new_cluster[0].docker_image.basic_auth[0].password",
				err:        nil,
			},
		},
		"DifferentLengthWithAllWildcard": {
			args: args{
				expandedXP: "jobCluster.newCluster.dockerImage.basicAuth.passwordSecretRef",
				mapping: map[string]string{
					"job_cluster[*].new_cluster[*].docker_image[*].basic_auth[*].password": "jobCluster.newCluster.dockerImage.basicAuth.passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "job_cluster[0].new_cluster[0].docker_image[0].basic_auth[0].password",
				err:        nil,
			},
		},
		"DifferentLengthWithNoWildcard": {
			args: args{
				expandedXP: "jobCluster.newCluster.dockerImage.basicAuth.passwordSecretRef",
				mapping: map[string]string{
					"job_cluster.new_cluster.docker_image.basic_auth.password": "jobCluster.newCluster.dockerImage.basicAuth.passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "job_cluster.new_cluster.docker_image.basic_auth.password",
				err:        nil,
			},
		},
		"WrongMapping": {
			args: args{
				expandedXP: "jobCluster.newCluster.passwordSecretRef",
				mapping: map[string]string{
					"job_cluster.new_cluster[*].docker_image[*].basic_auth.password": "jobCluster.newCluster.passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "",
				err:        errors.New("wrong mapping configuration, xp path is too short"),
			},
		},
		"ShorterTfPath": {
			args: args{
				expandedXP: "jobCluster.newCluster.dockerImage.basicAuth.passwordSecretRef",
				mapping: map[string]string{
					"job_cluster.basic_auth.password": "jobCluster.newCluster.dockerImage.basicAuth.passwordSecretRef",
				},
			},
			want: want{
				expandedTF: "",
				err:        errors.New("tf path must be longer than xp path"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			expandedTF, gotErr := expandedTFPath(tc.args.expandedXP, tc.args.mapping)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("secretKeyToFieldPath(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.expandedTF, expandedTF); diff != "" {
				t.Errorf("secretKeyToFieldPath(...) out = %v, want %v", expandedTF, tc.want.expandedTF)
			}
		})
	}
}
