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

package piholecluster

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
	"supporterino.de/pihole/internal/pihole_api"
	"supporterino.de/pihole/internal/utils"
)

const finalizerName = "piholecluster.finalizers.supporterino.de"

type PiHoleClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// One APIClient per pod (keyed by the pod name)
	ApiClients map[string]*pihole_api.APIClient

	currentClusterName string
}

// RBAC permissions -------------------------------------------------------

/*
+kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters,verbs=get;list;watch;create;update;patch;delete
+kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters/status,verbs=get;update;patch
+kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters/finalizers,verbs=update
+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
+kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
+kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;list;watch;create;update;patch;delete
+kubebuilder:rbac:groups="monitoring.coreos.com",resources=podmonitors,verbs=get;list;watch;create;update;patch;delete
*/

// Reconcile ----------------------------------------------------------------

func (r *PiHoleClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// 0️⃣ Fetch the PiHoleCluster instance
	piHoleCluster := &supporterinodev1alpha1.PiHoleCluster{}
	if err := r.Get(ctx, req.NamespacedName, piHoleCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil // deleted
		}
		return ctrl.Result{}, fmt.Errorf("failed to get PiHoleCluster: %w", err)
	}

	// 1️⃣ Add finalizer if missing
	if !utils.ContainsString(piHoleCluster.Finalizers, finalizerName) {
		piHoleCluster.Finalizers = append(piHoleCluster.Finalizers, finalizerName)
		if err := r.Update(ctx, piHoleCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	r.currentClusterName = piHoleCluster.Name

	// 2️⃣ Ensure the API‑password secret is available
	if _, err := r.ensureAPISecret(ctx, piHoleCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensureAPISecret: %w", err)
	}

	// 3️⃣ Ensure the PVC for the read‑write PiHole pod
	if _, err := r.ensurePiHolePVC(ctx, piHoleCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensurePiHolePVC: %w", err)
	}

	// 4️⃣ Create / update the read‑write StatefulSet
	if err := r.ensureReadWriteSTS(ctx, piHoleCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensureReadWriteSTS: %w", err)
	}

	// 5️⃣ Create / update the read‑only StatefulSet (if replicas > 0)
	if piHoleCluster.Spec.Replicas > 0 {
		ready, err := r.areRWPodsReady(ctx, piHoleCluster)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("checking RW pod readiness: %w", err)
		}
		if !ready {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		if err := r.ensureReadOnlySTS(ctx, piHoleCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensureReadOnlySTS: %w", err)
		}
	}

	// 6️⃣ Sync API clients and perform configuration sync
	if piHoleCluster.Spec.Sync != nil && piHoleCluster.Spec.Replicas > 0 {
		if err := r.syncAPIClients(ctx, piHoleCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("sync API clients: %w", err)
		}
		if err := r.performConfigSync(ctx, piHoleCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("perform config sync: %w", err)
		}
	}

	// 7️⃣ Ingress
	if piHoleCluster.Spec.Ingress != nil && piHoleCluster.Spec.Ingress.Enabled {
		if err := r.ensureIngress(ctx, piHoleCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensureIngress: %w", err)
		}
	}

	// 8️⃣ PodMonitor
	if piHoleCluster.Spec.Monitoring != nil && piHoleCluster.Spec.Monitoring.PodMonitor != nil &&
		piHoleCluster.Spec.Monitoring.PodMonitor.Enabled {
		if err := r.ensurePodMonitor(ctx, piHoleCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensurePodMonitor: %w", err)
		}
	}

	// 9️⃣ DNS Service
	if err := r.ensureDNSService(ctx, piHoleCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensureDNSService: %w", err)
	}

	// 10️⃣ Update status
	if ready, err := r.resourcesReady(ctx, piHoleCluster); err == nil {
		if err := r.updateStatus(ctx, piHoleCluster, ready, ""); err != nil {
			return ctrl.Result{}, fmt.Errorf("update status: %w", err)
		}
	} else {
		if err := r.updateStatus(ctx, piHoleCluster, false, fmt.Sprintf("resourcesReady check failed: %v", err)); err != nil {
			return ctrl.Result{}, fmt.Errorf("update status: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// Finalizer handling -------------------------------------------------------

func (r *PiHoleClusterReconciler) handleDelete(ctx context.Context, cluster *supporterinodev1alpha1.PiHoleCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Close all API clients
	for name, cli := range r.ApiClients {
		cli.Close()
		log.Info("closed API client for pod %s", name)
	}
	r.ApiClients = make(map[string]*pihole_api.APIClient)

	// Remove finalizer
	cluster.Finalizers = utils.RemoveString(cluster.Finalizers, finalizerName)
	if err := r.Update(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// Helper ---------------------------------------------------------

func (r *PiHoleClusterReconciler) clusterName() string { return r.currentClusterName }

// SetupWithManager -----------------------------------------------------

func (r *PiHoleClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&supporterinodev1alpha1.PiHoleCluster{}).
		Named("piholecluster").
		Complete(r)
}
