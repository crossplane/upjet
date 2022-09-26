/*
Copyright 2021 Upbound Inc.
*/

package terraform

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/meta"
	xpfake "github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/resource/fake"
	"github.com/upbound/upjet/pkg/resource/json"
)

const (
	dir = "random-dir"
)

func TestEnsureTFState(t *testing.T) {
	type args struct {
		tr  resource.Terraformed
		cfg *config.Resource
		s   Setup
		fs  func() afero.Afero
	}
	type want struct {
		tfstate string
		err     error
	}
	empty := `{"version":4,"terraform_version":"","serial":1,"lineage":"","outputs":null,"resources":[]}`
	now := metav1.Now()
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"SuccessWrite": {
			reason: "Standard resources should be able to write everything it has into tfstate file when state is empty",
			args: args{
				tr: &fake.Terraformed{
					Managed: xpfake.Managed{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								resource.AnnotationKeyPrivateRawAttribute: "privateraw",
								meta.AnnotationKeyExternalName:            "some-id",
							},
						},
					},
					Parameterizable: fake.Parameterizable{Parameters: map[string]any{
						"param": "paramval",
					}},
					Observable: fake.Observable{Observation: map[string]any{
						"obs": "obsval",
					}},
				},
				cfg: config.DefaultResource("upjet_resource", nil, nil),
				fs: func() afero.Afero {
					return afero.Afero{Fs: afero.NewMemMapFs()}
				},
			},
			want: want{
				tfstate: `{"version":4,"terraform_version":"","serial":1,"lineage":"","outputs":null,"resources":[{"mode":"managed","type":"","name":"","provider":"provider[\"registry.terraform.io/\"]","instances":[{"schema_version":0,"attributes":{"id":"some-id","name":"some-id","obs":"obsval","param":"paramval"},"private":"cHJpdmF0ZXJhdw=="}]}]}`,
			},
		},
		"SuccessWithTimeout": {
			reason: "Configured timeouts should be reflected tfstate as private meta",
			args: args{
				tr: &fake.Terraformed{
					Managed: xpfake.Managed{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								resource.AnnotationKeyPrivateRawAttribute: "{}",
								meta.AnnotationKeyExternalName:            "some-id",
							},
						},
					},
					Parameterizable: fake.Parameterizable{Parameters: map[string]any{
						"param": "paramval",
					}},
					Observable: fake.Observable{Observation: map[string]any{
						"obs": "obsval",
					}},
				},
				cfg: config.DefaultResource("upjet_resource", nil, nil, func(r *config.Resource) {
					r.OperationTimeouts.Read = 2 * time.Minute
				}),
				fs: func() afero.Afero {
					return afero.Afero{Fs: afero.NewMemMapFs()}
				},
			},
			want: want{
				tfstate: `{"version":4,"terraform_version":"","serial":1,"lineage":"","outputs":null,"resources":[{"mode":"managed","type":"","name":"","provider":"provider[\"registry.terraform.io/\"]","instances":[{"schema_version":0,"attributes":{"id":"some-id","name":"some-id","obs":"obsval","param":"paramval"},"private":"eyJlMmJmYjczMC1lY2FhLTExZTYtOGY4OC0zNDM2M2JjN2M0YzAiOnsicmVhZCI6MTIwMDAwMDAwMDAwfX0="}]}]}`,
			},
		},
		"SuccessSkipDuringDeletion": {
			reason: "During an ongoing deletion, tfstate file should not be touched since its emptiness signals success.",
			args: args{
				tr: &fake.Terraformed{
					Managed: xpfake.Managed{
						ObjectMeta: metav1.ObjectMeta{
							DeletionTimestamp: &now,
							Annotations: map[string]string{
								resource.AnnotationKeyPrivateRawAttribute: "privateraw",
								meta.AnnotationKeyExternalName:            "some-id",
							},
						},
					},
					Parameterizable: fake.Parameterizable{Parameters: map[string]any{
						"param": "paramval",
					}},
					Observable: fake.Observable{Observation: map[string]any{
						"obs": "obsval",
					}},
				},
				cfg: config.DefaultResource("upjet_resource", nil, nil),
				fs: func() afero.Afero {
					fss := afero.Afero{Fs: afero.NewMemMapFs()}
					_ = fss.WriteFile(filepath.Join(dir, "terraform.tfstate"), []byte(empty), 0600)
					return fss
				},
			},
			want: want{
				tfstate: empty,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.TODO()
			files := tc.args.fs()
			fp, err := NewFileProducer(ctx, nil, dir, tc.args.tr, tc.args.s, tc.args.cfg, WithFileSystem(files))
			if err != nil {
				t.Errorf("cannot initialize a file producer: %s", err.Error())
			}
			err = fp.EnsureTFState(ctx)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nWriteTFState(...): -want error, +got error:\n%s", tc.reason, diff)
			}
			s, _ := files.ReadFile(filepath.Join(dir, "terraform.tfstate"))
			if diff := cmp.Diff(tc.want.tfstate, string(s)); diff != "" {
				t.Errorf("\n%s\nWriteTFState(...): -want tfstate, +got tfstate:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestIsStateEmpty(t *testing.T) {
	type args struct {
		fs func() afero.Afero
	}
	type want struct {
		empty bool
		err   error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"FileDoesNotExist": {
			reason: "If the tfstate file is not there, it should return true.",
			args: args{
				fs: func() afero.Afero {
					return afero.Afero{Fs: afero.NewMemMapFs()}
				},
			},
			want: want{
				empty: true,
			},
		},
		"NoAttributes": {
			reason: "If there is no attributes, that means the state is empty.",
			args: args{
				fs: func() afero.Afero {
					f := afero.Afero{Fs: afero.NewMemMapFs()}
					s := json.NewStateV4()
					s.Resources = []json.ResourceStateV4{}
					d, _ := json.JSParser.Marshal(s)
					_ = f.WriteFile(filepath.Join(dir, "terraform.tfstate"), d, 0600)
					return f
				},
			},
			want: want{
				empty: true,
			},
		},
		"NoID": {
			reason: "If there is no ID in the state, that means state is empty",
			args: args{
				fs: func() afero.Afero {
					f := afero.Afero{Fs: afero.NewMemMapFs()}
					s := json.NewStateV4()
					s.Resources = []json.ResourceStateV4{
						{
							Instances: []json.InstanceObjectStateV4{
								{
									AttributesRaw: []byte(`{}`),
								},
							},
						},
					}
					d, _ := json.JSParser.Marshal(s)
					_ = f.WriteFile(filepath.Join(dir, "terraform.tfstate"), d, 0600)
					return f
				},
			},
			want: want{
				empty: true,
			},
		},
		"NonStringID": {
			reason: "If the ID is there but not string, return true.",
			args: args{
				fs: func() afero.Afero {
					f := afero.Afero{Fs: afero.NewMemMapFs()}
					s := json.NewStateV4()
					s.Resources = []json.ResourceStateV4{
						{
							Instances: []json.InstanceObjectStateV4{
								{
									AttributesRaw: []byte(`{"id": 0}`),
								},
							},
						},
					}
					d, _ := json.JSParser.Marshal(s)
					_ = f.WriteFile(filepath.Join(dir, "terraform.tfstate"), d, 0600)
					return f
				},
			},
			want: want{
				err: errors.Errorf(errFmtNonString, fmt.Sprint(0)),
			},
		},
		"NotEmpty": {
			reason: "If there is a string ID at minimum, state file is workable",
			args: args{
				fs: func() afero.Afero {
					f := afero.Afero{Fs: afero.NewMemMapFs()}
					s := json.NewStateV4()
					s.Resources = []json.ResourceStateV4{
						{
							Instances: []json.InstanceObjectStateV4{
								{
									AttributesRaw: []byte(`{"id": "someid"}`),
								},
							},
						},
					}
					d, _ := json.JSParser.Marshal(s)
					_ = f.WriteFile(filepath.Join(dir, "terraform.tfstate"), d, 0600)
					return f
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fp, _ := NewFileProducer(
				context.TODO(),
				nil,
				dir,
				&fake.Terraformed{
					Parameterizable: fake.Parameterizable{Parameters: map[string]any{}},
				},
				Setup{},
				config.DefaultResource("upjet_resource", nil, nil), WithFileSystem(tc.args.fs()),
			)
			empty, err := fp.isStateEmpty()
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nisStateEmpty(...): -want error, +got error:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.empty, empty); diff != "" {
				t.Errorf("\n%s\nisStateEmpty(...): -want empty, +got empty:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestWriteMainTF(t *testing.T) {
	type args struct {
		tr  resource.Terraformed
		cfg *config.Resource
		s   Setup
	}
	type want struct {
		maintf string
		err    error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"TimeoutsConfigured": {
			reason: "Configured resources should be able to write everything it has into maintf file",
			args: args{
				tr: &fake.Terraformed{
					Managed: xpfake.Managed{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								resource.AnnotationKeyPrivateRawAttribute: "privateraw",
								meta.AnnotationKeyExternalName:            "some-id",
							},
						},
					},
					Parameterizable: fake.Parameterizable{Parameters: map[string]any{
						"param": "paramval",
					}},
					Observable: fake.Observable{Observation: map[string]any{
						"obs": "obsval",
					}},
				},
				cfg: config.DefaultResource("upjet_resource", nil, nil, func(r *config.Resource) {
					r.OperationTimeouts = config.OperationTimeouts{
						Read:   30 * time.Second,
						Update: 2 * time.Minute,
					}
				}),
				s: Setup{
					Requirement: ProviderRequirement{
						Source:  "hashicorp/provider-test",
						Version: "1.2.3",
					},
					Configuration: nil,
				},
			},
			want: want{
				maintf: `{"provider":{"provider-test":null},"resource":{"":{"":{"lifecycle":{"prevent_destroy":true},"name":"some-id","param":"paramval","timeouts":{"read":"30s","update":"2m0s"}}}},"terraform":{"required_providers":{"provider-test":{"source":"hashicorp/provider-test","version":"1.2.3"}}}}`,
			},
		},
		"Success": {
			reason: "Standard resources should be able to write everything it has into maintf file",
			args: args{
				tr: &fake.Terraformed{
					Managed: xpfake.Managed{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								resource.AnnotationKeyPrivateRawAttribute: "privateraw",
								meta.AnnotationKeyExternalName:            "some-id",
							},
						},
					},
					Parameterizable: fake.Parameterizable{Parameters: map[string]any{
						"param": "paramval",
					}},
					Observable: fake.Observable{Observation: map[string]any{
						"obs": "obsval",
					}},
				},
				cfg: config.DefaultResource("upjet_resource", nil, nil),
				s: Setup{
					Requirement: ProviderRequirement{
						Source:  "hashicorp/provider-test",
						Version: "1.2.3",
					},
					Configuration: nil,
				},
			},
			want: want{
				maintf: `{"provider":{"provider-test":null},"resource":{"":{"":{"lifecycle":{"prevent_destroy":true},"name":"some-id","param":"paramval"}}},"terraform":{"required_providers":{"provider-test":{"source":"hashicorp/provider-test","version":"1.2.3"}}}}`,
			},
		},
		"Custom Source": {
			reason: "Custom source like my-company/namespace/provider-test resources should be able to write everything it has into maintf file",
			args: args{
				tr: &fake.Terraformed{
					Managed: xpfake.Managed{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								resource.AnnotationKeyPrivateRawAttribute: "privateraw",
								meta.AnnotationKeyExternalName:            "some-id",
							},
						},
					},
					Parameterizable: fake.Parameterizable{Parameters: map[string]any{
						"param": "paramval",
					}},
					Observable: fake.Observable{Observation: map[string]any{
						"obs": "obsval",
					}},
				},
				cfg: config.DefaultResource("upjet_resource", nil, nil),
				s: Setup{
					Requirement: ProviderRequirement{
						Source:  "my-company/namespace/provider-test",
						Version: "1.2.3",
					},
					Configuration: nil,
				},
			},
			want: want{
				maintf: `{"provider":{"provider-test":null},"resource":{"":{"":{"lifecycle":{"prevent_destroy":true},"name":"some-id","param":"paramval"}}},"terraform":{"required_providers":{"provider-test":{"source":"my-company/namespace/provider-test","version":"1.2.3"}}}}`,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			fp, err := NewFileProducer(context.TODO(), nil, dir, tc.args.tr, tc.args.s, tc.args.cfg, WithFileSystem(fs))
			if err != nil {
				t.Errorf("cannot initialize a file producer: %s", err.Error())
			}
			err = fp.WriteMainTF()
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nWriteMainTF(...): -want error, +got error:\n%s", tc.reason, diff)
			}
			s, _ := afero.Afero{Fs: fs}.ReadFile(filepath.Join(dir, "main.tf.json"))
			if diff := cmp.Diff(tc.want.maintf, string(s)); diff != "" {
				t.Errorf("\n%s\nWriteMainTF(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}
