package conversion

import (
	"context"

	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/meta"
	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli"
)

const (
	errCannotConsumeState = "cannot consume state"

	errFmtCannotDoWithTFCli = "cannot %s with tf cli"
)

// BuildClientForResource returns a tfcli client by setting attributes
// (i.e. desired spec input) and terraform state (if available) for a given
// client builder base.
func BuildClientForResource(builderBase tfcli.Builder, tr resource.Terraformed) (tfcli.Client, error) {
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

	return builderBase.WithState(stateRaw).WithResourceBody(attr).BuildClient()
}

// CLI is an Adapter implementation for Terraform CLI
type CLI struct {
	tfcli tfcli.Client
}

// NewCli returns a CLI object
func NewCli(client tfcli.Client) *CLI {
	return &CLI{
		tfcli: client,
	}
}

// Observe is a Terraform CLI implementation for Observe function of Adapter interface.
func (t *CLI) Observe(ctx context.Context, tr resource.Terraformed) (Observation, error) {

	tfRes, err := t.tfcli.Refresh(ctx, xpmeta.GetExternalName(tr))

	if isOperationInProgress(err, tfcli.OperationApply) {
		//  A previously started "Apply" operation is in progress or completed
		//  but one last call needs to be done as completed to be able to kick
		//  off a new operation. We will return "Exists: true, UpToDate: false"
		//  in order to trigger an Update call.
		return Observation{
			Exists:   true,
			UpToDate: false,
		}, nil
	}

	if isOperationInProgress(err, tfcli.OperationDestroy) {
		// A previously started "Destroy" operation is in progress or completed
		// but one last call needs to be done as completed to be able to kick
		// off a new operation. We will return "Exists: true, UpToDate: true" in
		// order to trigger a Delete call (given we already have deletion
		// timestamp set).
		return Observation{
			Exists:   true,
			UpToDate: true,
		}, nil
	}

	if err != nil {
		return Observation{}, errors.Wrapf(err, errFmtCannotDoWithTFCli, "observe")
	}
	// No tfcli operation was in progress, our blocking observation completed
	// successfully, and we have an observation to consume.

	// If resource does not exist, and it was actually deleted, we no longer
	// need this client (hence underlying workspace) for this resource.
	if !tfRes.Exists && xpmeta.WasDeleted(tr) {
		return Observation{}, errors.Wrap(t.tfcli.Close(ctx), "failed to clean up tfcli client")
	}

	// After a successful observation, we now have a state to consume.
	// We will consume the state by:
	// - returning "sensitive attributes" as connection details
	// - setting external name annotation, if not set already, from <id> attribute
	// - late initializing "spec.forProvider" with "attributes"
	// - setting observation at "status.atProvider" with "attributes"
	// - storing base64encoded "tfstate" as an annotation
	conn, err := consumeState(tfRes.State, tr)
	if err != nil {
		return Observation{}, errors.Wrap(err, errCannotConsumeState)
	}

	return Observation{
		ConnectionDetails: conn,
		UpToDate:          tfRes.UpToDate,
		Exists:            tfRes.Exists,
	}, nil
}

// Update is a Terraform CLI implementation for Update function of Adapter interface.
func (t *CLI) Update(ctx context.Context, tr resource.Terraformed) (Update, error) {
	ar, err := t.tfcli.Apply(ctx)
	if err != nil {
		return Update{}, errors.Wrapf(err, errFmtCannotDoWithTFCli, "update")
	}

	if !ar.Completed {
		return Update{}, nil
	}

	// After a successful Apply, we now have a state to consume.
	// We will consume the state by:
	// - returning "sensitive attributes" as connection details
	// - setting external name annotation, if not set already, from <id> attribute
	// - late initializing "spec.forProvider" with "attributes"
	// - setting observation at "status.atProvider" with "attributes"
	// - storing base64encoded "tfstate" as an annotation
	conn, err := consumeState(ar.State, tr)
	if err != nil {
		return Update{}, errors.Wrap(err, errCannotConsumeState)
	}
	return Update{
		Completed:         true,
		ConnectionDetails: conn,
	}, err
}

// Delete is a Terraform CLI implementation for Delete function of Adapter interface.
func (t *CLI) Delete(ctx context.Context, tr resource.Terraformed) (bool, error) {
	dr, err := t.tfcli.Destroy(ctx)
	if err != nil {
		return false, errors.Wrapf(err, errFmtCannotDoWithTFCli, "delete")
	}

	return dr.Completed, nil
}

// consumeState parses input tfstate and sets related fields in the custom resource.
func consumeState(state []byte, tr resource.Terraformed) (managed.ConnectionDetails, error) {
	st, err := ParseStateV4(state)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build state")
	}

	if xpmeta.GetExternalName(tr) == "" {
		// Terraform stores id for the external resource as an attribute in the
		// resource state. Key for the attribute holding external identifier is
		// resource specific. We rely on GetTerraformResourceIdField() function
		// to find out that key.
		stAttr := map[string]interface{}{}
		if err = JSParser.Unmarshal(st.GetAttributes(), &stAttr); err != nil {
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
	if err = JSParser.Unmarshal(st.GetSensitiveAttributes(), &conn); err != nil {
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
