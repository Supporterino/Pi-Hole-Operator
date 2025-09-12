package controller

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

// ensureDNSService creates/updates a Service that selects both RW and RO pods.
func (r *PiHoleClusterReconciler) ensureDNSService(ctx context.Context, piholecluster *supporterinodev1alpha1.PiHoleCluster) error {
	svcName := fmt.Sprintf("%s-dns", piholecluster.Name)

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: piholecluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":     "pihole",
				"app.kubernetes.io/instance": piholecluster.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: piholecluster.Spec.ServiceType, // ClusterIP or LoadBalancer
			Ports: []corev1.ServicePort{
				{
					Name:       "dns",
					Protocol:   corev1.ProtocolUDP,
					Port:       53,                 // service port
					TargetPort: intstr.FromInt(53), // container port
				},
			},
			Selector: map[string]string{
				"app.kubernetes.io/name":     "pihole",
				"app.kubernetes.io/instance": piholecluster.Name,
			},
		},
	}

	// Owner reference
	if err := ctrl.SetControllerReference(piholecluster, desired, r.Scheme); err != nil {
		return err
	}

	// Reconcile: create or update the Service
	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: piholecluster.Namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Update only the spec that can change (type, ports)
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		return r.Update(ctx, existing)
	}
	return nil
}
