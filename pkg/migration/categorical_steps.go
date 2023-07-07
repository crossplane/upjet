// Copyright 2023 Upbound Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package migration

import (
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	errEditCategory = "failed to put the edited resource of category %q: %s"
)

func (pg *PlanGenerator) stepEditCategory(source UnstructuredWithMetadata, t *UnstructuredWithMetadata) error {
	s := pg.stepConfiguration(stepOrphanMRs)
	t.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(t.Object))
	s.Patch.Files = append(s.Patch.Files, t.Metadata.Path)
	patchMap, err := computeJSONMergePathDoc(source.Object, t.Object)
	if err != nil {
		return err
	}
	return errors.Wrapf(pg.target.Put(UnstructuredWithMetadata{
		Object: unstructured.Unstructured{
			Object: addNameGVK(t.Object, patchMap),
		},
		Metadata: t.Metadata,
	}), errEditCategory, source.Metadata.Category, source.Object.GetName())
}
