package utils

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kubeutilretry "k8s.io/client-go/util/retry"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
)

type Customizer func(existing, desired ctrlclient.Object) (ctrlclient.Object, error)

// UpsertResource reconciles an arbitrary Kubernetes object with a desired state.
// It sets the controller reference, performs a Get/Create/Update cycle,
// and retries on conflict using client-go's retry.RetryOnConflict.
// The function logs create/update actions and returns the *desired* object on success.
func UpsertResource(ctx context.Context, cl ctrlclient.Client, owner ctrlclient.Object, desired ctrlclient.Object, name string, log logr.Logger, customizer Customizer) (ctrlclient.Object, error) {
	if err := ctrl.SetControllerReference(owner, desired, cl.Scheme()); err != nil {
		return nil, fmt.Errorf("set owner reference: %w", err)
	}

	existing := desired.DeepCopyObject().(ctrlclient.Object)
	if err := cl.Get(ctx, ctrlclient.ObjectKey{Namespace: owner.GetNamespace(), Name: name}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating object", "kind", desired.GetObjectKind().GroupVersionKind(), "name", name)
			if err := cl.Create(ctx, desired); err != nil {
				return nil, fmt.Errorf("create: %w", err)
			}
			return desired, nil
		}
		return nil, fmt.Errorf("get: %w", err)
	}

	if customizer != nil {
		return customizer(existing, desired)
	}

	if !reflect.DeepEqual(existing, desired) {
		log.Info("Updating object", "kind", desired.GetObjectKind().GroupVersionKind(), "name", name)

		if err := kubeutilretry.RetryOnConflict(kubeutilretry.DefaultBackoff, func() error {
			if err := cl.Get(ctx, ctrlclient.ObjectKey{Namespace: owner.GetNamespace(), Name: name}, existing); err != nil {
				return fmt.Errorf("refresh before update: %w", err)
			}

			if err := ctrl.SetControllerReference(owner, desired, cl.Scheme()); err != nil {
				return fmt.Errorf("set owner: %w", err)
			}

			return cl.Update(ctx, desired)
		}); err != nil {
			return nil, fmt.Errorf("update: %w", err)
		}
	}

	return existing, nil
}

// UpdateClusterStatusWithRetry updates the Status subresource of a cluster‑like object
// and retries on conflict using controller-runtime's retry helper.
//
//   - ctx:      the context from the reconcile loop
//   - c:        a controller-runtime client (must support status updates)
//   - obj:      the object whose Status should be updated (must implement client.Object)
//   - logger:   a logr.Logger to emit retry / failure logs
//
// The function returns the error returned by the last attempt (or nil if successful).
func UpdateClusterStatusWithRetry(ctx context.Context, c ctrlclient.Client, obj ctrlclient.Object, logger logr.Logger) error {
	return kubeutilretry.RetryOnConflict(kubeutilretry.DefaultBackoff, func() error {
		if err := c.Status().Update(ctx, obj); err != nil {
			logger.V(1).Info(fmt.Sprintf("failed to update status: %v", err))
			return err
		}
		logger.V(1).Info("status updated successfully")
		return nil
	})
}
