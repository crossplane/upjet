// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package migration

import "fmt"

type errUnsupportedStepType struct {
	planStep Step
}

func (e errUnsupportedStepType) Error() string {
	return fmt.Sprintf("executor does not support steps of type %q in step: %s", e.planStep.Type, e.planStep.Name)
}

func NewUnsupportedStepTypeError(s Step) error {
	return errUnsupportedStepType{
		planStep: s,
	}
}
