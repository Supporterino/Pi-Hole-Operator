package piholecluster

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/client"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
	"supporterino.de/pihole/internal/utils"
)

// --------------------
// RW StatefulSet
// --------------------
func (r *Reconciler) ensureReadWriteSTS(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	log := logf.FromContext(ctx)
	stsName := fmt.Sprintf("%s-rw", piHoleCluster.Name)

	desired := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stsName,
			Namespace: piHoleCluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "pihole",
				"app.kubernetes.io/instance":  piHoleCluster.Name,
				"supporterino.de/pihole-role": "readwrite",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: utils.Int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "pihole",
					"app.kubernetes.io/instance":  piHoleCluster.Name,
					"supporterino.de/pihole-role": "readwrite",
				},
			},
			ServiceName: stsName,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":      "pihole",
						"app.kubernetes.io/instance":  piHoleCluster.Name,
						"supporterino.de/pihole-role": "readwrite",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "pihole",
							Image: "pihole/pihole:latest",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 80, Name: "http"},
								{ContainerPort: 53, Protocol: corev1.ProtocolUDP, Name: "dns"},
							},
							Env: []corev1.EnvVar{
								{Name: "TZ", Value: "UTC"},
							},
						},
					},
				},
			},
		},
	}

	addExporterIfEnabled(piHoleCluster, &desired.Spec.Template.Spec)

	// API password env
	secret, err := r.ensureAPISecret(ctx, piHoleCluster)
	if err != nil {
		return err
	}
	if secret != nil {
		var key string
		if piHoleCluster.Spec.Config != nil && piHoleCluster.Spec.Config.APIPassword.SecretRef != nil {
			key = piHoleCluster.Spec.Config.APIPassword.SecretRef.Key
		} else {
			key = "password"
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

	pvc, err := r.ensurePiHolePVC(ctx, piHoleCluster)
	if err != nil {
		return err
	}

	volumeName := "pihole-data"
	desired.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{Name: volumeName, MountPath: "/etc/pihole/db", SubPath: "databases"},
		{Name: volumeName, MountPath: "/etc/pihole", SubPath: "config"},
		{Name: volumeName, MountPath: "/etc/dnsmasq.d", SubPath: "dnsmasq"},
	}
	desired.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc.Name},
			},
		},
	}

	envCM, err := r.ensurePiHoleEnvCM(ctx, piHoleCluster)
	if err != nil {
		return err
	}
	if envCM != nil {
		desired.Spec.Template.Spec.Containers[0].EnvFrom = append(desired.Spec.Template.Spec.Containers[0].EnvFrom, corev1.EnvFromSource{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: envCM.Name}}})
	}

	if !reflect.DeepEqual(piHoleCluster.Spec.Resources, corev1.ResourceRequirements{}) {
		desired.Spec.Template.Spec.Containers[0].Resources = piHoleCluster.Spec.Resources
	}

	if piHoleCluster.Spec.Security != nil && piHoleCluster.Spec.Security.PodSecurityContext != nil {
		desired.Spec.Template.Spec.SecurityContext = piHoleCluster.Spec.Security.PodSecurityContext
	}

	if piHoleCluster.Spec.Security != nil && piHoleCluster.Spec.Security.ContainerSecurityContext != nil {
		desired.Spec.Template.Spec.Containers[0].SecurityContext = piHoleCluster.Spec.Security.ContainerSecurityContext
	}

	if _, err := utils.UpsertResource(ctx, r.Client, piHoleCluster, desired, stsName, log, nil); err != nil {
		return err
	}
	return nil
}

// areRWPodsReady reports whether at least one RW pod is Ready.
func (r *Reconciler) areRWPodsReady(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (bool, error) {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(piHoleCluster.Namespace), client.MatchingLabels{"app.kubernetes.io/name": "pihole",
		"app.kubernetes.io/instance":  piHoleCluster.Name,
		"supporterino.de/pihole-role": "readwrite"}); err != nil {
		return false, fmt.Errorf("list RW pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return false, nil
	}
	for _, p := range podList.Items {
		if utils.IsPodReady(&p) {
			return true, nil
		}
	}
	return false, nil
}

// --------------------
// RO StatefulSet
// --------------------
func (r *Reconciler) ensureReadOnlySTS(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	if piHoleCluster.Spec.Replicas <= 0 {
		return nil
	}
	log := logf.FromContext(ctx)
	stsName := fmt.Sprintf("%s-ro", piHoleCluster.Name)

	desired := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stsName,
			Namespace: piHoleCluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "pihole",
				"app.kubernetes.io/instance":  piHoleCluster.Name,
				"supporterino.de/pihole-role": "readonly",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: utils.Int32Ptr(piHoleCluster.Spec.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "pihole",
					"app.kubernetes.io/instance":  piHoleCluster.Name,
					"supporterino.de/pihole-role": "readonly",
				},
			},
			ServiceName: stsName,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":      "pihole",
						"app.kubernetes.io/instance":  piHoleCluster.Name,
						"supporterino.de/pihole-role": "readonly",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "pihole",
							Image: "pihole/pihole:latest",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 80, Name: "http"},
								{ContainerPort: 53, Protocol: corev1.ProtocolUDP, Name: "dns"},
							},
							Env: []corev1.EnvVar{
								{Name: "TZ", Value: "UTC"},
							},
						},
					},
				},
			},
		},
	}

	addExporterIfEnabled(piHoleCluster, &desired.Spec.Template.Spec)

	secret, err := r.ensureAPISecret(ctx, piHoleCluster)
	if err != nil {
		return err
	}
	if secret != nil {
		var key string
		if piHoleCluster.Spec.Config != nil && piHoleCluster.Spec.Config.APIPassword.SecretRef != nil {
			key = piHoleCluster.Spec.Config.APIPassword.SecretRef.Key
		} else {
			key = "password"
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

	envCM, err := r.ensurePiHoleEnvCM(ctx, piHoleCluster)
	if err != nil {
		return err
	}
	if envCM != nil {
		desired.Spec.Template.Spec.Containers[0].EnvFrom = append(desired.Spec.Template.Spec.Containers[0].EnvFrom, corev1.EnvFromSource{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: envCM.Name}}})
	}

	volumeName := "pihole-data"
	desired.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{Name: volumeName, MountPath: "/etc/pihole/db", SubPath: "databases"},
		{Name: volumeName, MountPath: "/etc/pihole", SubPath: "config"},
		{Name: volumeName, MountPath: "/etc/dnsmasq.d", SubPath: "dnsmasq"},
	}
	desired.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	if !reflect.DeepEqual(piHoleCluster.Spec.Resources, corev1.ResourceRequirements{}) {
		desired.Spec.Template.Spec.Containers[0].Resources = piHoleCluster.Spec.Resources
	}

	if piHoleCluster.Spec.Security != nil && piHoleCluster.Spec.Security.PodSecurityContext != nil {
		desired.Spec.Template.Spec.SecurityContext = piHoleCluster.Spec.Security.PodSecurityContext
	}

	if piHoleCluster.Spec.Security != nil && piHoleCluster.Spec.Security.ContainerSecurityContext != nil {
		desired.Spec.Template.Spec.Containers[0].SecurityContext = piHoleCluster.Spec.Security.ContainerSecurityContext
	}

	if _, err := utils.UpsertResource(ctx, r.Client, piHoleCluster, desired, stsName, log, nil); err != nil {
		return err
	}
	return nil
}

// areROPodsReady reports whether the read‑only StatefulSet has at least one pod that is fully ready.
// The original logic only inspected the pod list and returned true as soon as a pod had a Ready condition,
// which caused the operator to think the cluster was ready immediately after the StatefulSet
// was created.  The new logic checks that the StatefulSet reports ready replicas and
// ensures that all desired replicas are in a Ready state.
func (r *Reconciler) areROPodsReady(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (bool, error) {
	// 1️⃣ Get the read‑only StatefulSet.
	stsName := fmt.Sprintf("%s-ro", piHoleCluster.Name)
	ss := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: piHoleCluster.Namespace, Name: stsName}, ss); err != nil {
		if apierrors.IsNotFound(err) { // StatefulSet not yet created
			return false, nil
		}
		return false, fmt.Errorf("unable to get RO statefulset %s: %w", stsName, err)
	}

	// 2️⃣ Wait until the StatefulSet reports at least one ready replica.
	if ss.Status.ReadyReplicas == 0 {
		return false, nil
	}

	// 3️⃣ Verify that every replica pod is actually ready.
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(piHoleCluster.Namespace), client.MatchingLabels{
		"app.kubernetes.io/name":      "pihole",
		"app.kubernetes.io/instance":  piHoleCluster.Name,
		"supporterino.de/pihole-role": "readonly",
	}); err != nil {
		return false, fmt.Errorf("list RO pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return false, nil
	}

	readyCount := 0
	for _, p := range podList.Items {
		if utils.IsPodReady(&p) {
			readyCount++
		}
	}

	// 4️⃣ All desired replicas must be ready.
	return readyCount == int(*ss.Spec.Replicas), nil
}

// --------------------
// API Secret
// --------------------
func (r *Reconciler) ensureAPISecret(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (*corev1.Secret, error) {
	if piHoleCluster.Spec.Config == nil ||
		(piHoleCluster.Spec.Config.APIPassword.Password == "" && piHoleCluster.Spec.Config.APIPassword.SecretRef == nil) {
		return nil, nil
	}

	if piHoleCluster.Spec.Config.APIPassword.SecretRef != nil {
		src := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: piHoleCluster.Namespace,
			Name:      piHoleCluster.Spec.Config.APIPassword.SecretRef.Name,
		}, src); err != nil {
			return nil, fmt.Errorf("failed to read referenced secret: %w", err)
		}
		if _, ok := src.Data[piHoleCluster.Spec.Config.APIPassword.SecretRef.Key]; !ok {
			return nil, fmt.Errorf("key %q not found in secret %s/%s", piHoleCluster.Spec.Config.APIPassword.SecretRef.Key, src.Namespace, src.Name)
		}
		return src, nil
	}

	log := logf.FromContext(ctx)
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

	secret, err := utils.UpsertResource(ctx, r.Client, piHoleCluster, desired, secretName, log, nil)
	if err != nil {
		return nil, err
	}

	return secret.(*corev1.Secret), nil
}

func (r *Reconciler) getAPISecret(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (*corev1.Secret, error) {
	if piHoleCluster.Spec.Config != nil && piHoleCluster.Spec.Config.APIPassword.SecretRef != nil {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: piHoleCluster.Namespace,
			Name:      piHoleCluster.Spec.Config.APIPassword.SecretRef.Name,
		}, secret); err != nil {
			return nil, fmt.Errorf("failed to read referenced secret: %w", err)
		}
		if _, ok := secret.Data[piHoleCluster.Spec.Config.APIPassword.SecretRef.Key]; !ok {
			return nil, fmt.Errorf("key %q not found in secret %s/%s", piHoleCluster.Spec.Config.APIPassword.SecretRef.Key, secret.Namespace, secret.Name)
		}
		return secret, nil
	}

	secretName := fmt.Sprintf("%s-api-password", piHoleCluster.Name)
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: piHoleCluster.Namespace, Name: secretName}, secret); err != nil {
		return nil, fmt.Errorf("reading API secret %s/%s: %w", piHoleCluster.Namespace, secretName, err)
	}
	return secret, nil
}

// --------------------
// PersistentVolumeClaim for PiHole data
// --------------------
func (r *Reconciler) ensurePiHolePVC(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (*corev1.PersistentVolumeClaim, error) {
	log := logf.FromContext(ctx)

	pvcName := fmt.Sprintf("%s-data", piHoleCluster.Name)
	storageSize := "10Gi"
	accessModes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	var storageClassName *string

	if piHoleCluster.Spec.Persistence != nil {
		if piHoleCluster.Spec.Persistence.Size != "" {
			storageSize = piHoleCluster.Spec.Persistence.Size
		}
		if len(piHoleCluster.Spec.Persistence.AccessModes) > 0 {
			accessModes = piHoleCluster.Spec.Persistence.AccessModes
		}
		if piHoleCluster.Spec.Persistence.StorageClassName != nil {
			storageClassName = piHoleCluster.Spec.Persistence.StorageClassName
		}
	}

	desired := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: piHoleCluster.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}
	if storageClassName != nil {
		desired.Spec.StorageClassName = storageClassName
	}

	// Customizer that only updates the storage request
	pvcCustomizer := func(existing, desired ctrlclient.Object) (ctrlclient.Object, error) {
		existingPVC := existing.(*corev1.PersistentVolumeClaim)
		desiredPVC := desired.(*corev1.PersistentVolumeClaim)

		if !reflect.DeepEqual(existingPVC.Spec.Resources.Requests, desiredPVC.Spec.Resources.Requests) {
			existingPVC.Spec.Resources.Requests = desiredPVC.Spec.Resources.Requests
			if err := r.Update(ctx, existingPVC); err != nil {
				return nil, fmt.Errorf("update pvc: %w", err)
			}
			return existingPVC, nil
		}
		return desiredPVC, nil
	}

	pvc, err := utils.UpsertResource(ctx, r.Client, piHoleCluster, desired, pvcName, log, pvcCustomizer)
	if err != nil {
		return nil, err
	}

	return pvc.(*corev1.PersistentVolumeClaim), nil
}

// --------------------
// PodMonitor
// --------------------
func (r *Reconciler) ensurePodMonitor(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	if piHoleCluster.Spec.Monitoring == nil ||
		piHoleCluster.Spec.Monitoring.PodMonitor == nil ||
		!piHoleCluster.Spec.Monitoring.PodMonitor.Enabled {
		return nil
	}

	log := logf.FromContext(ctx)
	pmName := piHoleCluster.Name

	desired := &monitoringv1.PodMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pmName,
			Namespace: piHoleCluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":     "pihole",
				"app.kubernetes.io/instance": piHoleCluster.Name,
			},
		},
		Spec: monitoringv1.PodMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "pihole",
					"app.kubernetes.io/instance": piHoleCluster.Name,
				},
			},
			PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
				{Port: utils.PtrTo("metrics"), Path: "/metrics", Interval: "30s"},
			},
		},
	}

	_, err := utils.UpsertResource(ctx, r.Client, piHoleCluster, desired, pmName, log, nil)
	if err != nil {
		return err
	}

	return nil
}

// --------------------
// Ingress
// --------------------
func (r *Reconciler) ensureIngress(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	if piHoleCluster.Spec.Ingress == nil || !piHoleCluster.Spec.Ingress.Enabled {
		return nil
	}

	log := logf.FromContext(ctx)
	ingName := piHoleCluster.Name

	desired := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingName,
			Namespace: piHoleCluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":     "pihole",
				"app.kubernetes.io/instance": piHoleCluster.Name,
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: piHoleCluster.Spec.Ingress.Domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: utils.PtrTo(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: piHoleCluster.Name + "-rw",
											Port: networkingv1.ServiceBackendPort{Number: 80},
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

	_, err := utils.UpsertResource(ctx, r.Client, piHoleCluster, desired, ingName, log, nil)
	if err != nil {
		return err
	}

	return nil
}

// --------------------
// DNS Service
// --------------------
func (r *Reconciler) ensureDNSService(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	log := logf.FromContext(ctx)
	svcName := fmt.Sprintf("%s-dns", piHoleCluster.Name)

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: piHoleCluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":     "pihole",
				"app.kubernetes.io/instance": piHoleCluster.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: piHoleCluster.Spec.ServiceType,
			Ports: []corev1.ServicePort{
				{Name: "dns", Protocol: corev1.ProtocolUDP, Port: 53, TargetPort: intstr.FromInt32(53)},
			},
			Selector: map[string]string{
				"app.kubernetes.io/name":     "pihole",
				"app.kubernetes.io/instance": piHoleCluster.Name,
			},
		},
	}

	_, err := utils.UpsertResource(ctx, r.Client, piHoleCluster, desired, svcName, log, nil)
	if err != nil {
		return err
	}

	return nil
}

// --------------------
// EnvConfigMap
// --------------------
func (r *Reconciler) ensurePiHoleEnvCM(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (*corev1.ConfigMap, error) {
	if piHoleCluster.Spec.Config == nil || len(piHoleCluster.Spec.Config.EnvVars) == 0 {
		return nil, nil
	}

	log := logf.FromContext(ctx)
	cmName := fmt.Sprintf("%s-env", piHoleCluster.Name)

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: piHoleCluster.Namespace,
		},
		Data: piHoleCluster.Spec.Config.EnvVars,
	}

	cm, err := utils.UpsertResource(ctx, r.Client, piHoleCluster, desired, cmName, log, nil)
	if err != nil {
		return nil, err
	}

	return cm.(*corev1.ConfigMap), nil
}
