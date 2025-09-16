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
	"sigs.k8s.io/controller-runtime/pkg/client"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

// ensureAPISecret creates a secret only when a plain password is supplied.
// If SecretRef is used, the function simply returns that secret.
func (r *PiHoleClusterReconciler) ensureAPISecret(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (*corev1.Secret, error) {
	if piHoleCluster.Spec.Config == nil ||
		(piHoleCluster.Spec.Config.APIPassword.Password == "" && piHoleCluster.Spec.Config.APIPassword.SecretRef == nil) {
		return nil, nil // nothing to do
	}

	// ---------- 1️⃣ Handle SecretRef ----------
	if piHoleCluster.Spec.Config.APIPassword.SecretRef != nil {
		src := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: piHoleCluster.Namespace,
			Name:      piHoleCluster.Spec.Config.APIPassword.SecretRef.Name,
		}, src); err != nil {
			return nil, fmt.Errorf("failed to read referenced secret: %w", err)
		}

		// Verify the key exists
		if _, ok := src.Data[piHoleCluster.Spec.Config.APIPassword.SecretRef.Key]; !ok {
			return nil, fmt.Errorf("key %q not found in secret %s/%s",
				piHoleCluster.Spec.Config.APIPassword.SecretRef.Key,
				src.Namespace, src.Name)
		}
		return src, nil // just pass it through
	}

	// ---------- 2️⃣ Create / Update local secret ----------
	secretName := fmt.Sprintf("%s-api-password", piHoleCluster.Name)
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: piHoleCluster.Namespace,
		},
		Data: map[string][]byte{
			"password": []byte(piHoleCluster.Spec.Config.APIPassword.Password),
		},
	}

	// OwnerRef – so the Secret is garbage‑collected with the cluster
	if err := ctrl.SetControllerReference(piHoleCluster, desired, r.Scheme); err != nil {
		return nil, err
	}

	// Reconcile: create or update the Secret
	existing := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: piHoleCluster.Namespace}, existing)
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

// getAPISecret reads the Kubernetes Secret that holds the Pi‑hole password.
// The secret name is derived from the Cluster CR (`{name}-api-secret`).
// It returns the *raw* data map – callers can pick out whatever keys they need.
func (r *PiHoleClusterReconciler) getAPISecret(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (*corev1.Secret, error) {
	if piHoleCluster.Spec.Config.APIPassword.SecretRef != nil {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: piHoleCluster.Namespace,
			Name:      piHoleCluster.Spec.Config.APIPassword.SecretRef.Name,
		}, secret); err != nil {
			return nil, fmt.Errorf("failed to read referenced secret: %w", err)
		}

		// Verify the key exists
		if _, ok := secret.Data[piHoleCluster.Spec.Config.APIPassword.SecretRef.Key]; !ok {
			return nil, fmt.Errorf("key %q not found in secret %s/%s",
				piHoleCluster.Spec.Config.APIPassword.SecretRef.Key,
				secret.Namespace, secret.Name)
		}
		return secret, nil // just pass it through
	}

	secretName := fmt.Sprintf("%s-api-password", piHoleCluster.Name)

	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: piHoleCluster.Namespace, Name: secretName}, secret)
	if err != nil {
		return nil, fmt.Errorf("reading API secret %s/%s: %w", piHoleCluster.Namespace, secretName, err)
	}
	return secret, nil
}
