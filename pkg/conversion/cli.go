package conversion

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/json"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"

	"github.com/crossplane-contrib/terrajet/pkg/meta"
	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli"
)

// Cli is an Adapter implementation for Terraform Cli
type Cli struct {
	builderBase tfcli.Builder
}

func NewCli(l logging.Logger, tr resource.Terraformed, cliBuilder tfcli.Builder) *Cli {
	return &Cli{
		builderBase: cliBuilder,
	}
}

func (t *Cli) Observe(ctx context.Context, tr resource.Terraformed) (ObserveResult, error) {
	attr, err := tr.GetParameters()
	if err != nil {
		return ObserveResult{}, errors.Wrap(err, "failed to get attributes")
	}

	var stRaw []byte
	if meta.GetState(tr) != "" {
		stEnc := meta.GetState(tr)
		st, err := BuildStateV4(stEnc, nil)
		if err != nil {
			return ObserveResult{}, errors.Wrap(err, "cannot build state")
		}

		stRaw, err = st.Serialize()
		if err != nil {
			return ObserveResult{}, errors.Wrap(err, "cannot serialize state")
		}
	}

	tfc, err := t.builderBase.WithState(stRaw).WithResourceBody(attr).BuildCreateClient()

	tfRes, err := tfc.Observe(xpmeta.GetExternalName(tr))

	if opErr, ok := err.(*tfcli.OperationInProgressError); ok {
		if opErr.GetOperation() == tfcli.OperationCreate {
			return ObserveResult{
				Completed: true,
				Exists:    false,
			}, nil
		}
		if opErr.GetOperation() == tfcli.OperationUpdate || opErr.GetOperation() == tfcli.OperationDelete {
			return ObserveResult{
				Completed: true,
				Exists:    true,
			}, nil
		}
	}
	if err != nil {
		return ObserveResult{}, errors.Wrap(err, "failed to observe with tf cli")
	}

	if !tfRes.Completed {
		return ObserveResult{
			Completed: false,
		}, nil
	}

	newStateRaw := tfc.GetState()

	newSt, err := ReadStateV4(newStateRaw)
	if err != nil {
		return ObserveResult{}, errors.Wrap(err, "cannot build state")
	}

	// TODO(hasan): Handle late initialization

	if err = tr.SetObservation(newSt.GetAttributes()); err != nil {
		return ObserveResult{}, errors.Wrap(err, "cannot set observation")
	}

	conn := managed.ConnectionDetails{}
	if err = json.Unmarshal(newSt.GetSensitiveAttributes(), &conn); err != nil {
		return ObserveResult{}, errors.Wrap(err, "cannot parse connection details")
	}

	newStEnc, err := newSt.GetEncodedState()
	if err != nil {
		return ObserveResult{}, errors.Wrap(err, "cannot encode new state")
	}

	return ObserveResult{
		Completed:         true,
		State:             newStEnc,
		ConnectionDetails: conn,
		UpToDate:          tfRes.UpToDate,
		Exists:            tfRes.Exists,
	}, nil
}

func (t *Cli) Create(ctx context.Context, tr resource.Terraformed) (CreateResult, error) {
	attr, err := tr.GetParameters()
	if err != nil {
		return CreateResult{}, errors.Wrap(err, "failed to get attributes")
	}

	tfc, err := t.builderBase.WithResourceBody(attr).BuildCreateClient()

	if err != nil {
		return CreateResult{}, errors.Wrap(err, "cannot build create client")
	}

	completed, err := tfc.Create()
	if err != nil {
		return CreateResult{}, errors.Wrap(err, "create failed with")
	}

	if !completed {
		return CreateResult{}, nil
	}

	stRaw := tfc.GetState()
	st, err := ReadStateV4(stRaw)
	if err != nil {
		return CreateResult{}, errors.Wrap(err, "cannot parse state")
	}

	stAttr := map[string]interface{}{}

	if err = json.Unmarshal(st.GetAttributes(), &stAttr); err != nil {
		return CreateResult{}, errors.Wrap(err, "cannot parse state attributes")
	}

	id, exists := stAttr[tr.GetTerraformResourceIdField()]
	if !exists {
		return CreateResult{}, errors.Wrap(err, fmt.Sprintf("no value for id field: %s", tr.GetTerraformResourceIdField()))
	}
	en, ok := id.(string)
	if !ok {
		return CreateResult{}, errors.Wrap(err, "id field is not a string")
	}

	conn := managed.ConnectionDetails{}
	if err = json.Unmarshal(st.GetSensitiveAttributes(), &conn); err != nil {
		return CreateResult{}, errors.Wrap(err, "cannot parse connection details")
	}

	stEnc, err := st.GetEncodedState()
	if err != nil {
		return CreateResult{}, errors.Wrap(err, "cannot encode new state")
	}

	return CreateResult{
		Completed:         true,
		ExternalName:      en,
		State:             stEnc,
		ConnectionDetails: conn,
	}, nil
}

// Update is a Terraform Cli implementation for Apply function of Adapter interface.
func (t *Cli) Update(ctx context.Context, tr resource.Terraformed) (UpdateResult, error) {
	return UpdateResult{}, nil
}

// Delete is a Terraform Cli implementation for Delete function of Adapter interface.
func (t *Cli) Delete(ctx context.Context, tr resource.Terraformed) (DeletionResult, error) {
	stEnc := meta.GetState(tr)
	st, err := BuildStateV4(stEnc, nil)
	if err != nil {
		return DeletionResult{}, errors.Wrap(err, "cannot build state")
	}

	stRaw, err := st.Serialize()
	if err != nil {
		return DeletionResult{}, errors.Wrap(err, "cannot serialize state")
	}

	attr, err := tr.GetParameters()
	if err != nil {
		return DeletionResult{}, errors.Wrap(err, "failed to get attributes")
	}

	tfc, err := t.builderBase.WithState(stRaw).WithResourceBody(attr).BuildDeletionClient()
	if err != nil {
		return DeletionResult{}, errors.Wrap(err, "cannot build delete client")
	}

	completed, err := tfc.Delete()
	if err != nil {
		return DeletionResult{}, errors.Wrap(err, "failed to delete")
	}

	if !completed {
		return DeletionResult{}, nil
	}

	return DeletionResult{Completed: true}, nil
}
