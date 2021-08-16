package conversion

import (
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/json"

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

func NewCli(cliBuilder tfcli.Builder) *Cli {
	return &Cli{
		builderBase: cliBuilder,
	}
}

func (t *Cli) Observe(tr resource.Terraformed) (ObserveResult, error) {
	b, err := t.getBuilderForResource(tr)
	if err != nil {
		return ObserveResult{}, errors.Wrap(err, "cannot get builder")
	}

	tfc, err := b.BuildObserveClient()
	if err != nil {
		return ObserveResult{}, errors.Wrap(err, "cannot build observe client")
	}

	tfRes, err := tfc.Observe(xpmeta.GetExternalName(tr))

	if opErr, ok := err.(*tfcli.OperationInProgressError); ok {
		if opErr.GetOperation() == tfcli.OperationCreate {
			return ObserveResult{
				Completed: true,

				Exists: false,
			}, nil
		}
		if opErr.GetOperation() == tfcli.OperationUpdate || opErr.GetOperation() == tfcli.OperationDelete {
			return ObserveResult{
				Completed: true,

				Exists:   true,
				UpToDate: false,
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

	stParseResp, err := consumeState(tfc.GetState(), tr, false)
	if err != nil {
		return ObserveResult{}, errors.Wrap(err, "cannot parse state")
	}

	return ObserveResult{
		Completed:         true,
		State:             stParseResp.encodedState,
		ConnectionDetails: stParseResp.connectionDetails,
		UpToDate:          tfRes.UpToDate,
		Exists:            tfRes.Exists,
	}, nil
}

func (t *Cli) Create(tr resource.Terraformed) (CreateResult, error) {
	b, err := t.getBuilderForResource(tr)
	if err != nil {
		return CreateResult{}, errors.Wrap(err, "cannot get builder")
	}

	tfc, err := b.BuildCreateClient()
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

	stParseResp, err := consumeState(tfc.GetState(), tr, true)
	if err != nil {
		return CreateResult{}, errors.Wrap(err, "cannot parse state")
	}

	return CreateResult{
		Completed:         true,
		ExternalName:      stParseResp.externalID,
		State:             stParseResp.encodedState,
		ConnectionDetails: stParseResp.connectionDetails,
	}, nil
}

// Update is a Terraform Cli implementation for Apply function of Adapter interface.
func (t *Cli) Update(tr resource.Terraformed) (UpdateResult, error) {
	b, err := t.getBuilderForResource(tr)
	if err != nil {
		return UpdateResult{}, errors.Wrap(err, "cannot get builder")
	}

	tfc, err := b.BuildUpdateClient()
	if err != nil {
		return UpdateResult{}, errors.Wrap(err, "cannot build update client")
	}

	completed, err := tfc.Update()
	if err != nil {
		return UpdateResult{}, errors.Wrap(err, "update failed")
	}

	if !completed {
		return UpdateResult{}, nil
	}

	stParseResp, err := consumeState(tfc.GetState(), tr, false)
	if err != nil {
		return UpdateResult{}, errors.Wrap(err, "cannot parse state")
	}
	return UpdateResult{
		Completed:         true,
		State:             stParseResp.encodedState,
		ConnectionDetails: stParseResp.connectionDetails,
	}, err
}

// Delete is a Terraform Cli implementation for Delete function of Adapter interface.
func (t *Cli) Delete(tr resource.Terraformed) (DeletionResult, error) {
	b, err := t.getBuilderForResource(tr)
	if err != nil {
		return DeletionResult{}, errors.Wrap(err, "cannot get builder")
	}

	tfc, err := b.BuildDeletionClient()
	if err != nil {
		return DeletionResult{}, errors.Wrap(err, "cannot build deletion client")
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

func (t *Cli) getBuilderForResource(tr resource.Terraformed) (tfcli.Builder, error) {
	var stateRaw []byte
	if meta.GetState(tr) != "" {
		stEnc := meta.GetState(tr)
		st, err := BuildStateV4(stEnc, nil)
		if err != nil {
			return nil, errors.Wrap(err, "cannot build state")
		}

		stateRaw, err = st.Serialize()
		if err != nil {
			return nil, errors.Wrap(err, "cannot serialize state")
		}
	}

	attr, err := tr.GetParameters()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get attributes")
	}

	return t.builderBase.WithState(stateRaw).WithResourceBody(attr), nil
}

type consumeStateResponse struct {
	externalID        string
	encodedState      string
	connectionDetails managed.ConnectionDetails
}

func consumeState(state []byte, tr resource.Terraformed, parseExternalID bool) (consumeStateResponse, error) {
	st, err := ReadStateV4(state)
	if err != nil {
		return consumeStateResponse{}, errors.Wrap(err, "cannot build state")
	}

	var extID string
	if parseExternalID {
		stAttr := map[string]interface{}{}
		if err = json.Unmarshal(st.GetAttributes(), &stAttr); err != nil {
			return consumeStateResponse{}, errors.Wrap(err, "cannot parse state attributes")
		}

		id, exists := stAttr[tr.GetTerraformResourceIdField()]
		if !exists {
			return consumeStateResponse{}, errors.Wrap(err, fmt.Sprintf("no value for id field: %s", tr.GetTerraformResourceIdField()))
		}
		var ok bool
		extID, ok = id.(string)
		if !ok {
			return consumeStateResponse{}, errors.Wrap(err, "id field is not a string")
		}
	}

	// TODO(hasan): Handle late initialization

	if err = tr.SetObservation(st.GetAttributes()); err != nil {
		return consumeStateResponse{}, errors.Wrap(err, "cannot set observation")
	}

	conn := managed.ConnectionDetails{}
	if err = json.Unmarshal(st.GetSensitiveAttributes(), &conn); err != nil {
		return consumeStateResponse{}, errors.Wrap(err, "cannot parse connection details")
	}

	stEnc, err := st.GetEncodedState()
	if err != nil {
		return consumeStateResponse{}, errors.Wrap(err, "cannot encode new state")
	}
	return consumeStateResponse{
		externalID:        extID,
		encodedState:      stEnc,
		connectionDetails: conn,
	}, nil
}
