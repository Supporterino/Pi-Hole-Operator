package controller

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

func (r *PiHoleClusterReconciler) ensurePiHolePVC(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (*corev1.PersistentVolumeClaim, error) {
	log := logf.FromContext(ctx)

	pvcName := fmt.Sprintf("%s-data", piHoleCluster.Name)

	// Default values
	storageSize := "10Gi"
	accessModes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	var storageClassName *string

	if piHoleCluster.Spec.Persistence != nil {
		if piHoleCluster.Spec.Persistence.Size != "" {
			storageSize = piHoleCluster.Spec.Persistence.Size
		}
		if len(piHoleCluster.Spec.Persistence.AccessModes) > 0 {
			accessModes = piHoleCluster.Spec.Persistence.AccessModes
		}
		if piHoleCluster.Spec.Persistence.StorageClassName != nil {
			storageClassName = piHoleCluster.Spec.Persistence.StorageClassName
		}
	}

	desired := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: piHoleCluster.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}

	if storageClassName != nil {
		desired.Spec.StorageClassName = storageClassName
	}

	// OwnerRef – so the PVC is garbage‑collected with the cluster
	if err := ctrl.SetControllerReference(piHoleCluster, desired, r.Scheme); err != nil {
		return nil, err
	}

	// Reconcile: create or update the PVC
	existing := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: piHoleCluster.Namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			log.Error(err, "Failed to create PVC")
			return nil, err
		}
		log.V(0).Info("PVC created", "name", pvcName)
		return desired, nil
	}
	if err != nil {
		log.V(1).Info("Error while fetching PVC", "err:", err, "pvcName:", pvcName)
		return nil, err
	}

	// Update only the spec that can change (size, access modes, class)
	// Update only mutable field: Resources.Requests
	if !reflect.DeepEqual(existing.Spec.Resources.Requests, desired.Spec.Resources.Requests) {
		existing.Spec.Resources.Requests = desired.Spec.Resources.Requests
		if err := r.Update(ctx, existing); err != nil {
			log.Error(err, "Failed to update PVC")
			return nil, err
		}
	}
	return existing, nil
}
