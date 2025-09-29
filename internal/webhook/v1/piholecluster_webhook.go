// internal/webhook/v1/piholecluster_webhook.go

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
package v1

import (
    "context"
    "fmt"
    "regexp"

    cronparser "github.com/robfig/cron/v3"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/util/validation/field"

    ctrl "sigs.k8s.io/controller-runtime"
    logf "sigs.k8s.io/controller-runtime/pkg/log"
    "sigs.k8s.io/controller-runtime/pkg/webhook"
    "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

    supporterino "supporterino.de/pihole/api/v1"
)

// log is for logging in this package.
var piholeclusterlog = logf.Log.WithName("piholecluster-resource")

// SetupPiHoleClusterWebhookWithManager registers the webhook for PiHoleCluster in the manager.
func SetupPiHoleClusterWebhookWithManager(mgr ctrl.Manager) error {
    return ctrl.NewWebhookManagedBy(mgr).
        For(&supporterino.PiHoleCluster{}).
        WithValidator(&PiHoleClusterCustomValidator{}).
        WithDefaulter(&PiHoleClusterCustomDefaulter{}).
        Complete()
}

// +kubebuilder:webhook:path=/mutate-supporterino-de-v1-piholecluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=supporterino.de,resources=piholeclusters,verbs=create;update,versions=v1,name=mpiholecluster-v1.kb.io,admissionReviewVersions=v1

// PiHoleClusterCustomDefaulter sets default values on the custom resource of the Kind PiHoleCluster.
type PiHoleClusterCustomDefaulter struct{}

// +kubebuilder:object:generate=false
var _ webhook.CustomDefaulter = &PiHoleClusterCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind PiHoleCluster.
func (d *PiHoleClusterCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
    piholecluster, ok := obj.(*supporterino.PiHoleCluster)
    if !ok {
        return fmt.Errorf("expected a PiHoleCluster object but got %T", obj)
    }
    piholeclusterlog.Info("Defaulting for PiHoleCluster", "name", piholecluster.GetName())

    // Ensure the top‑level Spec is non‑nil – this is normally guaranteed by CRD validation
    if piholecluster.Spec.Ingress == nil {
        // Default Ingress to disabled
        piholecluster.Spec.Ingress = &supporterino.IngressSpec{Enabled: false}
    }

    if piholecluster.Spec.Monitoring == nil {
        // Default Monitoring to disabled Exporter and PodMonitor
        piholecluster.Spec.Monitoring = &supporterino.MonitoringSpec{
            Exporter:   &supporterino.ExporterSpec{Enabled: false},
            PodMonitor: &supporterino.PodMonitorSpec{Enabled: false},
        }
    } else {
        if piholecluster.Spec.Monitoring.Exporter == nil {
            piholecluster.Spec.Monitoring.Exporter = &supporterino.ExporterSpec{Enabled: false}
        }
        if piholecluster.Spec.Monitoring.PodMonitor == nil {
            piholecluster.Spec.Monitoring.PodMonitor = &supporterino.PodMonitorSpec{Enabled: false}
        }
    }

    if piholecluster.Spec.Sync == nil {
        // Default Sync to disabled cron and ad‑list sync
        piholecluster.Spec.Sync = &supporterino.SyncSpec{Cron: "", AdLists: false}
    }

    // Replicas defaults to 0 (zero value) – no action needed.

    return nil
}

// +kubebuilder:webhook:path=/validate-supporterino-de-v1-piholecluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=supporterino.de,resources=piholeclusters,verbs=create;update,versions=v1,name=vpiholecluster-v1.kb.io,admissionReviewVersions=v1

// PiHoleClusterCustomValidator validates the PiHoleCluster resource.
type PiHoleClusterCustomValidator struct{}

// +kubebuilder:object:generate=false
var _ webhook.CustomValidator = &PiHoleClusterCustomValidator{}

// ValidateCreate implements webhook.CustomValidator.
func (v *PiHoleClusterCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
    piholecluster, ok := obj.(*supporterino.PiHoleCluster)
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
    // Additional validation: Replicas must be non‑negative.
    if piholecluster.Spec.Replicas < 0 {
        gvk := metav1.SchemeGroupVersion.WithKind("PiHoleCluster").GroupKind()
        return nil, apierrors.NewInvalid(gvk, piholecluster.Name, field.ErrorList{field.Invalid(field.NewPath("spec", "replicas"), piholecluster.Spec.Replicas, "must be >= 0")})
    }
    return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator.
func (v *PiHoleClusterCustomValidator) ValidateUpdate(_ context.Context, _ runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
    piholecluster, ok := newObj.(*supporterino.PiHoleCluster)
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
    if piholecluster.Spec.Replicas < 0 {
        gvk := metav1.SchemeGroupVersion.WithKind("PiHoleCluster").GroupKind()
        return nil, apierrors.NewInvalid(gvk, piholecluster.Name, field.ErrorList{field.Invalid(field.NewPath("spec", "replicas"), piholecluster.Spec.Replicas, "must be >= 0")})
    }
    return nil, nil
}

// ValidateDelete implements webhook.CustomValidator.
func (v *PiHoleClusterCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
    piholecluster, ok := obj.(*supporterino.PiHoleCluster)
    if !ok {
        return nil, fmt.Errorf("expected a PiHoleCluster object but got %T", obj)
    }
    piholeclusterlog.Info("Validation for PiHoleCluster upon deletion", "name", piholecluster.GetName())

    // TODO: add deletion‑specific validation if required

    return nil, nil
}

/* -------------------------------------------------------------------------- */
/* Validation helpers                                                        */
/* -------------------------------------------------------------------------- */

func (v *PiHoleClusterCustomValidator) validateIngress(piholecluster *supporterino.PiHoleCluster) error {
    spec := piholecluster.Spec
    if spec.Ingress == nil || !spec.Ingress.Enabled {
        return nil // nothing to validate
    }
    if spec.Ingress.Domain == "" {
        gvk := metav1.SchemeGroupVersion.WithKind("PiHoleCluster").GroupKind()
        return apierrors.NewInvalid(gvk, piholecluster.Name, field.ErrorList{field.Invalid(field.NewPath("spec", "ingress", "domain"), spec.Ingress.Domain, "must not be empty")})
    }

    // Basic FQDN check – allows `example.com`, `sub.example.org`, etc.
    fqdnRe := regexp.MustCompile(`^([a-zA-Z0-9](-?[a-zA-Z0-9])*\.)+[a-zA-Z]{2,}$`)
    if !fqdnRe.MatchString(spec.Ingress.Domain) {
        gvk := metav1.SchemeGroupVersion.WithKind("PiHoleCluster").GroupKind()
        return apierrors.NewInvalid(gvk, piholecluster.Name, field.ErrorList{field.Invalid(field.NewPath("spec", "ingress", "domain"), spec.Ingress.Domain, "must be a valid FQDN")})
    }
    return nil
}

func (v *PiHoleClusterCustomValidator) validateCron(piholecluster *supporterino.PiHoleCluster) error {
    spec := piholecluster.Spec
    if spec.Sync == nil || spec.Sync.Cron == "" {
        return nil // CRD already enforces MinLength=1
    }
    if _, err := cronparser.ParseStandard(spec.Sync.Cron); err != nil {
        gvk := metav1.SchemeGroupVersion.WithKind("PiHoleCluster").GroupKind()
        return apierrors.NewInvalid(gvk, piholecluster.Name, field.ErrorList{field.Invalid(field.NewPath("spec", "sync", "cron"), spec.Sync.Cron, fmt.Sprintf("invalid cron expression: %v", err))})
    }
    return nil
}

func (v *PiHoleClusterCustomValidator) validateMonitoring(piholecluster *supporterino.PiHoleCluster) error {
    spec := piholecluster.Spec
    if spec.Monitoring == nil || (spec.Monitoring.PodMonitor == nil && spec.Monitoring.Exporter == nil) {
        return nil // nothing to validate
    }

    if spec.Monitoring.PodMonitor != nil && spec.Monitoring.PodMonitor.Enabled {
        if spec.Monitoring.Exporter == nil || !spec.Monitoring.Exporter.Enabled {
            gvk := metav1.SchemeGroupVersion.WithKind("PiHoleCluster").GroupKind()
            return apierrors.NewInvalid(gvk, piholecluster.Name, field.ErrorList{field.Invalid(field.NewPath("spec", "monitoring", "exporter", "enabled"), spec.Monitoring.Exporter.Enabled, "must be true when podMonitor.enabled is true")})
        }
    }
    return nil
}
