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

package controller

import (
	"context"

	"github.com/crossplane-contrib/terrajet/pkg/terraform"
)

type Client interface {
	ApplyAsync() error
	Apply(ctx context.Context) (terraform.ApplyResult, error)
	DestroyAsync() error
	Destroy(ctx context.Context) error
	Refresh(ctx context.Context) (terraform.RefreshResult, error)
	Plan(ctx context.Context) (terraform.PlanResult, error)
}
