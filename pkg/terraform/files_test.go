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

package terraform

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/meta"
	xpfake "github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/crossplane/terrajet/pkg/config"
	"github.com/crossplane/terrajet/pkg/resource"
	"github.com/crossplane/terrajet/pkg/resource/fake"
)

const (
	dir = "random-dir"
)

func TestWriteTFState(t *testing.T) {
	type args struct {
		tr resource.Terraformed
		s  Setup
	}
	type want struct {
		tfstate string
		err     error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"Success": {
			reason: "Standard resources should be able to write everything it has into maintf file",
			args: args{tr: &fake.Terraformed{
				Managed: xpfake.Managed{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							resource.AnnotationKeyPrivateRawAttribute: "privateraw",
							meta.AnnotationKeyExternalName:            "some-id",
						},
					},
				},
				Parameterizable: fake.Parameterizable{Parameters: map[string]interface{}{
					"param": "paramval",
				}},
				Observable: fake.Observable{Observation: map[string]interface{}{
					"obs": "obsval",
				}},
			}},
			want: want{
				tfstate: `{"version":4,"terraform_version":"","serial":1,"lineage":"","outputs":null,"resources":[{"mode":"managed","type":"","name":"","provider":"provider[\"registry.terraform.io/\"]","instances":[{"schema_version":0,"attributes":{"id":"some-id","name":"some-id","obs":"obsval","param":"paramval"},"private":"cHJpdmF0ZXJhdw=="}]}]}`,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			ctx := context.TODO()
			fp, err := NewFileProducer(ctx, nil, dir, tc.args.tr, tc.args.s, config.DefaultResource("terrajet_resource", nil), WithFileSystem(fs))
			if err != nil {
				t.Errorf("cannot initialize a file producer: %s", err.Error())
			}
			err = fp.WriteTFState(ctx)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nWriteTFState(...): -want error, +got error:\n%s", tc.reason, diff)
			}
			s, _ := afero.Afero{Fs: fs}.ReadFile(filepath.Join(dir, "terraform.tfstate"))
			if diff := cmp.Diff(tc.want.tfstate, string(s)); diff != "" {
				t.Errorf("\n%s\nWriteTFState(...): -want tfstate, +got tfstate:\n%s", tc.reason, diff)
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
					Parameterizable: fake.Parameterizable{Parameters: map[string]interface{}{
						"param": "paramval",
					}},
					Observable: fake.Observable{Observation: map[string]interface{}{
						"obs": "obsval",
					}},
				},
				cfg: config.DefaultResource("terrajet_resource", nil, func(r *config.Resource) {
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
					Env:           nil,
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
					Parameterizable: fake.Parameterizable{Parameters: map[string]interface{}{
						"param": "paramval",
					}},
					Observable: fake.Observable{Observation: map[string]interface{}{
						"obs": "obsval",
					}},
				},
				cfg: config.DefaultResource("terrajet_resource", nil),
				s: Setup{
					Requirement: ProviderRequirement{
						Source:  "hashicorp/provider-test",
						Version: "1.2.3",
					},
					Configuration: nil,
					Env:           nil,
				},
			},
			want: want{
				maintf: `{"provider":{"provider-test":null},"resource":{"":{"":{"lifecycle":{"prevent_destroy":true},"name":"some-id","param":"paramval"}}},"terraform":{"required_providers":{"provider-test":{"source":"hashicorp/provider-test","version":"1.2.3"}}}}`,
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
