package tfcli

import "fmt"

// OperationType is an operation type for terraform cli
type OperationType int

const (
	OperationUnknown OperationType = iota
	OperationObserve
	OperationCreate
	OperationUpdate
	OperationDelete
)

func (o OperationType) String() string {
	return []string{"Unknown", "Observe", "Create", "Update", "Delete"}[o]
}

type OperationInProgressError struct {
	op OperationType
}

func (e *OperationInProgressError) Error() string {
	return fmt.Sprintf("operation %s in progress", e.op.String())
}

func (e *OperationInProgressError) GetOperation() OperationType {
	return e.op
}
