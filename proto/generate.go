//go:build generate
// +build generate

// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

// Generate gRPC types and stubs. See tools/buf.gen.yaml for
// buf's configuration.
//
//go:generate go -C tools tool buf generate .. --template buf.gen.yaml --output ..

package proto
