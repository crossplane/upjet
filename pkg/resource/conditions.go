// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package resource

import (
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tferrors "github.com/crossplane/upjet/v2/pkg/terraform/errors"
)

// Condition constants.
const (
	TypeLastAsyncOperation = "LastAsyncOperation"
	TypeAsyncOperation     = "AsyncOperation"

	ReasonApplyFailure       xpv2.ConditionReason = "ApplyFailure"
	ReasonDestroyFailure     xpv2.ConditionReason = "DestroyFailure"
	ReasonAsyncCreateFailure xpv2.ConditionReason = "AsyncCreateFailure"
	ReasonAsyncUpdateFailure xpv2.ConditionReason = "AsyncUpdateFailure"
	ReasonAsyncDeleteFailure xpv2.ConditionReason = "AsyncDeleteFailure"
	ReasonSuccess            xpv2.ConditionReason = "Success"
	ReasonOngoing            xpv2.ConditionReason = "Ongoing"
	ReasonFinished           xpv2.ConditionReason = "Finished"
	ReasonResourceUpToDate   xpv2.ConditionReason = "UpToDate"
)

// LastAsyncOperationCondition returns the condition depending on the content
// of the error.
func LastAsyncOperationCondition(err error) xpv2.Condition {
	switch {
	case err == nil:
		return xpv2.Condition{
			Type:               TypeLastAsyncOperation,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             ReasonSuccess,
		}
	case tferrors.IsApplyFailed(err):
		return xpv2.Condition{
			Type:               TypeLastAsyncOperation,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             ReasonApplyFailure,
			Message:            err.Error(),
		}
	case tferrors.IsDestroyFailed(err):
		return xpv2.Condition{
			Type:               TypeLastAsyncOperation,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             ReasonDestroyFailure,
			Message:            err.Error(),
		}
	case tferrors.IsAsyncCreateFailed(err):
		return xpv2.Condition{
			Type:               TypeLastAsyncOperation,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             ReasonAsyncCreateFailure,
			Message:            err.Error(),
		}
	case tferrors.IsAsyncUpdateFailed(err):
		return xpv2.Condition{
			Type:               TypeLastAsyncOperation,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             ReasonAsyncUpdateFailure,
			Message:            err.Error(),
		}
	case tferrors.IsAsyncDeleteFailed(err):
		return xpv2.Condition{
			Type:               TypeLastAsyncOperation,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             ReasonAsyncDeleteFailure,
			Message:            err.Error(),
		}
	default:
		return xpv2.Condition{
			Type:               "Unknown",
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "Unknown",
			Message:            err.Error(),
		}
	}
}

// AsyncOperationFinishedCondition returns the condition TypeAsyncOperation Finished
// if the operation was finished
func AsyncOperationFinishedCondition() xpv2.Condition {
	return xpv2.Condition{
		Type:               TypeAsyncOperation,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonFinished,
	}
}

// AsyncOperationOngoingCondition returns the condition TypeAsyncOperation Ongoing
// if the operation is still running
func AsyncOperationOngoingCondition() xpv2.Condition {
	return xpv2.Condition{
		Type:               TypeAsyncOperation,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonOngoing,
	}
}

// UpToDateCondition returns the condition TypeAsyncOperation Ongoing
// if the operation is still running
func UpToDateCondition() xpv2.Condition {
	return xpv2.Condition{
		Type:               "Test",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonResourceUpToDate,
	}
}

// SetUpToDateCondition sets UpToDate condition if the resource is a test resource and up-to-date
func SetUpToDateCondition(mg xpresource.Managed, upToDate bool) {
	// At this point, we know that late initialization is done
	// After running refresh, if the resource is up-to-date and a test resource
	// we can set the UpToDate condition
	if upToDate && IsTest(mg) {
		mg.SetConditions(UpToDateCondition())
	}
}
