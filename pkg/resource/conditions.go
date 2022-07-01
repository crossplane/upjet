/*
Copyright 2021 Upbound Inc.
*/

package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"

	tferrors "github.com/upbound/upjet/pkg/terraform/errors"
)

// Condition constants.
const (
	TypeLastAsyncOperation = "LastAsyncOperation"
	TypeAsyncOperation     = "AsyncOperation"

	ReasonApplyFailure     xpv1.ConditionReason = "ApplyFailure"
	ReasonDestroyFailure   xpv1.ConditionReason = "DestroyFailure"
	ReasonSuccess          xpv1.ConditionReason = "Success"
	ReasonOngoing          xpv1.ConditionReason = "Ongoing"
	ReasonFinished         xpv1.ConditionReason = "Finished"
	ReasonResourceUpToDate xpv1.ConditionReason = "UpToDate"
)

// LastAsyncOperationCondition returns the condition depending on the content
// of the error.
func LastAsyncOperationCondition(err error) xpv1.Condition {
	switch {
	case err == nil:
		return xpv1.Condition{
			Type:               TypeLastAsyncOperation,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             ReasonSuccess,
		}
	case tferrors.IsApplyFailed(err):
		return xpv1.Condition{
			Type:               TypeLastAsyncOperation,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             ReasonApplyFailure,
			Message:            err.Error(),
		}
	case tferrors.IsDestroyFailed(err):
		return xpv1.Condition{
			Type:               TypeLastAsyncOperation,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             ReasonDestroyFailure,
			Message:            err.Error(),
		}
	default:
		return xpv1.Condition{
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
func AsyncOperationFinishedCondition() xpv1.Condition {
	return xpv1.Condition{
		Type:               TypeAsyncOperation,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonFinished,
	}
}

// AsyncOperationOngoingCondition returns the condition TypeAsyncOperation Ongoing
// if the operation is still running
func AsyncOperationOngoingCondition() xpv1.Condition {
	return xpv1.Condition{
		Type:               TypeAsyncOperation,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonOngoing,
	}
}

// UpToDateCondition returns the condition TypeAsyncOperation Ongoing
// if the operation is still running
func UpToDateCondition() xpv1.Condition {
	return xpv1.Condition{
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
