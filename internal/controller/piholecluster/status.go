package piholecluster

import (
	"context"
	"fmt"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

func (r *PiHoleClusterReconciler) resourcesReady(ctx context.Context, piholecluster *supporterinodev1alpha1.PiHoleCluster) (bool, error) {
	// 1️⃣ RW StatefulSet
	rwSTS := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-rw", piholecluster.Name), Namespace: piholecluster.Namespace}, rwSTS); err != nil {
		return false, apierrors.NewNotFound(
			schema.ParseGroupResource(rwSTS.GroupVersionKind().String()),
			fmt.Sprintf("%s-rw", piholecluster.Name))
	}
	if rwSTS.Status.ReadyReplicas != 1 {
		return false, nil
	}

	// 2️⃣ RO StatefulSet (if any)
	if piholecluster.Spec.Sync != nil && piholecluster.Spec.Replicas > 0 {
		roSTS := &appsv1.StatefulSet{}
		if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-ro", piholecluster.Name), Namespace: piholecluster.Namespace}, roSTS); err != nil {
			return false, apierrors.NewNotFound(
				schema.ParseGroupResource(roSTS.GroupVersionKind().String()),
				fmt.Sprintf("%s-ro", piholecluster.Name))
		}
		if roSTS.Status.ReadyReplicas != piholecluster.Spec.Replicas {
			return false, nil
		}
	}

	// 3️⃣ PVC
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-data", piholecluster.Name), Namespace: piholecluster.Namespace}, pvc); err != nil {
		return false, apierrors.NewNotFound(
			schema.ParseGroupResource(pvc.GroupVersionKind().String()),
			fmt.Sprintf("%s-data", piholecluster.Name))
	}
	if pvc.Status.Phase != corev1.ClaimBound {
		return false, nil
	}

	// 4️⃣ Service (DNS)
	svc := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-dns", piholecluster.Name), Namespace: piholecluster.Namespace}, svc); err != nil {
		return false, apierrors.NewNotFound(
			schema.ParseGroupResource(svc.GroupVersionKind().String()),
			fmt.Sprintf("%s-dns", piholecluster.Name))
	}

	// 5️⃣ Ingress (if enabled)
	if piholecluster.Spec.Ingress != nil && piholecluster.Spec.Ingress.Enabled {
		ing := &networkingv1.Ingress{}
		if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s", piholecluster.Name), Namespace: piholecluster.Namespace}, ing); err != nil {
			return false, apierrors.NewNotFound(
				schema.ParseGroupResource(ing.GroupVersionKind().String()),
				piholecluster.Name)
		}
	}

	// 6️⃣ PodMonitor (if enabled)
	if piholecluster.Spec.Monitoring != nil && piholecluster.Spec.Monitoring.PodMonitor != nil &&
		piholecluster.Spec.Monitoring.PodMonitor.Enabled {
		pm := &monitoringv1.PodMonitor{}
		if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s", piholecluster.Name), Namespace: piholecluster.Namespace}, pm); err != nil {
			return false, apierrors.NewNotFound(
				schema.ParseGroupResource(pm.GroupVersionKind().String()),
				piholecluster.Name)
		}
	}

	// All checks passed
	return true, nil
}

func (r *PiHoleClusterReconciler) updateStatus(ctx context.Context, piholecluster *supporterinodev1alpha1.PiHoleCluster, ready bool, errMsg string) error {
	// 1️⃣ Update fields
	piholecluster.Status.ResourcesReady = ready
	piholecluster.Status.LastError = errMsg

	// 2️⃣ Update the Ready condition
	meta.SetStatusCondition(&piholecluster.Status.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "ResourcesReady",
		Message: fmt.Sprintf("All resources are healthy. Ready=%t", ready),
	})
	if !ready {
		meta.SetStatusCondition(&piholecluster.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "ResourcesNotReady",
			Message: errMsg,
		})
	}

	// 3️⃣ Persist the status
	if err := r.Status().Update(ctx, piholecluster); err != nil {
		return fmt.Errorf("status update failed: %w", err)
	}

	return nil
}

func (r *PiHoleClusterReconciler) updateConfigSynced(ctx context.Context, cluster *supporterinodev1alpha1.PiHoleCluster, synced bool) error {
	// Fetch the latest version to avoid race conditions
	if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), cluster); err != nil {
		return fmt.Errorf("refetching CR for status update: %w", err)
	}

	cluster.Status.ConfigSynced = synced
	return r.Status().Update(ctx, cluster)
}
