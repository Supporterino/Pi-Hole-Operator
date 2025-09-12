package controller

import (
	"context"
	"reflect"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

func (r *PiHoleClusterReconciler) ensureIngress(ctx context.Context, piholecluster *supporterinodev1alpha1.PiHoleCluster) error {
	// No ingress requested → nothing to do
	if piholecluster.Spec.Ingress == nil || !piholecluster.Spec.Ingress.Enabled {
		return nil
	}

	// Desired Ingress name – same as the cluster for simplicity
	ingName := piholecluster.Name

	// Build the Ingress spec
	desired := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingName,
			Namespace: piholecluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":     "pihole",
				"app.kubernetes.io/instance": piholecluster.Name,
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: piholecluster.Spec.Ingress.Domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: ptrTo(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: piholecluster.Name + "-rw", // headless service created by the STS
											Port: networkingv1.ServiceBackendPort{
												Number: 80, // PiHole HTTP port
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Owner reference so the Ingress gets garbage‑collected
	if err := ctrl.SetControllerReference(piholecluster, desired, r.Scheme); err != nil {
		return err
	}

	// Reconcile: create or update the Ingress
	existing := &networkingv1.Ingress{}
	err := r.Get(ctx, types.NamespacedName{Name: ingName, Namespace: piholecluster.Namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Update only the spec that can change (domain, backend)
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		return r.Update(ctx, existing)
	}
	return nil
}
