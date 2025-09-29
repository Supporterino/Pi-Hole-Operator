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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// PiHoleClusterSpec defines the desired state of PiHoleCluster
type PiHoleClusterSpec struct {
	// Replicas is the desired number of read‑only PiHole replicas.
	//
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas"`

	// Ingress holds configuration for exposing the cluster via an Ingress.
	//
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Sync defines optional sync behaviour for the cluster.
	//
	// +optional
	Sync *SyncSpec `json:"sync,omitempty"`

	// Monitoring holds optional monitoring configuration.
	//
	// +optional
	Monitoring *MonitoringSpec `json:"monitoring,omitempty"`

	// ServiceType controls the Kubernetes Service type.
	//
	// +kubebuilder:validation:Enum=ClusterIP;LoadBalancer
	// +kubebuilder:default=ClusterIP
	ServiceType corev1.ServiceType `json:"serviceType,omitempty"`

	Config *ConfigSpec `json:"config,omitempty"`

	// Persistence holds PVC configuration for the read‑write PiHole pod.
	//
	// +optional
	Persistence *PersistenceSpec `json:"persistence,omitempty"`

	// Resources is a standard ResourceRequirements block (CPU/mem).
	//
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Security holds pod & container security contexts for PiHole.
	//
	// +optional
	Security *SecuritySpec `json:"security,omitempty"`

	// LivenessProbe defines the liveness probe for the exporter container.
	//
	// The default is a simple TCP check on port 80 (the exporter’s HTTP
	// endpoint) that runs every 30 s and has a timeout of 5 s.  The user can
	// override any field; if the struct is omitted entirely, the defaults
	// above are used.
	//
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`
	// ReadinessProbe defines the readiness probe for the exporter container.
	//
	// The default is a simple HTTP GET to `/metrics` on port 80 that runs
	// every 10 s and has a timeout of 2 s.  As with the liveness probe,
	// all fields are optional and may be overridden by the user.
	//
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
}

// IngressSpec defines the ingress configuration for a PiHoleCluster.
type IngressSpec struct {
	// Enabled indicates whether an Ingress resource should be created.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// Domain is the fully‑qualified domain that the Ingress will expose.
	//
	// +kubebuilder:validation:MinLength=1
	Domain string `json:"domain,omitempty"`
}

// SyncSpec defines optional sync behaviour for a PiHoleCluster.
type SyncSpec struct {
	// Config indicates whether the operator should sync PiHole configuration files.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:default=true
	Config bool `json:"config"`

	// AdLists indicates whether the operator should sync PiHole ad‑list files.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:default=true
	AdLists bool `json:"adLists"`
}

// MonitoringSpec defines optional monitoring configuration for a PiHoleCluster.
type MonitoringSpec struct {
	// Exporter contains the exporter configuration.
	//
	// +optional
	Exporter *ExporterSpec `json:"exporter,omitempty"`

	// PodMonitor contains the pod‑monitor configuration.
	//
	// +optional
	PodMonitor *PodMonitorSpec `json:"podMonitor,omitempty"`
}

// ExporterSpec defines the exporter configuration.
type ExporterSpec struct {
	// Enabled indicates whether a Prometheus exporter should be deployed.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// Resources is a standard ResourceRequirements block (CPU/mem).
	//
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// ContainerSecurityContext applies to the individual container.
	//
	// +optional
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// LivenessProbe defines the liveness probe for the exporter container.
	//
	// The default is a simple TCP check on port 80 (the exporter’s HTTP
	// endpoint) that runs every 30 s and has a timeout of 5 s.  The user can
	// override any field; if the struct is omitted entirely, the defaults
	// above are used.
	//
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`
	// ReadinessProbe defines the readiness probe for the exporter container.
	//
	// The default is a simple HTTP GET to `/metrics` on port 80 that runs
	// every 10 s and has a timeout of 2 s.  As with the liveness probe,
	// all fields are optional and may be overridden by the user.
	//
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
}

// PodMonitorSpec defines the pod‑monitor configuration.
type PodMonitorSpec struct {
	// Enabled indicates whether a Prometheus pod‑monitor should be deployed.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
}

// ConfigSpec holds optional configuration knobs for PiHole.
type ConfigSpec struct {
	// APIPassword is the password used by the PiHole web‑server.
	//
	// +kubebuilder:validation:Required
	APIPassword APIPassword `json:"apiPassword"`

	// EnvVars is an arbitrary map of environment variables that will be
	// injected into the PiHole container via a ConfigMap.
	//
	// +optional
	EnvVars map[string]string `json:"env,omitempty"`

	// AdLists holds an optional array of URLs that the operator should keep in sync.
	// The list is created with type “block”, comment “Created by PiHole operator”
	// and groups `[0]`.  It is only processed when `spec.sync.adlists` is true.
	// +kubebuilder:validation:Optional
	AdLists []string `json:"adLists,omitempty"`
}

// APIPassword holds either a plain string or a reference to an existing secret.
type APIPassword struct {
	// Plain text password – only one of the two fields may be set.
	//
	// +kubebuilder:validation:MaxLength=128
	Password string `json:"password,omitempty"`

	// SecretRef points to a Secret that contains the password under key "password".
	//
	// +kubebuilder:validation:Xor=Password SecretRef
	SecretRef *corev1.SecretKeySelector `json:"secretRef,omitempty"`
}

type PersistenceSpec struct {
	// Size is the requested storage size, e.g. "10Gi".
	//
	// +kubebuilder:validation:Pattern=^[0-9]+[KMGTP]i?B$
	Size string `json:"size"`

	// StorageClassName is the name of a StorageClass to use.
	//
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// AccessModes lists the desired PV access modes.
	//
	// +optional
	// +kubebuilder:validation:MinItems=1
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
}

// SecuritySpec groups the two security context types
type SecuritySpec struct {
	// PodSecurityContext applies to the entire pod.
	//
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// ContainerSecurityContext applies to the individual container.
	//
	// +optional
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`
}

// PiHoleClusterStatus defines the observed state of PiHoleCluster.
type PiHoleClusterStatus struct {
	// ResourcesReady is true when all operator‑managed resources (STS, PVC, Service,
	// Ingress, PodMonitor, ConfigMap, Secret) exist and are in a healthy state.
	//
	// +optional
	ResourcesReady bool `json:"resourcesReady,omitempty"`
	// ConfigSynced indicates that the latest configuration has been
	// successfully pushed to all read‑only replicas.
	//
	// +kubebuilder:validation:Optional
	ConfigSynced bool `json:"configSynced,omitempty"`
	// AdListSynced indicates that the latest ad-list has been
	// successfully pushed to all replicas.
	//
	// +kubebuilder:validation:Optional
	AdListSynced bool `json:"adListSynced,omitempty"`
	// SyncedPods lists the names of all RO pods that have already
	// received the latest configuration.  A pod that is not listed
	// will trigger a sync when it becomes ready.
	SyncedPods []SyncedPod `json:"syncedPods,omitempty"`
	// LastSyncTime records the last time a sync was performed.
	// It is used to evaluate the cron schedule.
	LastConfigSyncTime metav1.Time `json:"lastConfigSyncTime,omitempty"`
	// LastSyncTime records the last time a sync was performed.
	// It is used to evaluate the cron schedule.
	LastAdListSyncTime metav1.Time `json:"lastAdListSyncTime,omitempty"`
	// LastError holds the most recent error message that caused ResourcesReady to be false.
	//
	// +optional
	LastError string `json:"lastError,omitempty"`
	// conditions represent the current state of the PiHoleCluster resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type SyncedPod struct {
	Name string    `json:"name"`
	UID  types.UID `json:"uid"` // the pod’s unique identifier
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PiHoleCluster is the Schema for the piholeclusters API
type PiHoleCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of PiHoleCluster
	// +required
	Spec PiHoleClusterSpec `json:"spec"`

	// status defines the observed state of PiHoleCluster
	// +optional
	Status PiHoleClusterStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// PiHoleClusterList contains a list of PiHoleCluster
type PiHoleClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PiHoleCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PiHoleCluster{}, &PiHoleClusterList{})
}
