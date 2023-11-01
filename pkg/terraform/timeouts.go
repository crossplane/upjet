// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package terraform

import (
	"github.com/crossplane/crossplane-runtime/pkg/errors"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/resource/json"
)

// "e2bfb730-ecaa-11e6-8f88-34363bc7c4c0" is a hardcoded string for Terraform
// timeout key in private raw, i.e. provider specific metadata:
// https://github.com/hashicorp/terraform-plugin-sdk/blob/112e2164c381d80e8ada3170dac9a8a5db01079a/helper/schema/resource_timeout.go#L14
const tfMetaTimeoutKey = "e2bfb730-ecaa-11e6-8f88-34363bc7c4c0"

type timeouts config.OperationTimeouts

func (ts timeouts) asParameter() map[string]string {
	param := make(map[string]string)
	if t := ts.Read.String(); t != "0s" {
		param["read"] = t
	}
	if t := ts.Create.String(); t != "0s" {
		param["create"] = t
	}
	if t := ts.Update.String(); t != "0s" {
		param["update"] = t
	}
	if t := ts.Delete.String(); t != "0s" {
		param["delete"] = t
	}
	return param
}

func (ts timeouts) asMetadata() map[string]any {
	// See how timeouts encoded as metadata on Terraform side:
	// https://github.com/hashicorp/terraform-plugin-sdk/blob/112e2164c381d80e8ada3170dac9a8a5db01079a/helper/schema/resource_timeout.go#L170
	meta := make(map[string]any)
	if t := ts.Read.String(); t != "0s" {
		meta["read"] = ts.Read.Nanoseconds()
	}
	if t := ts.Create.String(); t != "0s" {
		meta["create"] = ts.Create.Nanoseconds()
	}
	if t := ts.Update.String(); t != "0s" {
		meta["update"] = ts.Update.Nanoseconds()
	}
	if t := ts.Delete.String(); t != "0s" {
		meta["delete"] = ts.Delete.Nanoseconds()
	}
	return meta
}

func insertTimeoutsMeta(existingMeta []byte, to timeouts) ([]byte, error) {
	customTimeouts := to.asMetadata()
	if len(customTimeouts) == 0 {
		// No custom timeout configured, nothing to do.
		return existingMeta, nil
	}
	meta := make(map[string]any)
	if len(existingMeta) == 0 {
		// No existing data, just initialize a new meta with custom timeouts.
		meta[tfMetaTimeoutKey] = customTimeouts
		return json.JSParser.Marshal(meta)
	}
	// There are some existing metadata, let's parse it to insert custom
	// timeouts properly.
	if err := json.JSParser.Unmarshal(existingMeta, &meta); err != nil {
		return nil, errors.Wrap(err, "cannot parse existing metadata")
	}
	if existingTimeouts, ok := meta[tfMetaTimeoutKey].(map[string]any); ok {
		// There are some timeout configuration exists in existing metadata.
		// Only override custom timeouts.
		for k, v := range customTimeouts {
			existingTimeouts[k] = v
		}
		return json.JSParser.Marshal(meta)
	}
	// No existing timeout configuration, initialize it with custom timeouts.
	meta[tfMetaTimeoutKey] = customTimeouts
	return json.JSParser.Marshal(meta)
}
