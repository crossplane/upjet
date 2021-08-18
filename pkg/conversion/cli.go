package conversion

import (
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/json"

	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"

	"github.com/crossplane-contrib/terrajet/pkg/meta"
	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli"
)

const (
	errCannotGetClientBuilder = "cannot get client builder"
	errCannotConsumeState     = "cannot consume state"

	errFmtCannotDoWithTFCli = "cannot %s with tf cli"
	errFmtCannotBuildClient = "cannot build %s client"
)

// CLI is an Adapter implementation for Terraform CLI
type CLI struct {
	builderBase tfcli.Builder
}

// NewCli returns a CLI object
func NewCli(cliBuilder tfcli.Builder) *CLI {
	return &CLI{
		builderBase: cliBuilder,
	}
}

// Observe is a Terraform CLI implementation for Observe function of Adapter interface.
func (t *CLI) Observe(tr resource.Terraformed) (Observation, error) {
	b, err := t.getClientBuilderForResource(tr)
	if err != nil {
		return Observation{}, errors.Wrap(err, errCannotGetClientBuilder)
	}
	tfc, err := b.BuildObserveClient()
	if err != nil {
		return Observation{}, errors.Wrapf(err, errFmtCannotBuildClient, "observe")
	}

	// Attempt to make an observation. There are a couple of possibilities at this point:
	// a. No tfcli operation in progress, we just kick off a new observation. It should
	//    immediately return "tfRes.Completed" as "false", and we return completed=false in Observation.
	// b. An "observe" operation is in progress that we kicked off in one of the previous reconciliations.
	//    This call would return tfRes.Completed as false, and we would return completed=false in Observation.
	// c. A previously started "observe" operation completed. We can just consume state and return Observation
	//    accordingly.
	// d. A previously started "create" operation is in progress or completed but its state needs to be
	//    read to kick off a new operation. We will return "Exists: false" in order to trigger a Create call.
	// e. A previously started "update" operation is in progress or completed but its state needs to be
	//    read to kick off a new operation. We will return "UpToDate: false" in order to trigger an Update call.
	// f. A previously started "delete" operation is in progress and by returning "Exists: true" and since
	//    deletion timestamp should already be set, it would trigger a delete call.
	tfRes, err := tfc.Observe(xpmeta.GetExternalName(tr))

	if isOperationInProgress(err, tfcli.OperationCreate) {
		return Observation{
			Completed: true,

			Exists: false,
		}, nil
	}

	if isOperationInProgress(err, tfcli.OperationUpdate) || isOperationInProgress(err, tfcli.OperationDelete) {
		return Observation{
			Completed: true,

			Exists:   true,
			UpToDate: false,
		}, nil
	}

	if err != nil {
		return Observation{}, errors.Wrapf(err, errFmtCannotDoWithTFCli, "observe")
	}

	if !tfRes.Completed {
		return Observation{
			Completed: false,
		}, nil
	}

	conn, err := consumeState(tfc.GetState(), tr, false)
	if err != nil {
		return Observation{}, errors.Wrap(err, errCannotConsumeState)
	}

	return Observation{
		Completed:         true,
		ConnectionDetails: conn,
		UpToDate:          tfRes.UpToDate,
		Exists:            tfRes.Exists,
	}, nil
}

// Create is a Terraform CLI implementation for Create function of Adapter interface.
func (t *CLI) Create(tr resource.Terraformed) (Creation, error) {
	b, err := t.getClientBuilderForResource(tr)
	if err != nil {
		return Creation{}, errors.Wrap(err, errCannotGetClientBuilder)
	}

	tfc, err := b.BuildCreateClient()
	if err != nil {
		return Creation{}, errors.Wrapf(err, errFmtCannotBuildClient, "create")
	}

	completed, err := tfc.Create()
	if err != nil {
		return Creation{}, errors.Wrapf(err, errFmtCannotDoWithTFCli, "create")
	}

	if !completed {
		return Creation{}, nil
	}

	conn, err := consumeState(tfc.GetState(), tr, true)
	if err != nil {
		return Creation{}, errors.Wrap(err, errCannotConsumeState)
	}

	return Creation{
		Completed:         true,
		ConnectionDetails: conn,
	}, nil
}

// Update is a Terraform CLI implementation for Update function of Adapter interface.
func (t *CLI) Update(tr resource.Terraformed) (Update, error) {
	b, err := t.getClientBuilderForResource(tr)
	if err != nil {
		return Update{}, errors.Wrap(err, errCannotGetClientBuilder)
	}

	tfc, err := b.BuildUpdateClient()
	if err != nil {
		return Update{}, errors.Wrapf(err, errFmtCannotBuildClient, "update")
	}

	completed, err := tfc.Update()
	if err != nil {
		return Update{}, errors.Wrapf(err, errFmtCannotDoWithTFCli, "update")
	}

	if !completed {
		return Update{}, nil
	}

	conn, err := consumeState(tfc.GetState(), tr, false)
	if err != nil {
		return Update{}, errors.Wrap(err, errCannotConsumeState)
	}
	return Update{
		Completed:         true,
		ConnectionDetails: conn,
	}, err
}

// Delete is a Terraform CLI implementation for Delete function of Adapter interface.
func (t *CLI) Delete(tr resource.Terraformed) (bool, error) {
	b, err := t.getClientBuilderForResource(tr)
	if err != nil {
		return false, errors.Wrap(err, errCannotGetClientBuilder)
	}

	tfc, err := b.BuildDeletionClient()
	if err != nil {
		return false, errors.Wrapf(err, errFmtCannotBuildClient, "delete")
	}

	completed, err := tfc.Delete()
	if err != nil {
		return false, errors.Wrapf(err, errFmtCannotDoWithTFCli, "delete")
	}

	if !completed {
		return false, nil
	}

	// TODO(hasan): Does it make any sense to call GetState on delete client?
	// Would delete operation be considered as completed (i.e. allowing a new operation like Observe)
	// if I didn't call GetState?
	_ = tfc.GetState()

	return true, nil
}

// getClientBuilderForResource returns a tfcli client builder by setting attributes
// (i.e. desired spec input) and terraform state (if available) on client builder base.
func (t *CLI) getClientBuilderForResource(tr resource.Terraformed) (tfcli.Builder, error) {
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

// consumeState parses input tfstate and sets related fields in the custom resource.
func consumeState(state []byte, tr resource.Terraformed, parseExternalID bool) (managed.ConnectionDetails, error) {
	st, err := ParseStateV4(state)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build state")
	}

	if parseExternalID {
		// Terraform stores id for the external resource as an attribute in the resource state.
		// Key for the attribute holding external identifier is resource specific. We rely on
		// GetTerraformResourceIdField() function to find out that key.
		stAttr := map[string]interface{}{}
		if err = json.Unmarshal(st.GetAttributes(), &stAttr); err != nil {
			return nil, errors.Wrap(err, "cannot parse state attributes")
		}

		id, exists := stAttr[tr.GetTerraformResourceIdField()]
		if !exists {
			return nil, errors.Wrapf(err, "no value for id field: %s", tr.GetTerraformResourceIdField())
		}
		extID, ok := id.(string)
		if !ok {
			return nil, errors.Wrap(err, "id field is not a string")
		}
		xpmeta.SetExternalName(tr, extID)
	}

	// TODO(hasan): Handle late initialization

	if err = tr.SetObservation(st.GetAttributes()); err != nil {
		return nil, errors.Wrap(err, "cannot set observation")
	}

	conn := managed.ConnectionDetails{}
	if err = json.Unmarshal(st.GetSensitiveAttributes(), &conn); err != nil {
		return nil, errors.Wrap(err, "cannot parse connection details")
	}

	stEnc, err := st.GetEncodedState()
	if err != nil {
		return nil, errors.Wrap(err, "cannot encoded state")
	}
	meta.SetState(tr, stEnc)

	return conn, nil
}

func isOperationInProgress(err error, op tfcli.OperationType) bool {
	if opErr, ok := err.(*tfcli.OperationInProgressError); ok {
		if opErr.GetOperation() == op {
			return true
		}
	}
	return false
}
