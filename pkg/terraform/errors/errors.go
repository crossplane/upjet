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

package errors

type applyFailed struct {
	log string
}

func (a *applyFailed) Error() string {
	return a.log
}

// NewApplyFailed returns a new apply failure error with given logs.
func NewApplyFailed(log string) error {
	return &applyFailed{log: log}
}

// IsApplyFailed returns whether error is due to failure of an apply operation.
func IsApplyFailed(err error) bool {
	_, ok := err.(*applyFailed)
	return ok
}

type destroyFailed struct {
	log string
}

func (a *destroyFailed) Error() string {
	return a.log
}

// NewDestroyFailed returns a new destroy failure error with given logs.
func NewDestroyFailed(log string) error {
	return &destroyFailed{log: log}
}

// IsDestroyFailed returns whether error is due to failure of a destroy operation.
func IsDestroyFailed(err error) bool {
	_, ok := err.(*destroyFailed)
	return ok
}
