package controller

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

func (r *PiHoleClusterReconciler) ensureReadWriteSTS(ctx context.Context, piholecluster *supporterinodev1alpha1.PiHoleCluster) error {
	stsName := fmt.Sprintf("%s-rw", piholecluster.Name)

	// Desired StatefulSet
	desired := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stsName,
			Namespace: piholecluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "pihole",
				"app.kubernetes.io/instance":  piholecluster.Name,
				"supporterino.de/pihole-role": "readwrite",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "pihole",
					"app.kubernetes.io/instance":  piholecluster.Name,
					"supporterino.de/pihole-role": "readwrite",
				},
			},
			ServiceName: stsName, // headless service for the STS
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":      "pihole",
						"app.kubernetes.io/instance":  piholecluster.Name,
						"supporterino.de/pihole-role": "readwrite",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "pihole",
							Image: "pihole/pihole:latest", // change to your preferred tag
							Ports: []corev1.ContainerPort{
								{ContainerPort: 80, Name: "http"},
								{ContainerPort: 53, Protocol: corev1.ProtocolUDP, Name: "dns"},
							},
							Env: []corev1.EnvVar{
								{Name: "TZ", Value: "UTC"}, // Add any other PiHole env vars you need
							},
						},
					},
				},
			},
		},
	}

	// add exporter if enabled
	addExporterIfEnabled(piholecluster, &desired.Spec.Template.Spec)

	// add api password env
	secret, err := r.ensureAPISecret(ctx, piholecluster)
	if err != nil {
		return err
	}
	if secret != nil {
		// Determine the key to use in the Secret.
		var key string
		if piholecluster.Spec.Config != nil && piholecluster.Spec.Config.APIPassword.SecretRef != nil {
			key = piholecluster.Spec.Config.APIPassword.SecretRef.Key
		} else {
			key = "password" // our locally created secret always uses this key
		}

		env := corev1.EnvVar{
			Name: "FTLCONF_webserver_api_password",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name},
					Key:                  key,
				},
			},
		}
		desired.Spec.Template.Spec.Containers[0].Env = append(desired.Spec.Template.Spec.Containers[0].Env, env)
	}

	// 1️⃣ Create / get the PVC
	pvc, err := r.ensurePiHolePVC(ctx, piholecluster)
	if err != nil {
		return err
	}

	// 2️⃣ Build the pod template with volumeMounts and volume
	volumeName := "pihole-data"
	desired.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{
			Name:      volumeName,
			MountPath: "/etc/pihole/db",
			SubPath:   "databases",
		},
		{
			Name:      volumeName,
			MountPath: "/etc/pihole",
			SubPath:   "config",
		},
		{
			Name:      volumeName,
			MountPath: "/etc/dnsmasq.d",
			SubPath:   "dnsmasq",
		},
	}

	// 3️⃣ Attach the PVC to the pod
	desired.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvc.Name,
				},
			},
		},
	}

	// 1️⃣ Create / get the ConfigMap
	envCM, err := r.ensurePiHoleEnvCM(ctx, piholecluster)
	if err != nil {
		return err
	}

	// 2️⃣ Attach it to the pod template
	if envCM != nil {
		desired.Spec.Template.Spec.Containers[0].EnvFrom = append(desired.Spec.Template.Spec.Containers[0].EnvFrom,
			corev1.EnvFromSource{
				ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: envCM.Name}},
			})
	}

	// Resources
	if !reflect.DeepEqual(piholecluster.Spec.Resources, corev1.ResourceRequirements{}) {
		desired.Spec.Template.Spec.Containers[0].Resources = piholecluster.Spec.Resources
	}

	// PodSecurityContext – applied to the *entire* pod
	if piholecluster.Spec.Security != nil && piholecluster.Spec.Security.PodSecurityContext != nil {
		desired.Spec.Template.Spec.SecurityContext = piholecluster.Spec.Security.PodSecurityContext
	}

	// ContainerSecurityContext – applied to the PiHole container only
	if piholecluster.Spec.Security != nil && piholecluster.Spec.Security.ContainerSecurityContext != nil {
		desired.Spec.Template.Spec.Containers[0].SecurityContext = piholecluster.Spec.Security.ContainerSecurityContext
	}

	// Ensure the StatefulSet is owned by the PiHoleCluster (controller-runtime will set OwnerReference)
	if err := ctrl.SetControllerReference(piholecluster, desired, r.Scheme); err != nil {
		return err
	}

	// Reconcile: create or update the StatefulSet
	existing := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: stsName, Namespace: piholecluster.Namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		// create
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Update only the fields that might change (replicas, image etc.)
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		return r.Update(ctx, existing)
	}
	return nil
}
