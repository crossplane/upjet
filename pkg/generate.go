// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

//go:build generate
// +build generate

package pkg

// NOTE(muvaf): We import the tools used un go:generate targets so that we can
// track their versions using go.mod and let Go handle its installation. See
// the following link for details: https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module

import (
	_ "github.com/golang/mock/mockgen" //nolint:typecheck
)
