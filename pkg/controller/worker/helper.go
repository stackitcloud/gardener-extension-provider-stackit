// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package worker

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
)

func (w *workerDelegate) decodeWorkerProviderStatus() (*stackitv1alpha1.WorkerStatus, error) {
	workerStatus := &stackitv1alpha1.WorkerStatus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WorkerStatus",
			APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
		},
	}

	if w.worker.Status.ProviderStatus == nil {
		return workerStatus, nil
	}

	marshaled, err := w.worker.Status.GetProviderStatus().MarshalJSON()
	if err != nil {
		return nil, err
	}
	if _, _, err := w.decoder.Decode(marshaled, nil, workerStatus); err != nil {
		return nil, fmt.Errorf("could not decode WorkerStatus %q: %w", k8sclient.ObjectKeyFromObject(w.worker), err)
	}

	return workerStatus, nil
}

func (w *workerDelegate) updateWorkerProviderStatus(ctx context.Context, workerStatus *stackitv1alpha1.WorkerStatus) error {
	patch := k8sclient.MergeFrom(w.worker.DeepCopy())
	w.worker.Status.ProviderStatus = &runtime.RawExtension{Object: workerStatus}
	return w.seedClient.Status().Patch(ctx, w.worker, patch)
}

// ClusterTechnicalName returns the technical name of the cluster this worker belongs.
func (w *workerDelegate) ClusterTechnicalName() string {
	return w.cluster.Shoot.Status.TechnicalID
}
