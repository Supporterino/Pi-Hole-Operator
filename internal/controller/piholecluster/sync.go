package piholecluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
	"supporterino.de/pihole/internal/pihole_api"
	"supporterino.de/pihole/internal/utils"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func (r *PiHoleClusterReconciler) syncAPIClients(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	// 1️⃣ List all pods that belong to the StatefulSet
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(piHoleCluster.Namespace),
		client.MatchingLabels(map[string]string{
			"app.kubernetes.io/name": "pihole",
		})); err != nil {
		return fmt.Errorf("listing pihole pods: %w", err)
	}

	// 2️⃣ For each pod that is ready, create / refresh the client
	for _, p := range podList.Items {
		// Skip pods that are not ready yet
		if !utils.IsPodReady(&p) {
			continue
		}

		// Build the baseURL from the pod IP (Pi‑hole listens on 80 by default)
		baseURL := fmt.Sprintf("http://%s", p.Status.PodIP)

		// Re‑use existing client if it already exists
		if _, ok := r.ApiClients[p.Name]; ok {
			continue
		}

		// The password comes from the secret that you already create in ensureAPISecret()
		// (you can read it once and cache it if you want)
		secret, err := r.getAPISecret(ctx, piHoleCluster)
		if err != nil {
			return fmt.Errorf("reading API secret: %w", err)
		}
		password := string(secret.Data["password"])

		// Create the client
		apiClient := pihole_api.NewAPIClient(
			baseURL,
			password,
			10*time.Second, // request timeout
			true,           // skipTLSVerification – set to true if you use self‑signed certs
			ctx,
		)

		r.ApiClients[p.Name] = apiClient
	}

	// 3️⃣ (Optional) Clean up clients for pods that no longer exist
	for name := range r.ApiClients {
		found := false
		for _, p := range podList.Items {
			if p.Name == name {
				found = true
				break
			}
		}
		if !found {
			delete(r.ApiClients, name)
		}
	}

	return nil
}

// ReadWriteAPIClient returns the client for the RW pod (ordinal 0).
func (r *PiHoleClusterReconciler) ReadWriteAPIClient() (*pihole_api.APIClient, error) {
	// The RW pod is always <stsName-rw>-0
	rwPod := fmt.Sprintf("%s-rw-0", r.clusterName()) // clusterName() returns the CR name
	apiClient, ok := r.ApiClients[rwPod]
	if !ok {
		return nil, fmt.Errorf("read‑write API apiClient %s not found", rwPod)
	}
	return apiClient, nil
}

// ReadOnlyAPIClients returns a slice of clients for all RO pods.
func (r *PiHoleClusterReconciler) ReadOnlyAPIClients() ([]*pihole_api.APIClient, error) {
	var ro []*pihole_api.APIClient
	prefix := fmt.Sprintf("%s-ro-", r.clusterName())

	for name, apiClient := range r.ApiClients {
		if strings.HasPrefix(name, prefix) {
			ro = append(ro, apiClient)
		}
	}

	if len(ro) == 0 {
		return nil, fmt.Errorf("no read‑only API clients found")
	}
	return ro, nil
}

// shouldSyncNow returns true if the current time matches the schedule
// or if it has already passed since the last sync.
func (r *PiHoleClusterReconciler) shouldSyncNow(cronExpr string, lastSync metav1.Time) bool {
	if cronExpr == "" {
		return false
	}
	sched, err := cronParser.Parse(cronExpr)
	if err != nil {
		return false
	}

	now := time.Now()
	// If lastSync is zero, we need to sync immediately.
	if lastSync.IsZero() {
		return true
	}
	// Next scheduled time after the last sync.
	next := sched.Next(lastSync.Time)
	return !now.Before(next) // true if now >= next
}

// isPodSynced reports whether a pod (by name+UID) already appears in the status list.
func (r *PiHoleClusterReconciler) isPodSynced(cluster *supporterinodev1alpha1.PiHoleCluster, podName string, uid types.UID) bool {
	for _, s := range cluster.Status.SyncedPods {
		if s.Name == podName && s.UID == uid {
			return true
		}
	}
	return false
}

// addPodSynced appends a pod (name+UID) to the status list – idempotent.
func (r *PiHoleClusterReconciler) addPodSynced(cluster *supporterinodev1alpha1.PiHoleCluster, podName string, uid types.UID) {
	if r.isPodSynced(cluster, podName, uid) {
		return
	}
	cluster.Status.SyncedPods = append(cluster.Status.SyncedPods, supporterinodev1alpha1.SyncedPod{Name: podName, UID: uid})
}
