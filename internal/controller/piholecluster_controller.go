/*
Copyright 2025.

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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

// PiHoleClusterReconciler reconciles a PiHoleCluster object
type PiHoleClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters/finalizers,verbs=update
func (r *PiHoleClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// ------------------------------------------------------------------
	// 0️⃣ Fetch the PiHoleCluster instance
	// ------------------------------------------------------------------
	piholecluster := &supporterinodev1alpha1.PiHoleCluster{}
	if err := r.Get(ctx, req.NamespacedName, piholecluster); err != nil {
		if apierrors.IsNotFound(err) {
			// Resource deleted – nothing to do
			return ctrl.Result{}, nil

		}
		return ctrl.Result{}, fmt.Errorf("failed to get PiHoleCluster: %w", err)
	}

	// ------------------------------------------------------------------
	// 1️⃣ Ensure the API‑password secret is available
	// ------------------------------------------------------------------
	_, err := r.ensureAPISecret(ctx, piholecluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensureAPISecret: %w", err)
	}

	// ------------------------------------------------------------------
	// 2️⃣ Ensure the PVC for the read‑write PiHole pod
	// ------------------------------------------------------------------
	_, err = r.ensurePiHolePVC(ctx, piholecluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensurePiHolePVC: %w", err)
	}

	// ------------------------------------------------------------------
	// 3️⃣ Create / update the read‑write StatefulSet
	// ------------------------------------------------------------------
	if err := r.ensureReadWriteSTS(ctx, piholecluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensureReadWriteSTS: %w", err)
	}

	// ------------------------------------------------------------------
	// 4️⃣ Create / update the read‑only StatefulSet (if replicas > 0)
	// ------------------------------------------------------------------
	if piholecluster.Spec.Sync != nil && piholecluster.Spec.Replicas > 0 {
		if err := r.ensureReadOnlySTS(ctx, piholecluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensureReadOnlySTS: %w", err)
		}
	}

	// ------------------------------------------------------------------
	// 5️⃣ Create / update the Ingress (if enabled)
	// ------------------------------------------------------------------
	if piholecluster.Spec.Ingress != nil && piholecluster.Spec.Ingress.Enabled {
		if err := r.ensureIngress(ctx, piholecluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensureIngress: %w", err)
		}
	}

	// ------------------------------------------------------------------
	// 6️⃣ Create / update the PodMonitor (if enabled)
	// ------------------------------------------------------------------
	if piholecluster.Spec.Monitoring != nil && piholecluster.Spec.Monitoring.PodMonitor != nil &&
		piholecluster.Spec.Monitoring.PodMonitor.Enabled {
		if err := r.ensurePodMonitor(ctx, piholecluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensurePodMonitor: %w", err)
		}
	}

	// ------------------------------------------------------------------
	// 7️⃣ Create / update the central DNS Service
	// ------------------------------------------------------------------
	if err := r.ensureDNSService(ctx, piholecluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensureDNSService: %w", err)
	}

	// ------------------------------------------------------------------
	// 8️⃣ All done – return success
	// ------------------------------------------------------------------
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PiHoleClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&supporterinodev1alpha1.PiHoleCluster{}).
		Named("piholecluster").
		Complete(r)
}
