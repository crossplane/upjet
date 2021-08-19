package tfcli

import "fmt"

// OperationType is an operation type for terraform cli
type OperationType int

const (
	// OperationUnknown represents an Unknown operation
	OperationUnknown OperationType = iota
	// OperationApply represents an Apply operation
	OperationApply
	// OperationDestroy respresents a Destrot operation
	OperationDestroy
)

func (o OperationType) String() string {
	return []string{"Unknown", "Apply", "Destroy"}[o]
}

// OperationInProgressError is a custom error indicating there is an ongoing
// operation which prevents starting a new one.
type OperationInProgressError struct {
	op OperationType
}

func (e *OperationInProgressError) Error() string {
	return fmt.Sprintf("operation %s in progress", e.op.String())
}

// GetOperation returns the OperationType that is in progress
func (e *OperationInProgressError) GetOperation() OperationType {
	return e.op
}
