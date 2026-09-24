// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package terraform

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/resource/json"
)

func stateAttributes(raw []byte) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	attr := map[string]any{}
	if err := json.JSParser.Unmarshal(raw, &attr); err != nil {
		return nil, errors.Wrap(err, errUnmarshalAttr)
	}
	return attr, nil
}

func stateExists(attr map[string]any, hasTFID bool) (bool, error) {
	if attr == nil {
		return false, nil
	}
	if !hasTFID {
		return len(attr) != 0, nil
	}
	id, ok := attr["id"]
	if !ok || id == nil {
		return false, nil
	}
	sid, ok := id.(string)
	if !ok {
		return false, errors.Errorf(errFmtNonString, fmt.Sprint(id))
	}
	return sid != "", nil
}
