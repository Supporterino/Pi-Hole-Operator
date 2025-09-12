package controller

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	supporterinodev1alpha1 "supporterino.de/pihole/api/v1alpha1"
)

// addExporterIfEnabled appends the exporter container to podSpec if monitoring.exporter.enabled == true.
func addExporterIfEnabled(piHoleCluster *supporterinodev1alpha1.PiHoleCluster, podSpec *corev1.PodSpec) {
	if piHoleCluster.Spec.Monitoring == nil ||
		piHoleCluster.Spec.Monitoring.Exporter == nil ||
		!piHoleCluster.Spec.Monitoring.Exporter.Enabled {
		return
	}

	exporter := corev1.Container{
		Name:  "pihole-exporter",
		Image: "ekofr/pihole-exporter:latest", // choose the tag you want
		Ports: []corev1.ContainerPort{
			{ContainerPort: 9615, Name: "metrics"},
		},
		Env: []corev1.EnvVar{
			{Name: "TZ", Value: "UTC"},
		},
	}

	// Resources
	if !reflect.DeepEqual(piHoleCluster.Spec.Monitoring.Exporter.Resources, corev1.ResourceRequirements{}) {
		exporter.Resources = piHoleCluster.Spec.Monitoring.Exporter.Resources
	}

	// ContainerSecurityContext – exporter only
	if piHoleCluster.Spec.Monitoring.Exporter.ContainerSecurityContext != nil {
		exporter.SecurityContext = piHoleCluster.Spec.Monitoring.Exporter.ContainerSecurityContext
	}

	podSpec.Containers = append(podSpec.Containers, exporter)
}
