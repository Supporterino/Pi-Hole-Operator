package controller

import (
	"time"

	"github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

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
