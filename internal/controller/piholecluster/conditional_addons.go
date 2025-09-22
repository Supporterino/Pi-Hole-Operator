package piholecluster

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	supporterinodev1 "supporterino.de/pihole/api/v1"
)

// addExporterIfEnabled appends the exporter container to a pod spec when
// monitoring.exporter.enabled is true.
func addExporterIfEnabled(piHoleCluster *supporterinodev1.PiHoleCluster, podSpec *corev1.PodSpec, passwordEnvSource *corev1.EnvVarSource) {
	if piHoleCluster.Spec.Monitoring == nil ||
		piHoleCluster.Spec.Monitoring.Exporter == nil ||
		!piHoleCluster.Spec.Monitoring.Exporter.Enabled {
		return
	}

	exporter := corev1.Container{
		Name:  "pihole-exporter",
		Image: "ekofr/pihole-exporter:latest", // pick your tag
		Ports: []corev1.ContainerPort{
			{ContainerPort: 9617, Name: "metrics"},
		},
		Env: []corev1.EnvVar{
			{Name: "TZ", Value: "UTC"},
			{Name: "PIHOLE_PORT", Value: "80"},
		},
	}

	env := corev1.EnvVar{
		Name:      "PIHOLE_PASSWORD",
		ValueFrom: passwordEnvSource,
	}

	exporter.Env = append(exporter.Env, env)

	if !reflect.DeepEqual(piHoleCluster.Spec.Monitoring.Exporter.Resources, corev1.ResourceRequirements{}) {
		exporter.Resources = piHoleCluster.Spec.Monitoring.Exporter.Resources
	}

	if piHoleCluster.Spec.Monitoring.Exporter.ContainerSecurityContext != nil {
		exporter.SecurityContext = piHoleCluster.Spec.Monitoring.Exporter.ContainerSecurityContext
	}

	defaultExporterProbes(piHoleCluster.Spec.Monitoring.Exporter)

	if piHoleCluster.Spec.Monitoring.Exporter.LivenessProbe != nil {
		exporter.LivenessProbe = piHoleCluster.Spec.Monitoring.Exporter.LivenessProbe
	}
	if piHoleCluster.Spec.Monitoring.Exporter.ReadinessProbe != nil {
		exporter.ReadinessProbe = piHoleCluster.Spec.Monitoring.Exporter.ReadinessProbe
	}

	podSpec.Containers = append(podSpec.Containers, exporter)
}

func defaultExporterProbes(e *supporterinodev1.ExporterSpec) {
	if e.LivenessProbe == nil {
		e.LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("metrics")},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       30,
			TimeoutSeconds:      5,
		}
	}

	if e.ReadinessProbe == nil {
		e.ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/metrics", Port: intstr.FromString("metrics")},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
			TimeoutSeconds:      2,
		}
	}
}
