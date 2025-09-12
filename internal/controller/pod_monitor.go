package controller

import (
	"context"
	"reflect"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

// ensurePodMonitor creates or updates a PodMonitor that scrapes the exporter.
func (r *PiHoleClusterReconciler) ensurePodMonitor(ctx context.Context, piholecluster *supporterinodev1alpha1.PiHoleCluster) error {
	// No pod‑monitor requested → nothing to do
	if piholecluster.Spec.Monitoring == nil ||
		piholecluster.Spec.Monitoring.PodMonitor == nil ||
		!piholecluster.Spec.Monitoring.PodMonitor.Enabled {
		return nil
	}

	// Desired PodMonitor name – same as the cluster for simplicity
	pmName := piholecluster.Name

	// Build the PodMonitor spec
	desired := &monitoringv1.PodMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pmName,
			Namespace: piholecluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":     "pihole",
				"app.kubernetes.io/instance": piholecluster.Name,
			},
		},
		Spec: monitoringv1.PodMonitorSpec{
			// Select the exporter container via labels
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "pihole",
					"app.kubernetes.io/instance": piholecluster.Name,
				},
			}, // Service discovery via the headless service created by the STS
			PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
				{
					Port:     ptrTo("metrics"), // exporter port name
					Path:     "/metrics",       // default path
					Interval: "30s",
				},
			},
		},
	}

	// Owner reference so the PodMonitor gets garbage‑collected
	if err := ctrl.SetControllerReference(piholecluster, desired, r.Scheme); err != nil {
		return err
	}

	// Reconcile: create or update the PodMonitor
	existing := &monitoringv1.PodMonitor{}
	err := r.Get(ctx, types.NamespacedName{Name: pmName, Namespace: piholecluster.Namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Update only the spec that can change (selector, endpoints)
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		return r.Update(ctx, existing)
	}
	return nil
}
