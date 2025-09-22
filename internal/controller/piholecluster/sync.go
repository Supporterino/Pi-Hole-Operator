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

	supporterinodev1 "supporterino.de/pihole/api/v1"
	"supporterino.de/pihole/internal/pihole_api"
	"supporterino.de/pihole/internal/utils"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ---------------------------------------------------------------------------
// Configuration sync
// ---------------------------------------------------------------------------

func (r *Reconciler) performConfigSync(ctx context.Context, cluster *supporterinodev1.PiHoleCluster) error {
	log := logf.FromContext(ctx)
	log.Info("Starting configuration sync", "cluster", cluster.Name)

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
		podMap[fmt.Sprintf("http://%s", podList.Items[i].Status.PodIP)] = &podList.Items[i]
	}
	log.V(1).Info("Found RO pods", "count", len(podMap))

	// 2️⃣ Detect pods that need a sync
	var podsNeedingSync []string
	for _, pod := range podMap {
		if !r.isPodSynced(cluster, pod.Name, pod.GetUID()) {
			podsNeedingSync = append(podsNeedingSync, pod.Name)
		}
	}
	log.V(1).Info("Pods needing sync", "count", len(podsNeedingSync))

	runSync := len(podsNeedingSync) > 0 || r.shouldSyncNow(cluster.Spec.Sync.Cron, cluster.Status.LastConfigSyncTime)

	if !runSync {
		log.Info("Config sync not required", "cron", cluster.Spec.Sync.Cron, "newPods", len(podsNeedingSync))
		return nil
	}
	log.Info("Running config sync", "cron", cluster.Spec.Sync.Cron, "newPods", len(podsNeedingSync))

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
	log.V(1).Info("Teleporter binary downloaded")

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
			log.Info("Teleporter sync succeeded for", "pod", roClient.BaseURL)
		}
	}

	// 6️⃣ Update status
	cluster.Status.ConfigSynced = true
	cluster.Status.LastConfigSyncTime = metav1.Now()
	if err := utils.UpdateClusterStatusWithRetry(ctx, r.Client, cluster, log); err != nil {
		log.V(1).Info(fmt.Sprintf("failed to update sync status: %v", err))
	}
	log.Info("Configuration sync completed successfully")

	// 7️⃣ Sync ad‑lists if enabled
	if cluster.Spec.Sync.AdLists && len(cluster.Spec.Config.AdLists) > 0 {
		log.Info("Starting ad-list sync", "cluster", cluster.Name)
		if err := r.syncAdLists(ctx, cluster); err != nil {
			return fmt.Errorf("sync ad lists: %w", err)
		}

		// 6️⃣ Update status
		cluster.Status.AdListSynced = true
		cluster.Status.LastAdListSyncTime = metav1.Now()
		if err := utils.UpdateClusterStatusWithRetry(ctx, r.Client, cluster, log); err != nil {
			log.V(1).Info(fmt.Sprintf("failed to update sync status: %v", err))
		}

		log.Info("Configuration sync completed successfully")
	}

	return nil
}

// ---------------------------------------------------------------------------
// Ad‑list sync
// ---------------------------------------------------------------------------

func (r *Reconciler) syncAdLists(ctx context.Context, cluster *supporterinodev1.PiHoleCluster) error {
	log := logf.FromContext(ctx)

	// ------------------------------------------------------------
	// 1️⃣ Get RW client & the *current* list after any creations
	// ------------------------------------------------------------
	rwClient, err := r.ReadWriteAPIClient()
	if err != nil {
		return fmt.Errorf("read‑write client unavailable: %w", err)
	}

	// First ensure all CRD URLs exist on RW
	crdAddrs := make(map[string]struct{}, len(cluster.Spec.Config.AdLists))
	for _, addr := range cluster.Spec.Config.AdLists {
		crdAddrs[addr] = struct{}{}
	}

	// Create missing lists on RW
	rwLists, err := rwClient.GetAdLists(ctx)
	if err != nil {
		return fmt.Errorf("fetching ad‑lists from RW: %w", err)
	}
	existing := make(map[string]struct{}, len(rwLists))
	for _, l := range rwLists {
		existing[l.Address] = struct{}{}
	}
	for addr := range crdAddrs {
		if _, ok := existing[addr]; !ok {
			log.V(1).Info("Adding ad-list to RW replica", "addr", addr)
			if err := rwClient.PostAdList(ctx, addr); err != nil {
				return fmt.Errorf("posting ad‑list %s to RW: %w", addr, err)
			}
		}
	}

	// After creation fetch the *updated* RW list
	updatedRW, err := rwClient.GetAdLists(ctx)
	if err != nil {
		return fmt.Errorf("refetching ad‑lists from RW: %w", err)
	}

	// ------------------------------------------------------------
	// 2️⃣ Delete any operator‑created lists that are no longer in CRD
	// ------------------------------------------------------------
	for _, l := range updatedRW {
		if l.Comment != nil && *l.Comment == "Created by PiHole operator" {
			if _, ok := crdAddrs[l.Address]; !ok {
				log.V(1).Info("Deleting ad-list from RW replica", "addr", l.Address)
				if err := rwClient.DeleteAdList(ctx, l.Address); err != nil {
					return fmt.Errorf("deleting ad‑list %s from RW: %w", l.Address, err)
				}
			}
		}
	}

	err = rwClient.RunGravity(ctx)
	if err != nil {
		return fmt.Errorf("updating gravity on RW: %w", err)
	}

	// ------------------------------------------------------------
	// 3️⃣ Sync the RO clients to match the *final* RW list
	// ------------------------------------------------------------
	rwList, err := rwClient.GetAdLists(ctx)
	if err != nil {
		return fmt.Errorf("fetching ad‑lists from RW: %w", err)
	}
	finalRW := make(map[string]struct{}, len(rwList))
	for _, l := range rwList {
		finalRW[l.Address] = struct{}{}
	}

	roClients, err := r.ReadOnlyAPIClients()
	if err != nil {
		return fmt.Errorf("listing read‑only clients: %w", err)
	}

	for _, ro := range roClients {
		// 3a) Ensure all CRD URLs exist on this RO
		roLists, err := ro.GetAdLists(ctx)
		if err != nil {
			return fmt.Errorf("fetching ad‑lists from RO: %w", err)
		}
		roExisting := make(map[string]struct{}, len(roLists))
		for _, l := range roLists {
			roExisting[l.Address] = struct{}{}
		}
		for addr := range finalRW {
			if _, ok := roExisting[addr]; !ok {
				if err := ro.PostAdList(ctx, addr); err != nil {
					log.V(1).Info("Adding ad-list to RO replica", "addr", addr, "instance", ro.BaseURL)
					return fmt.Errorf("posting ad‑list %s to RO: %w", addr, err)
				}
			}
		}

		// 3b) Delete any operator‑created lists that are no longer in the RW replica
		roLists, err = ro.GetAdLists(ctx)
		if err != nil {
			return fmt.Errorf("refetching ad‑lists from RO: %w", err)
		}
		for _, l := range roLists {
			if _, ok := finalRW[l.Address]; !ok {
				log.V(1).Info("Deleting ad-list from RO replica", "addr", l.Address, "instance", ro.BaseURL)
				if err := ro.DeleteAdList(ctx, l.Address); err != nil {
					return fmt.Errorf("deleting ad‑list %s from RO: %w", l.Address, err)
				}
			}
		}

		err = ro.RunGravity(ctx)
		if err != nil {
			return fmt.Errorf("updating gravity on ro(%s): %w", ro.BaseURL, err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// API client management
// ---------------------------------------------------------------------------

func (r *Reconciler) syncAPIClients(ctx context.Context, piHoleCluster *supporterinodev1.PiHoleCluster) error {
	log := logf.FromContext(ctx)
	log.Info("Syncing API clients", "cluster", piHoleCluster.Name)

	// 1️⃣ List all Pi‑Hole pods
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(piHoleCluster.Namespace), client.MatchingLabels(map[string]string{"app.kubernetes.io/name": "pihole"})); err != nil {
		return fmt.Errorf("listing pihole pods: %w", err)
	}
	log.V(1).Info(fmt.Sprintf("found %d pihole pod(s)", len(podList.Items)))

	// 2️⃣ Create / refresh a client for every ready pod
	for _, p := range podList.Items {
		if !utils.IsPodReady(&p) { // skip pods that are not ready
			log.V(1).Info("skipping pod – not ready", "pod", p.Name)
			continue
		}

		ip := p.Status.PodIP
		if ip == "" {
			log.V(1).Info("skipping pod – no IP yet", "pod", p.Name)
			continue
		}

		baseURL := fmt.Sprintf("http://%s", ip)

		entry, exists := r.ApiClients[p.Name]
		if exists && entry.ip == ip {
			log.V(1).Info("re‑using existing client", "pod", p.Name, "ip", ip)
			continue
		}

		if exists {
			log.V(1).Info("pod restarted – recreating client", "pod", p.Name, "oldIP", entry.ip, "newIP", ip)
			entry.client.Close()
		}

		secret, err := r.getAPISecret(ctx, piHoleCluster)
		if err != nil {
			return fmt.Errorf("reading API secret: %w", err)
		}
		password := string(secret.Data["password"])

		apiClient := pihole_api.NewAPIClient(baseURL, password, 10*time.Second, // request timeout
			true, // skipTLSVerification – true if you use self‑signed certs
			ctx)

		r.ApiClients[p.Name] = &ApiClientEntry{client: apiClient, ip: ip}
		log.V(1).Info("created/updated API client", "pod", p.Name, "ip", ip)
	}

	// 3️⃣ Remove clients for pods that no longer exist
	for name := range r.ApiClients {
		found := false
		for _, p := range podList.Items {
			if p.Name == name {
				found = true
				break
			}
		}
		if !found {
			entry := r.ApiClients[name]
			delete(r.ApiClients, name)
			if entry.client != nil {
				entry.client.Close()
			}
			log.V(1).Info("removed stale API client", "pod", name)
		}
	}

	return nil
}

// Read‑write client (the RW pod is always <stsName-rw>-0)
func (r *Reconciler) ReadWriteAPIClient() (*pihole_api.APIClient, error) {
	rwPod := fmt.Sprintf("%s-rw-0", r.clusterName()) // clusterName() returns the CR name
	pod, ok := r.ApiClients[rwPod]
	if !ok {
		return nil, fmt.Errorf("read‑write API client %s not found", rwPod)
	}
	return pod.client, nil
}

// Read‑only clients (all RO pods)
func (r *Reconciler) ReadOnlyAPIClients() ([]*pihole_api.APIClient, error) {
	var ro []*pihole_api.APIClient
	prefix := fmt.Sprintf("%s-ro-", r.clusterName())

	for name, pod := range r.ApiClients {
		if strings.HasPrefix(name, prefix) {
			ro = append(ro, pod.client)
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

func (r *Reconciler) isPodSynced(cluster *supporterinodev1.PiHoleCluster, podName string, uid types.UID) bool {
	for _, s := range cluster.Status.SyncedPods {
		if s.Name == podName && s.UID == uid {
			return true
		}
	}
	return false
}

func (r *Reconciler) addPodSynced(cluster *supporterinodev1.PiHoleCluster, podName string, uid types.UID) {
	if r.isPodSynced(cluster, podName, uid) {
		return
	}
	cluster.Status.SyncedPods = append(cluster.Status.SyncedPods, supporterinodev1.SyncedPod{Name: podName, UID: uid})
}
