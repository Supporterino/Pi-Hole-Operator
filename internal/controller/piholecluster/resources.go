package piholecluster

import (
	"context"
	"fmt"
	"reflect"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
	"supporterino.de/pihole/internal/utils"
)

// --------------------
// RW StatefulSet
// --------------------

func (r *PiHoleClusterReconciler) ensureReadWriteSTS(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	stsName := fmt.Sprintf("%s-rw", piHoleCluster.Name)

	// Desired StatefulSet
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
			ServiceName: stsName, // headless service for the STS
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
	addExporterIfEnabled(piHoleCluster, &desired.Spec.Template.Spec)

	// add api password env
	secret, err := r.ensureAPISecret(ctx, piHoleCluster)
	if err != nil {
		return err
	}
	if secret != nil {
		// Determine the key to use in the Secret.
		var key string
		if piHoleCluster.Spec.Config != nil && piHoleCluster.Spec.Config.APIPassword.SecretRef != nil {
			key = piHoleCluster.Spec.Config.APIPassword.SecretRef.Key
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
	pvc, err := r.ensurePiHolePVC(ctx, piHoleCluster)
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
	envCM, err := r.ensurePiHoleEnvCM(ctx, piHoleCluster)
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
	if !reflect.DeepEqual(piHoleCluster.Spec.Resources, corev1.ResourceRequirements{}) {
		desired.Spec.Template.Spec.Containers[0].Resources = piHoleCluster.Spec.Resources
	}

	// PodSecurityContext – applied to the *entire* pod
	if piHoleCluster.Spec.Security != nil && piHoleCluster.Spec.Security.PodSecurityContext != nil {
		desired.Spec.Template.Spec.SecurityContext = piHoleCluster.Spec.Security.PodSecurityContext
	}

	// ContainerSecurityContext – applied to the PiHole container only
	if piHoleCluster.Spec.Security != nil && piHoleCluster.Spec.Security.ContainerSecurityContext != nil {
		desired.Spec.Template.Spec.Containers[0].SecurityContext = piHoleCluster.Spec.Security.ContainerSecurityContext
	}

	// Ensure the StatefulSet is owned by the PiHoleCluster (controller-runtime will set OwnerReference)
	if err := ctrl.SetControllerReference(piHoleCluster, desired, r.Scheme); err != nil {
		return err
	}

	// Reconcile: create or update the StatefulSet
	existing := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: stsName, Namespace: piHoleCluster.Namespace}, existing)
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

// isRWPodReady returns true when the RW stateful‑set has at least one pod that reports Ready:true.
func (r *PiHoleClusterReconciler) isRWPodReady(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (bool, error) {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(piHoleCluster.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": "pihole",
			"app.kubernetes.io/instance":  piHoleCluster.Name,
			"supporterino.de/pihole-role": "readwrite"},
	); err != nil {
		return false, fmt.Errorf("list RW pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return false, nil // no pod yet
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

// createReadOnlySTS creates or updates the read‑only StatefulSet.
func (r *PiHoleClusterReconciler) ensureReadOnlySTS(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	if piHoleCluster.Spec.Replicas <= 0 {
		return nil
	}

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
			ServiceName: stsName, // headless service for the RO STS
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

	// add exporter if enabled
	addExporterIfEnabled(piHoleCluster, &desired.Spec.Template.Spec)

	// add api password env
	secret, err := r.ensureAPISecret(ctx, piHoleCluster)
	if err != nil {
		return err
	}
	if secret != nil {
		// Determine the key to use in the Secret.
		var key string
		if piHoleCluster.Spec.Config != nil && piHoleCluster.Spec.Config.APIPassword.SecretRef != nil {
			key = piHoleCluster.Spec.Config.APIPassword.SecretRef.Key
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

	// 1️⃣ Create / get the ConfigMap
	envCM, err := r.ensurePiHoleEnvCM(ctx, piHoleCluster)
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
	if !reflect.DeepEqual(piHoleCluster.Spec.Resources, corev1.ResourceRequirements{}) {
		desired.Spec.Template.Spec.Containers[0].Resources = piHoleCluster.Spec.Resources
	}

	// PodSecurityContext – applied to the *entire* pod
	if piHoleCluster.Spec.Security != nil && piHoleCluster.Spec.Security.PodSecurityContext != nil {
		desired.Spec.Template.Spec.SecurityContext = piHoleCluster.Spec.Security.PodSecurityContext
	}

	// ContainerSecurityContext – applied to the PiHole container only
	if piHoleCluster.Spec.Security != nil && piHoleCluster.Spec.Security.ContainerSecurityContext != nil {
		desired.Spec.Template.Spec.Containers[0].SecurityContext = piHoleCluster.Spec.Security.ContainerSecurityContext
	}

	if err := ctrl.SetControllerReference(piHoleCluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: stsName, Namespace: piHoleCluster.Namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Update only the fields that can change (replicas, image, env)
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		return r.Update(ctx, existing)
	}
	return nil
}

// isRWPodReady returns true when the RW stateful‑set has at least one pod that reports Ready:true.
func (r *PiHoleClusterReconciler) areROPodsReady(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (bool, error) {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(piHoleCluster.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": "pihole",
			"app.kubernetes.io/instance":  piHoleCluster.Name,
			"supporterino.de/pihole-role": "readonly"},
	); err != nil {
		return false, fmt.Errorf("list RO pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return false, nil // no pod yet
	}

	for _, p := range podList.Items {
		if utils.IsPodReady(&p) {
			return true, nil
		}
	}
	return false, nil
}

// --------------------
// API Secret
// --------------------

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

// --------------------
// PW PersistentVolumeClaim
// --------------------

func (r *PiHoleClusterReconciler) ensurePiHolePVC(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (*corev1.PersistentVolumeClaim, error) {
	log := logf.FromContext(ctx)

	pvcName := fmt.Sprintf("%s-data", piHoleCluster.Name)

	// Default values
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

	// OwnerRef – so the PVC is garbage‑collected with the cluster
	if err := ctrl.SetControllerReference(piHoleCluster, desired, r.Scheme); err != nil {
		return nil, err
	}

	// Reconcile: create or update the PVC
	existing := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: piHoleCluster.Namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			log.Error(err, "Failed to create PVC")
			return nil, err
		}
		log.V(0).Info("PVC created", "name", pvcName)
		return desired, nil
	}
	if err != nil {
		log.V(1).Info("Error while fetching PVC", "err:", err, "pvcName:", pvcName)
		return nil, err
	}

	// Update only the spec that can change (size, access modes, class)
	// Update only mutable field: Resources.Requests
	if !reflect.DeepEqual(existing.Spec.Resources.Requests, desired.Spec.Resources.Requests) {
		existing.Spec.Resources.Requests = desired.Spec.Resources.Requests
		if err := r.Update(ctx, existing); err != nil {
			log.Error(err, "Failed to update PVC")
			return nil, err
		}
	}
	return existing, nil
}

// --------------------
// PodMonitor
// --------------------

// ensurePodMonitor creates or updates a PodMonitor that scrapes the exporter.
func (r *PiHoleClusterReconciler) ensurePodMonitor(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	// No pod‑monitor requested → nothing to do
	if piHoleCluster.Spec.Monitoring == nil ||
		piHoleCluster.Spec.Monitoring.PodMonitor == nil ||
		!piHoleCluster.Spec.Monitoring.PodMonitor.Enabled {
		return nil
	}

	// Desired PodMonitor name – same as the cluster for simplicity
	pmName := piHoleCluster.Name

	// Build the PodMonitor spec
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
			// Select the exporter container via labels
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "pihole",
					"app.kubernetes.io/instance": piHoleCluster.Name,
				},
			}, // Service discovery via the headless service created by the STS
			PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
				{
					Port:     utils.PtrTo("metrics"), // exporter port name
					Path:     "/metrics",             // default path
					Interval: "30s",
				},
			},
		},
	}

	// Owner reference so the PodMonitor gets garbage‑collected
	if err := ctrl.SetControllerReference(piHoleCluster, desired, r.Scheme); err != nil {
		return err
	}

	// Reconcile: create or update the PodMonitor
	existing := &monitoringv1.PodMonitor{}
	err := r.Get(ctx, types.NamespacedName{Name: pmName, Namespace: piHoleCluster.Namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Update only the spec that can change (selector, endpoints)
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		return r.Update(ctx, existing)
	}
	return nil
}

// --------------------
// Ingress
// --------------------

func (r *PiHoleClusterReconciler) ensureIngress(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
	// No ingress requested → nothing to do
	if piHoleCluster.Spec.Ingress == nil || !piHoleCluster.Spec.Ingress.Enabled {
		return nil
	}

	// Desired Ingress name – same as the cluster for simplicity
	ingName := piHoleCluster.Name

	// Build the Ingress spec
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
											Name: piHoleCluster.Name + "-rw", // headless service created by the STS
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
	if err := ctrl.SetControllerReference(piHoleCluster, desired, r.Scheme); err != nil {
		return err
	}

	// Reconcile: create or update the Ingress
	existing := &networkingv1.Ingress{}
	err := r.Get(ctx, types.NamespacedName{Name: ingName, Namespace: piHoleCluster.Namespace}, existing)
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

// --------------------
// DNS Service
// --------------------

// ensureDNSService creates/updates a Service that selects both RW and RO pods.
func (r *PiHoleClusterReconciler) ensureDNSService(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) error {
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
			Type: piHoleCluster.Spec.ServiceType, // ClusterIP or LoadBalancer
			Ports: []corev1.ServicePort{
				{
					Name:       "dns",
					Protocol:   corev1.ProtocolUDP,
					Port:       53,                   // service port
					TargetPort: intstr.FromInt32(53), // container port
				},
			},
			Selector: map[string]string{
				"app.kubernetes.io/name":     "pihole",
				"app.kubernetes.io/instance": piHoleCluster.Name,
			},
		},
	}

	// Owner reference
	if err := ctrl.SetControllerReference(piHoleCluster, desired, r.Scheme); err != nil {
		return err
	}

	// Reconcile: create or update the Service
	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: piHoleCluster.Namespace}, existing)
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

// --------------------
// ENV ConfigMap
// --------------------

// ensurePiHoleEnvCM creates a ConfigMap that contains the user‑supplied env vars.
func (r *PiHoleClusterReconciler) ensurePiHoleEnvCM(ctx context.Context, piHoleCluster *supporterinodev1alpha1.PiHoleCluster) (*corev1.ConfigMap, error) {
	if piHoleCluster.Spec.Config == nil || len(piHoleCluster.Spec.Config.EnvVars) == 0 {
		return nil, nil // nothing to do
	}

	cmName := fmt.Sprintf("%s-env", piHoleCluster.Name)

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: piHoleCluster.Namespace,
		},
		Data: piHoleCluster.Spec.Config.EnvVars, // key/value pairs
	}

	if err := ctrl.SetControllerReference(piHoleCluster, desired, r.Scheme); err != nil {
		return nil, err
	}

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: piHoleCluster.Namespace}, existing)
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
