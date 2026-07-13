// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package terraform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	testingexec "k8s.io/utils/exec/testing"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
)

func newTestResource(opts ...config.ResourceOption) *config.Resource {
	return config.DefaultResource("upjet_resource", &schema.Resource{
		Schema: map[string]*schema.Schema{
			"id": {Type: schema.TypeString},
		},
	}, nil, nil, opts...)
}

func newTestTerraformed(uid types.UID) *fake.LegacyTerraformed {
	return &fake.LegacyTerraformed{
		LegacyManaged: xpfake.LegacyManaged{
			ObjectMeta: metav1.ObjectMeta{
				UID: uid,
				Annotations: map[string]string{
					resource.AnnotationKeyPrivateRawAttribute: "{}",
					meta.AnnotationKeyExternalName:            "my-external-name",
				},
			},
		},
		Parameterizable: fake.Parameterizable{Parameters: map[string]any{
			"param": "val",
		}},
		Observable: fake.Observable{Observation: map[string]any{}},
	}
}

func newTestWorkspaceStore() *WorkspaceStore {
	ws := NewWorkspaceStore(
		logging.NewNopLogger(),
		WithDisableInit(true),
	)
	ws.executor = &testingexec.FakeExec{DisableScripts: true}
	return ws
}

var testSetup = Setup{
	Requirement: ProviderRequirement{
		Source:  "hashicorp/test",
		Version: "1.0.0",
	},
}

func TestWorkspaceStoreGetImportIDFn(t *testing.T) {
	errBoom := errors.New("boom")

	type args struct {
		cfg *config.Resource
	}
	type want struct {
		terraformID string
		err         error
	}

	cases := map[string]struct {
		reason string
		args
		want
	}{
		"GetImportIDFnNil_FallsBackToGetIDFn": {
			reason: "When GetImportIDFn is nil, terraformID should equal the result of GetIDFn (backward compatibility).",
			args: args{
				cfg: newTestResource(func(r *config.Resource) {
					r.ExternalName.GetIDFn = func(_ context.Context, _ string, _ map[string]any, _ map[string]any) (string, error) {
						return "state-id-from-getid", nil
					}
					r.ExternalName.GetImportIDFn = nil
				}),
			},
			want: want{
				terraformID: "state-id-from-getid",
			},
		},
		"GetImportIDFnSet_UsedForTerraformID": {
			reason: "When GetImportIDFn is set, terraformID should equal its result, not GetIDFn's.",
			args: args{
				cfg: newTestResource(func(r *config.Resource) {
					r.ExternalName.GetIDFn = func(_ context.Context, _ string, _ map[string]any, _ map[string]any) (string, error) {
						return "state-id", nil
					}
					r.ExternalName.GetImportIDFn = func(_ context.Context, _ string, _ map[string]any, _ map[string]any) (string, error) {
						return "import-id", nil
					}
				}),
			},
			want: want{
				terraformID: "import-id",
			},
		},
		"GetImportIDFnError_Propagated": {
			reason: "When GetImportIDFn returns an error, it should be propagated.",
			args: args{
				cfg: newTestResource(func(r *config.Resource) {
					r.ExternalName.GetIDFn = func(_ context.Context, _ string, _ map[string]any, _ map[string]any) (string, error) {
						return "state-id", nil
					}
					r.ExternalName.GetImportIDFn = func(_ context.Context, _ string, _ map[string]any, _ map[string]any) (string, error) {
						return "", errBoom
					}
				}),
			},
			want: want{
				err: errors.Wrap(errBoom, errGetID),
			},
		},
		"GetIDFnError_Propagated": {
			reason: "When GetIDFn returns an error, GetImportIDFn is never reached.",
			args: args{
				cfg: newTestResource(func(r *config.Resource) {
					r.ExternalName.GetIDFn = func(_ context.Context, _ string, _ map[string]any, _ map[string]any) (string, error) {
						return "", errBoom
					}
					r.ExternalName.GetImportIDFn = func(_ context.Context, _ string, _ map[string]any, _ map[string]any) (string, error) {
						return "import-id", nil
					}
				}),
			},
			want: want{
				err: errors.Wrap(errBoom, errGetID),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ws := newTestWorkspaceStore()
			uid := types.UID("test-importid-" + name)
			tr := newTestTerraformed(uid)
			defer func() {
				_ = os.RemoveAll(filepath.Join(os.TempDir(), string(uid)))
			}()

			w, err := ws.Workspace(context.Background(), nil, tr, testSetup, tc.args.cfg)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Fatalf("\n%s\nWorkspace(...): -want error, +got error:\n%s", tc.reason, diff)
			}
			if tc.want.err != nil {
				return
			}
			if diff := cmp.Diff(tc.want.terraformID, w.terraformID); diff != "" {
				t.Errorf("\n%s\nWorkspace(...): -want terraformID, +got terraformID:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestWorkspaceStoreGetImportIDFnStateVsTerraformID(t *testing.T) {
	ws := newTestWorkspaceStore()

	cfg := newTestResource(func(r *config.Resource) {
		r.ExternalName.GetIDFn = func(_ context.Context, _ string, _ map[string]any, _ map[string]any) (string, error) {
			return "state-id-for-tfstate", nil
		}
		r.ExternalName.GetImportIDFn = func(_ context.Context, _ string, _ map[string]any, _ map[string]any) (string, error) {
			return "import-id-for-terraform", nil
		}
	})

	uid := types.UID("test-importid-state-vs-import")
	tr := newTestTerraformed(uid)
	defer func() {
		_ = os.RemoveAll(filepath.Join(os.TempDir(), string(uid)))
	}()

	_, err := ws.Workspace(context.Background(), nil, tr, testSetup, cfg)
	if err != nil {
		t.Fatalf("Workspace(...): unexpected error: %v", err)
	}

	wsDir := filepath.Join(os.TempDir(), string(uid))
	stateBytes, err := os.ReadFile(filepath.Join(wsDir, "terraform.tfstate"))
	if err != nil {
		t.Fatalf("cannot read terraform.tfstate: %v", err)
	}

	stateStr := string(stateBytes)
	if !strings.Contains(stateStr, "state-id-for-tfstate") {
		t.Errorf("terraform.tfstate should contain state ID 'state-id-for-tfstate', got:\n%s", stateStr)
	}
	if strings.Contains(stateStr, "import-id-for-terraform") {
		t.Errorf("terraform.tfstate should NOT contain import ID 'import-id-for-terraform', got:\n%s", stateStr)
	}
}
