// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestPreconditionFailure(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "example.crossplane.io", Version: "v1beta1", Kind: "Thing"}

	cases := map[string]struct {
		reason          string
		err             error
		msg             string
		wantMessage     string
		wantDescription string
	}{
		"NilErrorWithMessage": {
			reason:          "The Terraform CLI and Plugin Framework branches pass no error, so the message has to stand on its own.",
			err:             nil,
			msg:             errCLIDiffNotImplemented,
			wantMessage:     errCLIDiffNotImplemented,
			wantDescription: errCLIDiffNotImplemented,
		},
		"NilErrorWithoutMessage": {
			reason:          "The unknown resource type branch passes neither, so the sentinel has to stand in for both.",
			err:             nil,
			msg:             "",
			wantMessage:     ErrDiffComputationNotSupported.Error(),
			wantDescription: ErrDiffComputationNotSupported.Error(),
		},
		"NilErrorWithBlankMessage": {
			reason:          "A message of only whitespace is no message at all.",
			err:             nil,
			msg:             "   ",
			wantMessage:     ErrDiffComputationNotSupported.Error(),
			wantDescription: ErrDiffComputationNotSupported.Error(),
		},
		"ErrorWithMessage": {
			reason:          "An error from a diff implementation is wrapped with the message naming the implementation.",
			err:             errors.New("boom"),
			msg:             errDiffPluginSDKv2,
			wantMessage:     errDiffPluginSDKv2 + ": boom",
			wantDescription: errDiffPluginSDKv2 + ": boom",
		},
		"ErrorWithoutMessage": {
			reason:          "Without a message the error stands on its own.",
			err:             errors.New("boom"),
			msg:             "",
			wantMessage:     "boom",
			wantDescription: "boom",
		},
	}

	s := &PlanService{log: logging.NewNopLogger()}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			recovered, got := safePreconditionFailure(s, tc.err, tc.msg, gvk)
			if recovered != nil {
				t.Fatalf("\n%s\npreconditionFailure(...): unexpected panic: %v", tc.reason, recovered)
			}

			st, ok := status.FromError(got)
			if !ok {
				t.Fatalf("\n%s\npreconditionFailure(...): want a gRPC status, got %v", tc.reason, got)
			}
			if diff := cmp.Diff(codes.FailedPrecondition, st.Code()); diff != "" {
				t.Errorf("\n%s\npreconditionFailure(...): -want code, +got code:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.wantMessage, st.Message()); diff != "" {
				t.Errorf("\n%s\npreconditionFailure(...): -want message, +got message:\n%s", tc.reason, diff)
			}

			details := st.Details()
			if len(details) != 1 {
				t.Fatalf("\n%s\npreconditionFailure(...): want one status detail, got %d", tc.reason, len(details))
			}
			pf, ok := details[0].(*errdetails.PreconditionFailure)
			if !ok {
				t.Fatalf("\n%s\npreconditionFailure(...): want a PreconditionFailure detail, got %T", tc.reason, details[0])
			}
			if len(pf.GetViolations()) != 1 {
				t.Fatalf("\n%s\npreconditionFailure(...): want one violation, got %d", tc.reason, len(pf.GetViolations()))
			}
			v := pf.GetViolations()[0]
			if diff := cmp.Diff(violationDiffComputationNotSupported, v.GetType()); diff != "" {
				t.Errorf("\n%s\npreconditionFailure(...): -want violation type, +got violation type:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(gvk.String(), v.GetSubject()); diff != "" {
				t.Errorf("\n%s\npreconditionFailure(...): -want violation subject, +got violation subject:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.wantDescription, v.GetDescription()); diff != "" {
				t.Errorf("\n%s\npreconditionFailure(...): -want violation description, +got violation description:\n%s", tc.reason, diff)
			}
		})
	}
}

func safePreconditionFailure(s *PlanService, err error, msg string, gvk schema.GroupVersionKind) (recovered any, result error) {
	defer func() { recovered = recover() }()
	return nil, s.preconditionFailure(err, msg, gvk)
}

func TestServesAPIGroup(t *testing.T) {
	gvk := func(group string) schema.GroupVersionKind {
		return schema.GroupVersionKind{Group: group, Version: "v1beta1", Kind: "Thing"}
	}

	cases := map[string]struct {
		reason string
		groups []string
		gvk    schema.GroupVersionKind
		want   bool
	}{
		"NoGroupsServesEverything": {
			reason: "A service that declares no groups is the monolith, or a caller that did not restrict it, and serves every group.",
			groups: nil,
			gvk:    gvk("iam.aws.upbound.io"),
			want:   true,
		},
		"ClusterScopedGroup": {
			reason: "A group is named by its first label, which is what a package knows about itself.",
			groups: []string{"ec2"},
			gvk:    gvk("ec2.aws.upbound.io"),
			want:   true,
		},
		"NamespacedGroup": {
			reason: "The namespaced flavour of a group has the same first label, so one name has to cover both.",
			groups: []string{"ec2"},
			gvk:    gvk("ec2.aws.m.upbound.io"),
			want:   true,
		},
		"AnotherGroup": {
			reason: "A package whose controllers do not reconcile the resource must decline it rather than answer from a configuration that covers every group anyway.",
			groups: []string{"ec2"},
			gvk:    gvk("iam.aws.upbound.io"),
			want:   false,
		},
		"OneOfSeveral": {
			reason: "A package serving several groups serves each of them.",
			groups: []string{"ec2", "iam"},
			gvk:    gvk("iam.aws.upbound.io"),
			want:   true,
		},
		"PrefixOfAServedGroup": {
			reason: "A group whose first label merely starts with a served one is a different group.",
			groups: []string{"ec2"},
			gvk:    gvk("ec2metadata.aws.upbound.io"),
			want:   false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := NewPlanService(nil, nil, logging.NewNopLogger(), nil, tc.groups)
			if diff := cmp.Diff(tc.want, s.servesAPIGroup(tc.gvk)); diff != "" {
				t.Errorf("\n%s\nservesAPIGroup(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestServedAPIGroups(t *testing.T) {
	// Sorted, because the list goes into the error a declined request gets and
	// a message that reorders itself between calls is a poor one.
	s := NewPlanService(nil, nil, logging.NewNopLogger(), nil, []string{"s3", "ec2", "iam"})
	if diff := cmp.Diff([]string{"ec2", "iam", "s3"}, s.servedAPIGroups()); diff != "" {
		t.Errorf("servedAPIGroups(): -want, +got:\n%s", diff)
	}
}
