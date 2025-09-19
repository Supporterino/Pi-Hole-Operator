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
