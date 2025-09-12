/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"
	"fmt"

	cronparser "github.com/robfig/cron/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var piholeclusterlog = logf.Log.WithName("piholecluster-resource")

// SetupPiHoleClusterWebhookWithManager registers the webhook for PiHoleCluster in the manager.
func SetupPiHoleClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&supporterinodev1alpha1.PiHoleCluster{}).
		WithValidator(&PiHoleClusterCustomValidator{}).
		WithDefaulter(&PiHoleClusterCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-supporterino-de-v1alpha1-piholecluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=supporterino.de,resources=piholeclusters,verbs=create;update,versions=v1alpha1,name=mpiholecluster-v1alpha1.kb.io,admissionReviewVersions=v1

// PiHoleClusterCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind PiHoleCluster when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type PiHoleClusterCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &PiHoleClusterCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind PiHoleCluster.
func (d *PiHoleClusterCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	piholecluster, ok := obj.(*supporterinodev1alpha1.PiHoleCluster)

	if !ok {
		return fmt.Errorf("expected an PiHoleCluster object but got %T", obj)
	}
	piholeclusterlog.Info("Defaulting for PiHoleCluster", "name", piholecluster.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-supporterino-de-v1alpha1-piholecluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=supporterino.de,resources=piholeclusters,verbs=create;update,versions=v1alpha1,name=vpiholecluster-v1alpha1.kb.io,admissionReviewVersions=v1

// PiHoleClusterCustomValidator struct is responsible for validating the PiHoleCluster resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type PiHoleClusterCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &PiHoleClusterCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type PiHoleCluster.
func (v *PiHoleClusterCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	piholecluster, ok := obj.(*supporterinodev1alpha1.PiHoleCluster)
	if !ok {
		return nil, fmt.Errorf("expected a PiHoleCluster object but got %T", obj)
	}
	piholeclusterlog.Info("Validation for PiHoleCluster upon creation", "name", piholecluster.GetName())

	if err := v.validateIngress(piholecluster); err != nil {
		return nil, err
	}
	if err := v.validateCron(piholecluster); err != nil {
		return nil, err
	}
	if err := v.validateMonitoring(piholecluster); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type PiHoleCluster.
func (v *PiHoleClusterCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	piholecluster, ok := newObj.(*supporterinodev1alpha1.PiHoleCluster)
	if !ok {
		return nil, fmt.Errorf("expected a PiHoleCluster object for the newObj but got %T", newObj)
	}
	piholeclusterlog.Info("Validation for PiHoleCluster upon update", "name", piholecluster.GetName())

	if err := v.validateIngress(piholecluster); err != nil {
		return nil, err
	}
	if err := v.validateCron(piholecluster); err != nil {
		return nil, err
	}
	if err := v.validateMonitoring(piholecluster); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type PiHoleCluster.
func (v *PiHoleClusterCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	piholecluster, ok := obj.(*supporterinodev1alpha1.PiHoleCluster)
	if !ok {
		return nil, fmt.Errorf("expected a PiHoleCluster object but got %T", obj)
	}
	piholeclusterlog.Info("Validation for PiHoleCluster upon deletion", "name", piholecluster.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

// validateIngress checks the ingress rule
func (v *PiHoleClusterCustomValidator) validateIngress(piholecluster *supporterinodev1alpha1.PiHoleCluster) error {
	spec := piholecluster.Spec
	allErrs := field.ErrorList{}

	if spec.Ingress == nil {
		return nil
	}

	if spec.Ingress.Enabled && spec.Ingress.Domain == "" {
		return apierrors.NewInvalid(
			metav1.SchemeGroupVersion.WithKind("PiHoleCluster").GroupKind(),
			piholecluster.Name,
			append(allErrs, field.Invalid(field.NewPath("spec", "ingress", "domain"), spec.Ingress.Domain, "must not be empty")),
		)
	}
	return nil
}

// validateCron ensures the cron field is syntactically correct.
func (v *PiHoleClusterCustomValidator) validateCron(piholecluster *supporterinodev1alpha1.PiHoleCluster) error {
	spec := piholecluster.Spec
	allErrs := field.ErrorList{}

	// No sync block → nothing to validate
	if spec.Sync == nil {
		return nil
	}

	// Empty cron is already rejected by the CRD (MinLength=1),
	// but we still guard against it.
	cronExpr := spec.Sync.Cron
	if cronExpr == "" {
		return nil // let the CRD validator handle it
	}

	// Attempt to parse the cron expression.
	if _, err := cronparser.ParseStandard(cronExpr); err != nil {
		return apierrors.NewInvalid(
			metav1.SchemeGroupVersion.WithKind("PiHoleCluster").GroupKind(),
			piholecluster.Name,
			append(allErrs, field.Invalid(field.NewPath("spec", "sync", "cron"), cronExpr, "Invalid cron expression")),
		)
	}

	return nil
}

func (v *PiHoleClusterCustomValidator) validateMonitoring(piholecluster *supporterinodev1alpha1.PiHoleCluster) error {
	spec := piholecluster.Spec
	allErrs := field.ErrorList{}

	// nothing to validate if monitoring block is missing
	if spec.Monitoring == nil {
		return nil
	}

	// Guard against missing sub‑objects
	if spec.Monitoring.PodMonitor == nil || spec.Monitoring.Exporter == nil {
		return nil // other validations (required/ defaults) will handle missing fields
	}

	if spec.Monitoring.PodMonitor.Enabled && !spec.Monitoring.Exporter.Enabled {
		return apierrors.NewInvalid(
			metav1.SchemeGroupVersion.WithKind("PiHoleCluster").GroupKind(),
			piholecluster.Name,
			append(allErrs, field.Invalid(field.NewPath("spec", "monitoring", "exporter", "enabled"), spec.Monitoring.Exporter.Enabled, "must be true when podMonitor.enabled is true")),
		)
	}

	return nil
}
