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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
	"supporterino.de/pihole/internal/pihole_api"
	"supporterino.de/pihole/internal/utils"
)

const (
	finalizerName = "piholecluster.finalizers.supporterino.de"
)

// PiHoleClusterReconciler reconciles a PiHoleCluster object
type PiHoleClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// One APIClient per pod (keyed by the pod name)
	ApiClients map[string]*pihole_api.APIClient

	currentClusterName string
}

// +kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=supporterino.de,resources=piholeclusters/finalizers,verbs=update
func (r *PiHoleClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
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

	if !utils.ContainsString(piholecluster.Finalizers, finalizerName) {
		piholecluster.Finalizers = append(piholecluster.Finalizers, finalizerName)
		if err := r.Update(ctx, piholecluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		// Re‑queue to run the reconcile again with the finalizer in place
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	r.currentClusterName = piholecluster.Name

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
		// Wait until the RW pod is ready before creating/updating RO
		ready, err := r.isRWPodReady(ctx, piholecluster)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("checking RW pod readiness: %w", err)
		}
		if !ready {
			// Re‑queue after a short delay so we don't hammer the API
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if err := r.ensureReadOnlySTS(ctx, piholecluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensureReadOnlySTS: %w", err)
		}
	}

	// ------------------------------------------------------------------
	// 4️⃣ Create / update the read‑only StatefulSet (if replicas > 0)
	// ------------------------------------------------------------------
	if piholecluster.Spec.Sync != nil && piholecluster.Spec.Replicas > 0 {
		// Wait until the RW pod is ready before creating/updating RO
		ready, err := r.areROPodsReady(ctx, piholecluster)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("checking RO pod readiness: %w", err)
		}
		if !ready {
			// Re‑queue after a short delay so we don't hammer the API
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if err := r.syncAPIClients(ctx, piholecluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("sync API clients: %w", err)
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

	// ------------------------------------------------------------
	//  Sync configuration – only when a new RO pod appears or the cron matches.
	// ------------------------------------------------------------
	if piholecluster.Spec.Sync != nil && piholecluster.Spec.Sync.Config {
		// 1️⃣ Build a map of existing RO pods (name → pod object)
		podList := &corev1.PodList{}
		if err := r.List(ctx, podList,
			client.InNamespace(piholecluster.Namespace),
			client.MatchingLabels(map[string]string{
				"app.kubernetes.io/name": fmt.Sprintf("%s-ro", piholecluster.Name),
			})); err != nil {
			return ctrl.Result{}, fmt.Errorf("listing RO pods: %w", err)
		}
		podMap := make(map[string]*corev1.Pod)
		for i, p := range podList.Items {
			// capture pointer to the slice element
			podMap[p.Name] = &podList.Items[i]
		}

		// 2️⃣ Detect which RO pods need a sync
		var podsNeedingSync []string // names of pods that are ready but not yet synced
		for podName, pod := range podMap {
			if !r.isPodSynced(piholecluster, podName, pod.UID) {
				podsNeedingSync = append(podsNeedingSync, podName)
			}
		}

		// 3️⃣ Decide if we should sync now
		runSync := len(podsNeedingSync) > 0 || r.shouldSyncNow(piholecluster.Spec.Sync.Cron, piholecluster.Status.LastSyncTime)

		if runSync {
			log.Info(fmt.Sprintf("running config sync (cron=%q, newPods=%d)", *piholecluster.Spec.Sync, len(podsNeedingSync)))

			// 4️⃣ Get the RW client
			rwClient, err := r.ReadWriteAPIClient()
			if err != nil {
				log.V(0).Info(fmt.Sprintf("read‑write client not ready: %v", err))
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}

			// 5️⃣ Download the binary from RW
			ctxSync, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			data, err := rwClient.DownloadTeleporter(ctxSync)
			if err != nil {
				log.Error(err, "download teleporter.")
				return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
			}

			// 6️⃣ Upload to every RO pod that needs it
			roClients, err := r.ReadOnlyAPIClients()
			if err != nil {
				log.Error(err, "listing read‑only clients: %v")
				// Decide whether to requeue – here we just bail out and let the next reconcile try again
				return ctrl.Result{}, nil
			}

			for _, roClient := range roClients {
				// … your upload logic …
				// Find the pod object to get its UID
				pod, ok := podMap[roClient.BaseURL] // BaseURL was set to the pod name in syncAPIClients()
				if !ok {
					log.V(0).Info(fmt.Sprintf("cannot find pod %s in list – skipping", roClient.BaseURL))
					continue
				}
				if err := roClient.UploadTeleporter(ctxSync, data); err != nil {
					log.Error(err, "upload to %s failed.", "baseURL", roClient.BaseURL)
				} else {
					// Mark this pod as synced
					r.addPodSynced(piholecluster, pod.Name, pod.UID)
					log.Info(fmt.Sprintf("teleporter sync succeeded for %s", roClient.BaseURL))
				}
			}

			// 7️⃣ Update status
			piholecluster.Status.ConfigSynced = true
			piholecluster.Status.LastSyncTime = metav1.Now()
			if err := r.Status().Update(ctx, piholecluster); err != nil {
				log.V(1).Info(fmt.Sprintf("failed to update sync status: %v", err))
			}
		} else {
			log.Info(fmt.Sprintf("sync not needed (cron=%q, newPods=%d)", *piholecluster.Spec.Sync, len(podsNeedingSync)))
		}
	}

	// ------------------------------------------------------------------
	// 8️⃣ Update the status field ResourcesReady
	// ------------------------------------------------------------------
	ready, err := r.resourcesReady(ctx, piholecluster)
	var errMsg string
	if err != nil {
		errMsg = fmt.Sprintf("resourcesReady check failed: %v", err)
	}
	if err := r.updateStatus(ctx, piholecluster, ready, errMsg); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}

	// ------------------------------------------------------------------
	// 9️⃣ All done – return success
	// ------------------------------------------------------------------
	return ctrl.Result{}, nil
}

func (r *PiHoleClusterReconciler) handleDelete(ctx context.Context, cluster *supporterinodev1alpha1.PiHoleCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1️⃣ Close all API clients – this will drop idle connections
	for name, cli := range r.ApiClients {
		cli.Close()
		log.Info("closed API client for pod %s", name)
	}
	r.ApiClients = make(map[string]*pihole_api.APIClient)

	// 3️⃣ Remove the finalizer so Kubernetes can actually delete the CR
	cluster.Finalizers = utils.RemoveString(cluster.Finalizers, finalizerName)
	if err := r.Update(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}

	// Re‑queue to allow the delete to finish
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *PiHoleClusterReconciler) clusterName() string {
	return r.currentClusterName
}

// SetupWithManager sets up the controller with the Manager.
func (r *PiHoleClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&supporterinodev1alpha1.PiHoleCluster{}).
		Named("piholecluster").
		Complete(r)
}
