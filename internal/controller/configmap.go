package controller

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

// ensurePiHoleEnvCM creates a ConfigMap that contains the user‑supplied env vars.
func (r *PiHoleClusterReconciler) ensurePiHoleEnvCM(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (*corev1.ConfigMap, error) {
	if piHoleCluster.Spec.Config == nil || len(piHoleCluster.Spec.Config.EnvVars) == 0 {
		return nil, nil // nothing to do
	}

	cmName := fmt.Sprintf("%s-env", piHoleCluster.Name)

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: piHoleCluster.Namespace,
		},
		Data: piHoleCluster.Spec.Config.EnvVars, // key/value pairs
	}

	if err := ctrl.SetControllerReference(piHoleCluster, desired, r.Scheme); err != nil {
		return nil, err
	}

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: piHoleCluster.Namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return nil, err
		}
		return desired, nil
	}
	if err != nil {
		return nil, err
	}

	// Update only the data field
	if !reflect.DeepEqual(existing.Data, desired.Data) {
		existing.Data = desired.Data
		if err := r.Update(ctx, existing); err != nil {
			return nil, err
		}
	}
	return existing, nil
}
