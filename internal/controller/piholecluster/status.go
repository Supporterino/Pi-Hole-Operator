package piholecluster

import (
	"context"
	"fmt"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"supporterino.de/pihole/internal/utils"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	supporterinodev1 "supporterino.de/pihole/api/v1"
)

// ---------------------------------------------------------------------------
// Resource readiness checks
// ---------------------------------------------------------------------------

func (r *Reconciler) resourcesReady(ctx context.Context, piHoleCluster *supporterinodev1.PiHoleCluster) (bool, error) {
	log := logf.FromContext(ctx)
	log.V(1).Info("Checking resource readiness", "cluster", piHoleCluster.Name)

	// 1️⃣ RW StatefulSet
	rwSTS := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-rw", piHoleCluster.Name), Namespace: piHoleCluster.Namespace}, rwSTS); err != nil {
		log.Error(err, "RW StatefulSet not found", "name", fmt.Sprintf("%s-rw", piHoleCluster.Name))
		return false, apierrors.NewNotFound(schema.ParseGroupResource(rwSTS.GroupVersionKind().String()), fmt.Sprintf("%s-rw", piHoleCluster.Name))
	}
	if rwSTS.Status.ReadyReplicas != 1 {
		log.V(1).Info("RW StatefulSet not ready", "readyReplicas", rwSTS.Status.ReadyReplicas)
		return false, nil
	}

	// 2️⃣ RO StatefulSet (if any)
	if piHoleCluster.Spec.Sync != nil && piHoleCluster.Spec.Replicas > 0 {
		roSTS := &appsv1.StatefulSet{}
		if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-ro", piHoleCluster.Name), Namespace: piHoleCluster.Namespace}, roSTS); err != nil {
			log.Error(err, "RO StatefulSet not found", "name", fmt.Sprintf("%s-ro", piHoleCluster.Name))
			return false, apierrors.NewNotFound(schema.ParseGroupResource(roSTS.GroupVersionKind().String()), fmt.Sprintf("%s-ro", piHoleCluster.Name))
		}
		if roSTS.Status.ReadyReplicas != piHoleCluster.Spec.Replicas {
			log.V(1).Info("RO StatefulSet not ready", "readyReplicas", roSTS.Status.ReadyReplicas)
			return false, nil
		}
	}

	// 3️⃣ PVC
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-data", piHoleCluster.Name), Namespace: piHoleCluster.Namespace}, pvc); err != nil {
		log.Error(err, "PVC not found", "name", fmt.Sprintf("%s-data", piHoleCluster.Name))
		return false, apierrors.NewNotFound(schema.ParseGroupResource(pvc.GroupVersionKind().String()), fmt.Sprintf("%s-data", piHoleCluster.Name))
	}
	if pvc.Status.Phase != corev1.ClaimBound {
		log.V(1).Info("PVC not bound", "phase", pvc.Status.Phase)
		return false, nil
	}

	// 4️⃣ DNS Service
	svc := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-dns", piHoleCluster.Name), Namespace: piHoleCluster.Namespace}, svc); err != nil {
		log.Error(err, "DNS service not found", "name", fmt.Sprintf("%s-dns", piHoleCluster.Name))
		return false, apierrors.NewNotFound(schema.ParseGroupResource(svc.GroupVersionKind().String()), fmt.Sprintf("%s-dns", piHoleCluster.Name))
	}

	// 5️⃣ Ingress (if enabled)
	if piHoleCluster.Spec.Ingress != nil && piHoleCluster.Spec.Ingress.Enabled {
		ing := &networkingv1.Ingress{}
		if err := r.Get(ctx, types.NamespacedName{Name: piHoleCluster.Name, Namespace: piHoleCluster.Namespace}, ing); err != nil {
			log.Error(err, "Ingress not found", "name", piHoleCluster.Name)
			return false, apierrors.NewNotFound(schema.ParseGroupResource(ing.GroupVersionKind().String()), piHoleCluster.Name)
		}
	}

	// 6️⃣ PodMonitor (if enabled)
	if piHoleCluster.Spec.Monitoring != nil &&
		piHoleCluster.Spec.Monitoring.PodMonitor != nil &&
		piHoleCluster.Spec.Monitoring.PodMonitor.Enabled {
		pm := &monitoringv1.PodMonitor{}
		if err := r.Get(ctx, types.NamespacedName{Name: piHoleCluster.Name, Namespace: piHoleCluster.Namespace}, pm); err != nil {
			log.Error(err, "PodMonitor not found", "name", piHoleCluster.Name)
			return false, apierrors.NewNotFound(schema.ParseGroupResource(pm.GroupVersionKind().String()), piHoleCluster.Name)
		}
	}

	log.V(1).Info("All resources are ready")
	return true, nil
}

// ---------------------------------------------------------------------------
// Status helpers
// ---------------------------------------------------------------------------

func (r *Reconciler) updateStatus(ctx context.Context, piHoleCluster *supporterinodev1.PiHoleCluster, ready bool, errMsg string) error {
	log := logf.FromContext(ctx)
	log.Info("Updating status", "cluster", piHoleCluster.Name, "ready", ready)

	// 1️⃣ Update fields
	piHoleCluster.Status.ResourcesReady = ready
	piHoleCluster.Status.LastError = errMsg

	// 2️⃣ Update the Ready condition
	meta.SetStatusCondition(&piHoleCluster.Status.Conditions, metav1.Condition{
		Type:    typePiHoleClusterReady,
		Status:  metav1.ConditionTrue,
		Reason:  "ResourcesReady",
		Message: fmt.Sprintf("All resources are healthy. Ready=%t", ready),
	})
	if !ready {
		meta.SetStatusCondition(&piHoleCluster.Status.Conditions, metav1.Condition{
			Type:    typePiHoleClusterNotReady,
			Status:  metav1.ConditionFalse,
			Reason:  "MissingResources",
			Message: errMsg,
		})
	}

	// 3️⃣ Persist the status
	if err := utils.UpdateClusterStatusWithRetry(ctx, r.Client, piHoleCluster, log); err != nil {
		return fmt.Errorf("status update failed: %w", err)
	}
	log.V(1).Info("Status updated successfully")
	return nil
}
