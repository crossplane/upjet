// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"
	"time"

	"github.com/crossplane/upjet/v2/pkg/config"
)

func TestAsyncTimeout(t *testing.T) {
	cfg := &config.Resource{OperationTimeouts: config.OperationTimeouts{
		Create: 3 * time.Hour,
		Delete: 2 * time.Hour,
	}}
	cases := map[string]struct {
		cfg  *config.Resource
		op   string
		want time.Duration
	}{
		"NilConfig":        {cfg: nil, op: "create", want: defaultAsyncTimeout},
		"CreateConfigured": {cfg: cfg, op: "create", want: 3 * time.Hour},
		"DeleteConfigured": {cfg: cfg, op: "delete", want: 2 * time.Hour},
		"UpdateUnset":      {cfg: cfg, op: "update", want: defaultAsyncTimeout},
		"UnknownOp":        {cfg: cfg, op: "read", want: defaultAsyncTimeout},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := asyncTimeout(tc.cfg, tc.op); got != tc.want {
				t.Errorf("asyncTimeout(%q) = %v, want %v", tc.op, got, tc.want)
			}
		})
	}
}
