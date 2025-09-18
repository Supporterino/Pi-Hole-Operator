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
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
	"supporterino.de/pihole/internal/pihole_api"
	"supporterino.de/pihole/internal/utils"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ---------------------------------------------------------------------------
// Configuration sync
// ---------------------------------------------------------------------------

func (r *Reconciler) performConfigSync(ctx context.Context, cluster *supporterinodev1alpha1.PiHoleCluster) error {
	log := logf.FromContext(ctx)

	// 1️⃣ Build a map of existing RO pods
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(cluster.Namespace), client.MatchingLabels(map[string]string{
		"app.kubernetes.io/instance":  cluster.Name,
		"app.kubernetes.io/name":      "pihole",
		"supporterino.de/pihole-role": "readonly",
	})); err != nil {
		return fmt.Errorf("listing RO pods: %w", err)
	}
	podMap := make(map[string]*corev1.Pod)
	for i := range podList.Items {
		//podMap[podList.Items[i].Name] = &podList.Items[i]
		podMap[fmt.Sprintf("http://%s", podList.Items[i].Status.PodIP)] = &podList.Items[i]
	}

	// 2️⃣ Detect pods that need a sync
	var podsNeedingSync []string
	log.V(1).Info("syncing pods", "count", len(podMap))
	for podName, pod := range podMap {
		log.V(1).Info("syncing pod", "pod", pod.Name, "uid", pod.GetUID())
		if !r.isPodSynced(cluster, pod.Name, pod.GetUID()) {
			podsNeedingSync = append(podsNeedingSync, podName)
		}
	}

	runSync := len(podsNeedingSync) > 0 || r.shouldSyncNow(cluster.Spec.Sync.Cron, cluster.Status.LastSyncTime)

	if !runSync {
		log.Info(fmt.Sprintf("config sync not needed (cron=%q, newPods=%d)", cluster.Spec.Sync.Cron, len(podsNeedingSync)))
		return nil
	}
	log.Info(fmt.Sprintf("running config sync (cron=%q, newPods=%d)", cluster.Spec.Sync.Cron, len(podsNeedingSync)))

	// 3️⃣ Read‑write client
	rwClient, err := r.ReadWriteAPIClient()
	if err != nil {
		log.V(0).Info(fmt.Sprintf("read‑write client not ready: %v", err))
		return fmt.Errorf("read‑write client unavailable")
	}

	// 4️⃣ Download teleporter binary
	ctxSync, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := rwClient.DownloadTeleporter(ctxSync)
	if err != nil {
		log.Error(err, "download teleporter")
		return fmt.Errorf("download teleporter: %w", err)
	}

	// 5️⃣ Upload to every RO pod that needs it
	roClients, err := r.ReadOnlyAPIClients()
	if err != nil {
		log.Error(err, "listing read‑only clients")
		return fmt.Errorf("read‑only clients unavailable")
	}
	for _, roClient := range roClients {
		pod, ok := podMap[roClient.BaseURL] // BaseURL is set to the pod name in syncAPIClients()
		if !ok {
			log.V(0).Info(fmt.Sprintf("cannot find pod %s in list – skipping", roClient.BaseURL))
			continue
		}
		if err := roClient.UploadTeleporter(ctxSync, data); err != nil {
			log.Error(err, "upload to pod failed", "pod", roClient.BaseURL)
		} else {
			r.addPodSynced(cluster, pod.Name, pod.UID)
			log.Info(fmt.Sprintf("teleporter sync succeeded for %s", roClient.BaseURL))
		}
	}

	// 6️⃣ Update status
	cluster.Status.ConfigSynced = true
	cluster.Status.LastSyncTime = metav1.Now()
	if err := r.Status().Update(ctx, cluster); err != nil {
		log.V(1).Info(fmt.Sprintf("failed to update sync status: %v", err))
	}

	return nil
}

// ---------------------------------------------------------------------------
// API client management
// ---------------------------------------------------------------------------

func (r *Reconciler) syncAPIClients(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	// 1️⃣ List all pods that belong to the StatefulSet
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(piHoleCluster.Namespace), client.MatchingLabels(map[string]string{
		"app.kubernetes.io/name": "pihole",
	})); err != nil {
		return fmt.Errorf("listing pihole pods: %w", err)
	}

	// 2️⃣ For each pod that is ready, create / refresh the client
	for _, p := range podList.Items {
		if !utils.IsPodReady(&p) { // skip not‑ready pods
			continue
		}

		baseURL := fmt.Sprintf("http://%s", p.Status.PodIP)

		if _, ok := r.ApiClients[p.Name]; ok { // reuse existing client
			continue
		}

		secret, err := r.getAPISecret(ctx, piHoleCluster)
		if err != nil {
			return fmt.Errorf("reading API secret: %w", err)
		}
		password := string(secret.Data["password"])

		apiClient := pihole_api.NewAPIClient(baseURL, password, 10*time.Second, // request timeout
			true, // skipTLSVerification – set to true if you use self‑signed certs
			ctx)

		r.ApiClients[p.Name] = apiClient
	}

	// 3️⃣ Clean up clients for pods that no longer exist
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

// Read‑write client (the RW pod is always <stsName-rw>-0)
func (r *Reconciler) ReadWriteAPIClient() (*pihole_api.APIClient, error) {
	rwPod := fmt.Sprintf("%s-rw-0", r.clusterName()) // clusterName() returns the CR name
	apiClient, ok := r.ApiClients[rwPod]
	if !ok {
		return nil, fmt.Errorf("read‑write API client %s not found", rwPod)
	}
	return apiClient, nil
}

// Read‑only clients (all RO pods)
func (r *Reconciler) ReadOnlyAPIClients() ([]*pihole_api.APIClient, error) {
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

// ---------------------------------------------------------------------------
// Scheduling helpers
// ---------------------------------------------------------------------------

func (r *Reconciler) shouldSyncNow(cronExpr string, lastSync metav1.Time) bool {
	if cronExpr == "" {
		return false
	}
	sched, err := cronParser.Parse(cronExpr)
	if err != nil {
		return false
	}

	now := time.Now()
	if lastSync.IsZero() { // never synced before → sync immediately
		return true
	}
	next := sched.Next(lastSync.Time)
	return !now.Before(next) // true if now >= next
}

// ---------------------------------------------------------------------------
// Pod sync status helpers
// ---------------------------------------------------------------------------

func (r *Reconciler) isPodSynced(cluster *supporterinodev1alpha1.PiHoleCluster, podName string, uid types.UID) bool {
	for _, s := range cluster.Status.SyncedPods {
		if s.Name == podName && s.UID == uid {
			return true
		}
	}
	return false
}

func (r *Reconciler) addPodSynced(cluster *supporterinodev1alpha1.PiHoleCluster, podName string, uid types.UID) {
	if r.isPodSynced(cluster, podName, uid) {
		return
	}
	cluster.Status.SyncedPods = append(cluster.Status.SyncedPods, supporterinodev1alpha1.SyncedPod{Name: podName, UID: uid})
}
