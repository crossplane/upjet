/*
Copyright 2021 Upbound Inc.
*/

package terraform

import (
	"sync"
	"time"
)

// Operation is the representation of a single Terraform CLI operation.
type Operation struct {
	Type string

	startTime *time.Time
	endTime   *time.Time
	mu        sync.RWMutex
}

// MarkStart marks the operation as started atomically after checking
// no previous operation is already running.
// Returns `false` if a previous operation is still in progress.
func (o *Operation) MarkStart(t string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.startTime != nil && o.endTime == nil {
		return false
	}
	now := time.Now()
	o.Type = t
	o.startTime = &now
	o.endTime = nil
	return true
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
}

// IsEnded returns whether the operation has ended, regardless of its result.
func (o *Operation) IsEnded() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.endTime != nil
}

// IsRunning returns whether there is an ongoing operation.
func (o *Operation) IsRunning() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.startTime != nil && o.endTime == nil
}

// StartTime returns the start time of the current operation.
func (o *Operation) StartTime() time.Time {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return *o.startTime
}

// EndTime returns the end time of the current operation.
func (o *Operation) EndTime() time.Time {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return *o.endTime
}
