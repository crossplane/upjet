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
	"sync"
	"time"
)

// Operation is the representation of a single Terraform CLI operation.
type Operation struct {
	Type string
	Err  error

	startTime *time.Time
	endTime   *time.Time
	mu        sync.RWMutex
}

// MarkStart marks the operation as started.
func (o *Operation) MarkStart(t string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now()
	o.Type = t
	o.startTime = &now
	o.endTime = nil
	o.Err = nil

}

// MarkEnd marks the operation as ended.
func (o *Operation) MarkEnd() {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now()
	o.endTime = &now
}

// Flush cleans the operation information.
func (o *Operation) Flush() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.Type = ""
	o.startTime = nil
	o.endTime = nil
	o.Err = nil
}

// IsEnded returns whether the operation has ended, regardless of its result.
func (o *Operation) IsEnded() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.endTime != nil
}

// IsInProgress returns whether there is an ongoing operation.
func (o *Operation) IsInProgress() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.startTime != nil && o.endTime == nil
}

// StartTime returns the start time of the current operation.
func (o *Operation) StartTime() *time.Time {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.startTime
}

// EndTime returns the end time of the current operation.
func (o *Operation) EndTime() *time.Time {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.endTime
}
