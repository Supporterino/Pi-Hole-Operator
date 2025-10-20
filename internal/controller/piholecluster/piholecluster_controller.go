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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	supporterinodev1 "supporterino.de/pihole/api/v1"
	"supporterino.de/pihole/internal/pihole_api"
	"supporterino.de/pihole/internal/utils"
)

type ApiClientEntry struct {
	client *pihole_api.APIClient
	ip     string
}

type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// One APIClient per pod (keyed by the pod name)
	ApiClients map[string]*ApiClientEntry // key: pod name

	currentClusterName string
}

// RBAC permissions -------------------------------------------------------

//nolint:revive
//+kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="monitoring.coreos.com",resources=podmonitors,verbs=get;list;watch;create;update;patch;delete

// Reconcile ----------------------------------------------------------------

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling PiHoleCluster", "namespace", req.Namespace, "name", req.Name)

	// 0️⃣ Fetch the PiHoleCluster instance
	cluster, err := r.fetchCluster(ctx, req)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 1️⃣ Add finalizer if missing
	if !utils.ContainsString(cluster.Finalizers, finalizer) {
		return r.addFinalizer(ctx, cluster)
	}

	// 2️⃣ Handle deletion
	if cluster.GetDeletionTimestamp() != nil {
		return r.handleDelete(ctx, cluster)
	}

	// 3️⃣ Ensure all resources (API secret, PVCs, STS, Ingress, etc.)
	if err := r.ensureResources(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure resources: %w", err)
	}

	// 4️⃣ Update status
	ready, err := r.resourcesReady(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resources ready check failed: %w", err)
	}
	if err := r.updateStatus(ctx, cluster, ready, ""); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}

	log.V(0).Info("Reconciliation complete for PiHoleCluster", "name", cluster.Name)
	return ctrl.Result{}, nil
}

// fetchCluster retrieves the PiHoleCluster instance and logs errors.
func (r *Reconciler) fetchCluster(ctx context.Context, req ctrl.Request) (*supporterinodev1.PiHoleCluster, error) {
	cluster := &supporterinodev1.PiHoleCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			logf.FromContext(ctx).Info("PiHoleCluster not found, it may have been deleted")
			return nil, fmt.Errorf("PiHoleCluster (%w) not found, it may have been deleted", err)
		}
		return nil, fmt.Errorf("failed to get PiHoleCluster: %w", err)
	}
	logf.FromContext(ctx).V(1).Info("Fetched PiHoleCluster object")
	r.currentClusterName = cluster.Name
	return cluster, nil
}

// addFinalizer adds the finalizer to the PiHoleCluster and requeues.
func (r *Reconciler) addFinalizer(ctx context.Context, cluster *supporterinodev1.PiHoleCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.V(1).Info("Adding finalizer to PiHoleCluster")
	cluster.Finalizers = append(cluster.Finalizers, finalizer)
	if err := r.Update(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// ensureResources contains the original “resource‑creation” logic.
func (r *Reconciler) ensureResources(ctx context.Context, cluster *supporterinodev1.PiHoleCluster) error {
	// 0️⃣ API‑password secret
	if _, err := r.ensureAPISecret(ctx, cluster); err != nil {
		return fmt.Errorf("ensureAPISecret: %w", err)
	}

	// 1️⃣ PVC for RW pod
	if _, err := r.ensurePiHolePVC(ctx, cluster); err != nil {
		return fmt.Errorf("ensurePiHolePVC: %w", err)
	}

	// 2️⃣ RW StatefulSet
	if err := r.ensureReadWriteSTS(ctx, cluster); err != nil {
		return fmt.Errorf("ensureReadWriteSTS: %w", err)
	}

	// 3️⃣ RO StatefulSet (if replicas > 0)
	if cluster.Spec.Replicas > 0 {
		ready, err := r.areRWPodsReady(ctx, cluster)
		if err != nil {
			return fmt.Errorf("checking RW pod readiness: %w", err)
		}
		if !ready {
			return fmt.Errorf("rw pods not ready")
		}

		if err := r.ensureReadOnlySTS(ctx, cluster); err != nil {
			return fmt.Errorf("ensureReadOnlySTS: %w", err)
		}
	}

	// 4️⃣ API sync
	if cluster.Spec.Sync != nil && cluster.Spec.Replicas > 0 {
		ready, err := r.areROPodsReady(ctx, cluster)
		if err != nil {
			return fmt.Errorf("checking RO pod readiness: %w", err)
		}
		if !ready {
			return fmt.Errorf("ro pods not ready")
		}

		if err := r.syncAPIClients(ctx, cluster); err != nil {
			return fmt.Errorf("sync API clients: %w", err)
		}
		if err := r.performConfigSync(ctx, cluster); err != nil {
			return fmt.Errorf("perform config sync: %w", err)
		}
	}

	// 5️⃣ Ingress
	if cluster.Spec.Ingress != nil && cluster.Spec.Ingress.Enabled {
		if err := r.ensureIngress(ctx, cluster); err != nil {
			return fmt.Errorf("ensureIngress: %w", err)
		}
	}

	// 6️⃣ PodMonitor
	if cluster.Spec.Monitoring != nil &&
		cluster.Spec.Monitoring.PodMonitor != nil &&
		cluster.Spec.Monitoring.PodMonitor.Enabled {
		if err := r.ensurePodMonitor(ctx, cluster); err != nil {
			return fmt.Errorf("ensurePodMonitor: %w", err)
		}
	}

	// 7️⃣ DNS Service
	if err := r.ensureDNSService(ctx, cluster); err != nil {
		return fmt.Errorf("ensureDNSService: %w", err)
	}

	return nil
}

// Finalizer handling -------------------------------------------------------

func (r *Reconciler) handleDelete(ctx context.Context, cluster *supporterinodev1.PiHoleCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Close all API clients
	for name, pod := range r.ApiClients {
		pod.client.Close()
		log.Info("Closed API client for pod", "pod", name)
	}
	r.ApiClients = make(map[string]*ApiClientEntry)

	// Remove finalizer
	log.Info("Removing finalizer from PiHoleCluster")
	cluster.Finalizers = utils.RemoveString(cluster.Finalizers, finalizer)
	if err := r.Update(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// Helper ---------------------------------------------------------

func (r *Reconciler) clusterName() string { return r.currentClusterName }

// SetupWithManager -----------------------------------------------------

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&supporterinodev1.PiHoleCluster{}).
		Named("piholecluster").
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}
