package v1

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PolarsClusterSpec defines the desired state of PolarsCluster
// +kubebuilder:validation:XValidation:rule="!has(self.license.onPremEnterprise) || (has(self.acceptEula) && self.acceptEula)",message="acceptEula must be true when using the On-Prem Enterprise license"
// +kubebuilder:validation:XValidation:rule="!has(self.runtime) || (has(self.version) && size(self.version) > 0) || (has(self.runtime.composed.dist) && has(self.runtime.composed.dist.tag) && size(self.runtime.composed.dist.tag) > 0)",message="version is required when runtime is configured (unless runtime.composed.dist.tag is set)"
type PolarsClusterSpec struct {
	Telemetry *TelemetrySpec `json:"telemetry,omitempty"`
	// +kubebuilder:default=info
	// +optional
	LogLevel *LogLevel `json:"logLevel,omitempty"`

	// +optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// ClusterID uniquely identifies the Polars cluster in a multi-tenant
	// environment. Defaults to "<namespace>/<name>".
	// +optional
	ClusterID string `json:"clusterID,omitempty"`

	// ClusterDomain is the Kubernetes cluster DNS domain (the suffix used in
	// "<svc>.<ns>.svc.<domain>").
	// +kubebuilder:default="cluster.local"
	// +optional
	ClusterDomain string `json:"clusterDomain,omitempty"`

	// AcceptEula must be set to true to use the On-Prem Enterprise license.
	// +kubebuilder:default=false
	// +optional
	AcceptEula bool `json:"acceptEula,omitempty"`

	// Version is the Polars on-premises release to run. It is used as the
	// composed runtime's dist tag unless runtime.composed.dist.tag overrides
	// it.
	// +optional
	Version string `json:"version,omitempty"`

	// Runtime composes the scheduler/worker containers from the Polars
	// distribution and a Python base image. When nil, the pod templates must
	// bring their own image.
	// +optional
	Runtime *RuntimeSpec `json:"runtime,omitempty"`

	License LicenseSpec `json:"license"`

	// +kubebuilder:default=true
	AllowLocalSinks bool `json:"allowLocalSinks,omitempty"`

	// +kubebuilder:default=false
	AllowLocalScans bool `json:"allowLocalScans,omitempty"`

	// +kubebuilder:default=true
	AllowAnonymousUsers bool `json:"allowAnonymousUsers,omitempty"`

	// RequireFreeWorkers makes the scheduler wait for free workers before
	// starting query execution. A nil count defaults to the worker pool's
	// replica count.
	// +optional
	RequireFreeWorkers *RequireFreeWorkersSpec `json:"requireFreeWorkers,omitempty"`

	// +optional
	AnonymousResults *AnonymousResultsSpec `json:"anonymousResults,omitempty"`

	// CheckpointPeriod is the period at which checkpoints are created.
	// Checkpointing is enabled whenever CheckpointData is configured.
	// +kubebuilder:default="20m"
	// +optional
	CheckpointPeriod *metav1.Duration `json:"checkpointPeriod,omitempty"`

	// +optional
	CheckpointData *CheckpointDataSpec `json:"checkpointData,omitempty"`

	// +optional
	Lineage *LineageSpec `json:"lineage,omitempty"`

	// +optional
	Observatory *ObservatorySpec `json:"observatory,omitempty"`

	// HostMetrics controls host metrics collection on both the scheduler and
	// worker pods. Defaults to enabled when unset.
	// +optional
	HostMetrics *HostMetricsSpec `json:"hostMetrics,omitempty"`

	SchedulerSpec SchedulerSpec         `json:"schedulerSpec"`
	WorkerPool    WorkerPoolDeclaration `json:"workerPool"`
}

// ValueOrSource holds either a literal value or a reference to where the
// value can be found (Secret key, ConfigMap key, field, ...).
// +kubebuilder:validation:ExactlyOneOf=value;valueFrom
type ValueOrSource struct {
	// +optional
	Value string `json:"value,omitempty"`

	// +optional
	ValueFrom *v1.EnvVarSource `json:"valueFrom,omitempty"`
}

// +kubebuilder:validation:ExactlyOneOf=onPrem;onPremEnterprise;licenseServer
type LicenseSpec struct {
	// +optional
	OnPrem *LicenseOnPremSpec `json:"onPrem,omitempty"`

	// +optional
	OnPremEnterprise *LicenseOnPremEnterpriseSpec `json:"onPremEnterprise,omitempty"`

	// LicenseServer connects to an offline Polars license server.
	// +optional
	LicenseServer *LicenseServerSpec `json:"licenseServer,omitempty"`
}

type LicenseServerSpec struct {
	// URI of the license server.
	URI string `json:"uri"`
}

type LicenseOnPremSpec struct {
	ClientID     ValueOrSource `json:"clientID"`
	ClientSecret ValueOrSource `json:"clientSecret"`
	WorkspaceID  ValueOrSource `json:"workspaceID"`

	// LicenseData persists the license certificate in an existing
	// PersistentVolumeClaim instead of an emptyDir, so it survives scheduler
	// pod restarts.
	// +optional
	LicenseData *PersistentVolumeClaimRef `json:"licenseData,omitempty"`
}

// PersistentVolumeClaimRef references an existing PersistentVolumeClaim.
// The operator never creates claims itself — provision the PVC separately
// and point this at it.
type PersistentVolumeClaimRef struct {
	ClaimName string `json:"claimName"`
}

type LicenseOnPremEnterpriseSpec struct {
	// SecretName is the name of the Secret containing the license key.
	SecretName string `json:"secretName"`

	// SecretProperty is the key on the Secret containing the license key.
	SecretProperty string `json:"secretProperty"`
}

type TelemetrySpec struct {
	OTLPEndpoint ValueOrSource `json:"otlpEndpoint"`
}

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// RuntimeSpec selects how the Polars runtime is provided.
type RuntimeSpec struct {
	Composed ComposedRuntimeSpec `json:"composed"`
}

// ComposedRuntimeSpec composes the runtime from the Polars distribution image
// (copied into an emptyDir by an init container) executed on a Python base
// image.
// +kubebuilder:validation:XValidation:rule="!has(self.polarsExtras) || size(self.polarsExtras) == 0 || self.polarsExtras.contains('cloudpickle')",message="polarsExtras must include cloudpickle"
type ComposedRuntimeSpec struct {
	// Dist is the Polars distribution image. Repository defaults to
	// "polarscloud/polars-on-premises" and tag defaults to spec.version.
	// +optional
	Dist ImageSpec `json:"dist,omitempty"`

	// Runtime is the Python base image the distribution runs on. Defaults to
	// "python:3.13.9-slim-bookworm".
	// +optional
	Runtime *ImageSpec `json:"runtime,omitempty"`

	// Requirements is a requirements.txt to install additional Python
	// packages into the runtime.
	// +optional
	Requirements string `json:"requirements,omitempty"`

	// PolarsExtras are the Polars pip extras to install. Must include
	// "cloudpickle". Defaults to the full extras set.
	// +optional
	PolarsExtras string `json:"polarsExtras,omitempty"`
}

type ImageSpec struct {
	// +optional
	Repository string `json:"repository,omitempty"`

	// +optional
	Tag string `json:"tag,omitempty"`

	// +optional
	PullPolicy v1.PullPolicy `json:"pullPolicy,omitempty"`
}

type RequireFreeWorkersSpec struct {
	// Count is the number of free workers to wait for. Defaults to the
	// worker pool's replica count.
	// +optional
	Count *uint32 `json:"count,omitempty"`
}

type ObservatorySpec struct {
	// MaxMetricsBytesTotal is the maximum number of bytes for host metrics storage.
	// +kubebuilder:default=104857600
	// +optional
	MaxMetricsBytesTotal int64 `json:"maxMetricsBytesTotal,omitempty"`

	// PersistentVolumeClaim persists the observatory database in an existing
	// claim instead of an emptyDir.
	// +optional
	PersistentVolumeClaim *PersistentVolumeClaimRef `json:"persistentVolumeClaim,omitempty"`
}

// +kubebuilder:validation:ExactlyOneOf=s3;abs;gcs;sharedFilesystem
type AnonymousResultsSpec struct {
	// +optional
	S3 *AnonymousResultsS3Spec `json:"s3,omitempty"`

	// +optional
	ABS *AnonymousResultsABSSpec `json:"abs,omitempty"`

	// +optional
	GCS *AnonymousResultsGCSSpec `json:"gcs,omitempty"`

	// +optional
	SharedFilesystem *SharedFilesystemSpec `json:"sharedFilesystem,omitempty"`
}

type AnonymousResultsS3Spec struct {
	Endpoint        string           `json:"endpoint"`
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`
	Options         []v1.EnvVar      `json:"options,omitempty"`
}

type AnonymousResultsABSSpec struct {
	EndpointURL     string           `json:"endpointURL"`
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`
	Options         []v1.EnvVar      `json:"options,omitempty"`
}

type AnonymousResultsGCSSpec struct {
	EndpointURL     string           `json:"endpointURL"`
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`
	Options         []v1.EnvVar      `json:"options,omitempty"`
}

// SharedFilesystemSpec configures a filesystem path on an already-mounted
// shared volume (e.g. an NFS or CSI driver mount supplied via the pod
// template's volumes/volumeMounts).
type SharedFilesystemSpec struct {
	Path string `json:"path"`
}

type LineageSpec struct {
	Transport LineageTransportSpec `json:"transport"`
}

type LineageTransportSpec struct {
	// +kubebuilder:validation:Required
	HTTP *LineageTransportHTTPSpec `json:"http"`
}

type LineageTransportHTTPSpec struct {
	Endpoint string `json:"endpoint"`
}

type SchedulerSpec struct {
	// +optional
	Services SchedulerServicesSpec `json:"services,omitempty"`

	// +optional
	PodTemplate *v1.PodTemplateSpec `json:"podTemplate,omitempty"`
}

type SchedulerServicesSpec struct {
	// Scheduler exposes the client-facing scheduler port (5051).
	// +optional
	Scheduler ServiceConfig `json:"scheduler,omitempty"`

	// Internal exposes the worker-facing scheduler (5050) and observatory
	// gRPC (5049) ports.
	// +optional
	Internal ServiceConfig `json:"internal,omitempty"`

	// Observatory exposes the observatory dashboard REST port (3001).
	// +optional
	Observatory ServiceConfig `json:"observatory,omitempty"`
}

type ServiceConfig struct {
	// +kubebuilder:default="ClusterIP"
	// +optional
	Type v1.ServiceType `json:"type,omitempty"`

	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// WorkerPoolDeclaration defines the desired state of a PolarsCluster's worker pool.
// +kubebuilder:validation:XValidation:rule="self.replicas >= self.minReplicas && (!has(self.maxReplicas) || self.replicas <= self.maxReplicas)",message="replicas must be within [minReplicas, maxReplicas]"
type WorkerPoolDeclaration struct {
	// +optional
	PodTemplate v1.PodTemplateSpec `json:"podTemplate,omitempty"`

	// +kubebuilder:validation:Minimum=0
	MinReplicas int32 `json:"minReplicas"`
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`

	// +optional
	WorkersToDelete []string `json:"workersToDelete,omitempty"`

	// HeartBeatInterval between workers and the scheduler. Defaults to 5s.
	// +optional
	HeartBeatInterval *metav1.Duration `json:"heartBeatInterval,omitempty"`

	// +optional
	ShuffleData *ShuffleDataSpec `json:"shuffleData,omitempty"`

	// +optional
	TemporaryData *TemporaryDataSpec `json:"temporaryData,omitempty"`
}

type HostMetricsSpec struct {
	Enabled bool `json:"enabled"`
}

type TemporaryDataSpec struct {
	// +optional
	EphemeralVolumeClaim *EphemeralVolumeClaimSpec `json:"ephemeralVolumeClaim,omitempty"`
}

// +kubebuilder:validation:ExactlyOneOf=local;sharedFilesystem;s3;abs;gcs
type ShuffleDataSpec struct {
	// Local stores shuffle data on per-worker local storage. When path is
	// omitted the operator mounts an emptyDir at a default location; set
	// path to use storage you mount yourself (via the pod template's
	// volumes/volumeMounts, or volumes injected at admission time).
	// +optional
	Local *LocalFilesystemSpec `json:"local,omitempty"`

	// SharedFilesystem stores shuffle data on a filesystem path all workers
	// see (e.g. an NFS or CSI mount). Mounting is the user's responsibility.
	// +optional
	SharedFilesystem *SharedFilesystemSpec `json:"sharedFilesystem,omitempty"`

	// +optional
	S3 *ShuffleDataS3Spec `json:"s3,omitempty"`

	// +optional
	ABS *ShuffleDataABSSpec `json:"abs,omitempty"`

	// +optional
	GCS *ShuffleDataGCSSpec `json:"gcs,omitempty"`
}

// LocalFilesystemSpec configures per-pod local storage at the given path,
// or at an operator-managed emptyDir when path is omitted.
type LocalFilesystemSpec struct {
	// +optional
	Path string `json:"path,omitempty"`
}

// EphemeralVolumeClaimSpec configures a per-pod generic ephemeral volume.
type EphemeralVolumeClaimSpec struct {
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`

	Size resource.Quantity `json:"size"`
}

type ShuffleDataS3Spec struct {
	Endpoint        string           `json:"endpoint"`
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`
	Options         []v1.EnvVar      `json:"options,omitempty"`
}

type ShuffleDataABSSpec struct {
	EndpointURL     string           `json:"endpointURL"`
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`
	Options         []v1.EnvVar      `json:"options,omitempty"`
}

type ShuffleDataGCSSpec struct {
	EndpointURL     string           `json:"endpointURL"`
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`
	Options         []v1.EnvVar      `json:"options,omitempty"`
}

// +kubebuilder:validation:ExactlyOneOf=sharedFilesystem;s3;abs;gcs
type CheckpointDataSpec struct {
	// SharedFilesystem stores checkpoint data on a filesystem path all
	// workers see. Mounting is the user's responsibility.
	// +optional
	SharedFilesystem *SharedFilesystemSpec `json:"sharedFilesystem,omitempty"`

	// +optional
	S3 *CheckpointS3Spec `json:"s3,omitempty"`

	// +optional
	ABS *CheckpointABSSpec `json:"abs,omitempty"`

	// +optional
	GCS *CheckpointGCSSpec `json:"gcs,omitempty"`
}

type CheckpointS3Spec struct {
	Endpoint string      `json:"endpoint"`
	Options  []v1.EnvVar `json:"options,omitempty"`
}

type CheckpointABSSpec struct {
	EndpointURL string      `json:"endpointURL"`
	Options     []v1.EnvVar `json:"options,omitempty"`
}

type CheckpointGCSSpec struct {
	EndpointURL string      `json:"endpointURL"`
	Options     []v1.EnvVar `json:"options,omitempty"`
}

// SchedulerStatus is the observed state of a PolarsCluster's scheduler.
type SchedulerStatus struct {
	Ready bool `json:"ready"`
}

// WorkerPoolStatus is the observed state of a PolarsCluster's worker pool.
type WorkerPoolStatus struct {
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	// +optional
	Selector string `json:"selector,omitempty"`
}

// PolarsClusterStatus defines the observed state of PolarsCluster.
type PolarsClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the PolarsCluster resource.
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

	// +optional
	Scheduler SchedulerStatus `json:"scheduler,omitempty"`

	// +optional
	WorkerPool WorkerPoolStatus `json:"workerPool,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PolarsCluster is the Schema for the polarsclusters API
type PolarsCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of PolarsCluster
	// +required
	Spec PolarsClusterSpec `json:"spec"`

	// status defines the observed state of PolarsCluster
	// +optional
	Status PolarsClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PolarsClusterList contains a list of PolarsCluster
type PolarsClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PolarsCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &PolarsCluster{}, &PolarsClusterList{})
		return nil
	})
}
