package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

type Resource interface {
	client.Object
}

func (r *PiHoleClusterReconciler) create(ctx context.Context, existing, desired Resource, resourceName, namespace string, piholecluster *supporterinodev1alpha1.PiHoleCluster) error {

	if err := ctrl.SetControllerReference(piholecluster, desired, r.Scheme); err != nil {
		return err
	}

	err := r.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}

	if err != nil {
		return err
	}

	return nil
}

func (r *PiHoleClusterReconciler) update() {

}
