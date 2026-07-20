package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DefaultVersion is the Polars on-premises release deployed when
// spec.version is unset. Keep in sync with the +kubebuilder:default marker
// on PolarsClusterSpec.Version.
const DefaultVersion = "0.6.3"

// PolarsClusterSpec defines the desired state of PolarsCluster
// +kubebuilder:validation:XValidation:rule="!has(self.license.onPremEnterprise) || (has(self.acceptEula) && self.acceptEula)",message="acceptEula must be true when using the On-Prem Enterprise license"
type PolarsClusterSpec struct {
	// Telemetry configures exporting the cluster's OTLP traces and metrics.
	// +optional
	Telemetry *TelemetrySpec `json:"telemetry,omitempty"`

	// LogLevel is the log verbosity of the scheduler and workers.
	// +kubebuilder:default=info
	// +optional
	LogLevel *LogLevel `json:"logLevel,omitempty"`

	// ImagePullSecrets are references to Secrets in the same namespace to
	// use for pulling any of the cluster's images.
	// +optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// ClusterID uniquely identifies the Polars cluster in a multi-tenant
	// environment. Defaults to "<namespace>/<name>".
	// +optional
	ClusterID string `json:"clusterID,omitempty"`

	// AcceptEula must be set to true to use the On-Prem Enterprise license.
	// +kubebuilder:default=false
	// +optional
	AcceptEula bool `json:"acceptEula,omitempty"`

	// Version is the Polars on-premises release to run, as a semantic
	// version. The operator enforces its minimum supported release at
	// reconcile time. Version is used as the composed runtime's dist tag
	// unless runtime.composed.dist.tag overrides it; use that override for
	// non-release image tags.
	// +kubebuilder:default="0.6.3"
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`
	// +optional
	Version string `json:"version,omitempty"`

	// Runtime composes the scheduler/worker containers from the Polars
	// distribution and a Python base image. When nil, the pod templates must
	// bring their own image.
	// +optional
	Runtime *RuntimeSpec `json:"runtime,omitempty"`

	// License selects how the cluster's Polars license is provided.
	License LicenseSpec `json:"license"`

	// AllowLocalSinks permits workers to write query results to local disk.
	// Disabling this prevents all local writes; it is not possible to allow
	// only specific sink locations. Users can alternatively configure sinks
	// that write to object storage. More info:
	// https://docs.pola.rs/user-guide/io/cloud-storage/#writing-to-cloud-storage
	// +kubebuilder:default=true
	AllowLocalSinks bool `json:"allowLocalSinks,omitempty"`

	// AllowLocalScans permits workers to read query inputs from local disk.
	// Disabling this prevents all local reads; it is not possible to allow
	// only specific scan locations. Users can alternatively configure scans
	// that read from object storage. More info:
	// https://docs.pola.rs/user-guide/io/cloud-storage/#reading-from-cloud-storage
	// +kubebuilder:default=false
	AllowLocalScans bool `json:"allowLocalScans,omitempty"`

	// AllowAnonymousUsers permits queries without a username. When false,
	// all queries must be sent with a set username.
	// +kubebuilder:default=true
	AllowAnonymousUsers bool `json:"allowAnonymousUsers,omitempty"`

	// RequireFreeWorkers makes the scheduler wait for free workers before
	// starting query execution. A nil count defaults to the worker pool's
	// replica count.
	// +optional
	RequireFreeWorkers *RequireFreeWorkersSpec `json:"requireFreeWorkers,omitempty"`

	// AnonymousResults is where results of queries that don't specify a
	// result location are stored. Object storage is recommended so results
	// persist; the compute plane never cleans up anonymous results itself.
	// +optional
	AnonymousResults *AnonymousResultsSpec `json:"anonymousResults,omitempty"`

	// Checkpoint enables checkpointing of queries: the scheduler
	// periodically checkpoints completed stages to the configured data
	// location so queries can resume after failures.
	// +optional
	Checkpoint *CheckpointSpec `json:"checkpoint,omitempty"`

	// Lineage enables exporting query lineage events to an external
	// endpoint.
	// +optional
	Lineage *LineageSpec `json:"lineage,omitempty"`

	// Observatory configures the observatory dashboard's metrics storage.
	// +optional
	Observatory *ObservatorySpec `json:"observatory,omitempty"`

	// HostMetrics controls host metrics collection on both the scheduler and
	// worker pods. Defaults to enabled when unset.
	// +optional
	HostMetrics *HostMetricsSpec `json:"hostMetrics,omitempty"`

	// Scheduler configures the cluster's scheduler pod and Services; all of
	// it is optional when a runtime composes the scheduler container.
	// +optional
	Scheduler *SchedulerSpec `json:"scheduler,omitempty"`

	// WorkerPool declares the cluster's pool of worker pods.
	WorkerPool WorkerPoolDeclaration `json:"workerPool"`
}

// ValueOrSource holds either a literal value or a reference to where the
// value can be found (Secret key, ConfigMap key, field, ...).
// +kubebuilder:validation:ExactlyOneOf=value;valueFrom
type ValueOrSource struct {
	// Value is the literal value.
	// +optional
	Value string `json:"value,omitempty"`

	// ValueFrom references the value from a Secret key, ConfigMap key, pod
	// field, or container resource.
	// +optional
	ValueFrom *v1.EnvVarSource `json:"valueFrom,omitempty"`
}

// LicenseSpec selects exactly one way the cluster's Polars license is
// provided.
// +kubebuilder:validation:ExactlyOneOf=onPrem;onPremEnterprise
type LicenseSpec struct {
	// OnPrem licenses the cluster with Polars workspace client credentials.
	// +optional
	OnPrem *LicenseOnPremSpec `json:"onPrem,omitempty"`

	// OnPremEnterprise licenses the cluster from a Secret holding an
	// On-Prem Enterprise license key; requires acceptEula to be true.
	// +optional
	OnPremEnterprise *LicenseOnPremEnterpriseSpec `json:"onPremEnterprise,omitempty"`
}

// LicenseOnPremSpec licenses the cluster with Polars workspace client
// credentials.
type LicenseOnPremSpec struct {
	// ClientID of the Polars workspace.
	ClientID ValueOrSource `json:"clientID"`

	// ClientSecret of the Polars workspace.
	ClientSecret ValueOrSource `json:"clientSecret"`

	// WorkspaceID of the Polars workspace.
	WorkspaceID ValueOrSource `json:"workspaceID"`

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
	// ClaimName is the name of the existing PersistentVolumeClaim.
	ClaimName string `json:"claimName"`
}

// LicenseOnPremEnterpriseSpec reads the On-Prem Enterprise license key from
// a Secret.
type LicenseOnPremEnterpriseSpec struct {
	// SecretName is the name of the Secret containing the license key.
	SecretName string `json:"secretName"`

	// SecretProperty is the key on the Secret containing the license key.
	SecretProperty string `json:"secretProperty"`
}

// TelemetrySpec configures exporting the cluster's telemetry.
type TelemetrySpec struct {
	// OTLPEndpoint is the endpoint to send OTLP traces and metrics to.
	OTLPEndpoint ValueOrSource `json:"otlpEndpoint"`
}

// LogLevel is a log verbosity level: off, error, warn, info, debug, or
// trace.
// +kubebuilder:validation:Enum=off;error;warn;info;debug;trace
type LogLevel string

const (
	LogLevelOff   LogLevel = "off"
	LogLevelError LogLevel = "error"
	LogLevelWarn  LogLevel = "warn"
	LogLevelInfo  LogLevel = "info"
	LogLevelDebug LogLevel = "debug"
	LogLevelTrace LogLevel = "trace"
)

// RuntimeSpec selects how the Polars runtime is provided.
type RuntimeSpec struct {
	// Composed builds the scheduler and worker containers from the Polars
	// distribution image and a Python base image.
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
	Dist *ImageSpec `json:"dist,omitempty"`

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

// ImageSpec identifies a container image.
type ImageSpec struct {
	// Repository is the container image name, without a tag. More info:
	// https://kubernetes.io/docs/concepts/containers/images
	// +optional
	Repository string `json:"repository,omitempty"`

	// Tag is the container image tag.
	// +optional
	Tag string `json:"tag,omitempty"`

	// PullPolicy is the image pull policy: one of Always, Never, or
	// IfNotPresent. Defaults to Always if the tag is "latest", or
	// IfNotPresent otherwise. More info:
	// https://kubernetes.io/docs/concepts/containers/images#updating-images
	// +optional
	PullPolicy v1.PullPolicy `json:"pullPolicy,omitempty"`
}

// RequireFreeWorkersSpec makes the scheduler wait for a number of free
// workers before starting query execution.
type RequireFreeWorkersSpec struct {
	// Count is the number of free workers to wait for. Defaults to the
	// worker pool's replica count.
	// +optional
	Count *uint32 `json:"count,omitempty"`
}

// ObservatorySpec configures the observatory dashboard.
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

// AnonymousResultsSpec selects exactly one storage location for anonymous
// results.
// +kubebuilder:validation:ExactlyOneOf=s3;abs;gcs;sharedFilesystem
type AnonymousResultsSpec struct {
	// S3 stores anonymous results in AWS S3.
	// +optional
	S3 *AnonymousResultsS3Spec `json:"s3,omitempty"`

	// ABS stores anonymous results in Azure Blob Storage.
	// +optional
	ABS *AnonymousResultsABSSpec `json:"abs,omitempty"`

	// GCS stores anonymous results in Google Cloud Storage.
	// +optional
	GCS *AnonymousResultsGCSSpec `json:"gcs,omitempty"`

	// SharedFilesystem stores anonymous results on a filesystem path all
	// workers see. The path must match the location mounted on the client,
	// since the cluster returns the exact written paths back to the client.
	// +optional
	SharedFilesystem *SharedFilesystemSpec `json:"sharedFilesystem,omitempty"`
}

// AnonymousResultsS3Spec stores anonymous results in AWS S3.
type AnonymousResultsS3Spec struct {
	// Endpoint is the entire S3 URI. If the storage location requires
	// authentication, provide the credentials in options.
	Endpoint string `json:"endpoint"`

	// PresignDuration is how long anonymous results are presigned for.
	// Defaults to 8h.
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`

	// Options for the S3 storage location. These correspond to Object
	// Store's AmazonS3ConfigKey. More info:
	// https://docs.rs/object_store/latest/object_store/aws/enum.AmazonS3ConfigKey.html
	Options []v1.EnvVar `json:"options,omitempty"`
}

// AnonymousResultsABSSpec stores anonymous results in Azure Blob Storage.
type AnonymousResultsABSSpec struct {
	// EndpointURL is the entire Azure Blob Storage URI. If the storage
	// location requires authentication, provide the credentials in options.
	EndpointURL string `json:"endpointURL"`

	// PresignDuration is how long anonymous results are presigned for.
	// Defaults to 8h.
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`

	// Options for the Azure Blob Storage location. These correspond to
	// Object Store's AzureConfigKey. More info:
	// https://docs.rs/object_store/latest/object_store/azure/enum.AzureConfigKey.html
	Options []v1.EnvVar `json:"options,omitempty"`
}

// AnonymousResultsGCSSpec stores anonymous results in Google Cloud Storage.
type AnonymousResultsGCSSpec struct {
	// EndpointURL is the entire Google Cloud Storage URI. If the storage
	// location requires authentication, provide the credentials in options.
	EndpointURL string `json:"endpointURL"`

	// PresignDuration is how long anonymous results are presigned for.
	// Defaults to 8h.
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`

	// Options for the Google Cloud Storage location. These correspond to
	// Object Store's GoogleConfigKey. More info:
	// https://docs.rs/object_store/latest/object_store/gcp/enum.GoogleConfigKey.html
	Options []v1.EnvVar `json:"options,omitempty"`
}

// SharedFilesystemSpec configures a filesystem path on an already-mounted
// shared volume (e.g. an NFS or CSI driver mount supplied via the pod
// template's volumes/volumeMounts).
type SharedFilesystemSpec struct {
	// Path on the shared volume where the data is stored.
	Path string `json:"path"`
}

// CheckpointSpec configures checkpointing of queries.
type CheckpointSpec struct {
	// Period is the period at which checkpoints are created. Once it has
	// passed after a stage completed, a checkpoint is created.
	// +kubebuilder:default="20m"
	// +optional
	Period *metav1.Duration `json:"period,omitempty"`

	// Data is where checkpoint data is stored.
	Data CheckpointDataSpec `json:"data"`
}

// LineageSpec exports query lineage events to an external endpoint.
type LineageSpec struct {
	// Transport is the transport used for lineage export.
	Transport LineageTransportSpec `json:"transport"`
}

// LineageTransportSpec selects the transport used for lineage export.
type LineageTransportSpec struct {
	// HTTP exports lineage events over HTTP.
	// +kubebuilder:validation:Required
	HTTP *LineageTransportHTTPSpec `json:"http"`
}

// LineageTransportHTTPSpec exports lineage events over HTTP.
type LineageTransportHTTPSpec struct {
	// Endpoint is the HTTP endpoint lineage events are sent to.
	Endpoint string `json:"endpoint"`
}

// SchedulerSpec configures the cluster's scheduler pod and its Services.
type SchedulerSpec struct {
	// Services configures the Services exposing the scheduler's ports.
	// +optional
	Services *SchedulerServicesSpec `json:"services,omitempty"`

	// PodTemplate customizes the scheduler pod; it is merged into the pod
	// the operator composes.
	// +optional
	PodTemplate *v1.PodTemplateSpec `json:"podTemplate,omitempty"`
}

// SchedulerServicesSpec configures the Services exposing the scheduler's
// ports.
type SchedulerServicesSpec struct {
	// Scheduler exposes the client-facing scheduler port (5051).
	// +optional
	Scheduler *ServiceConfig `json:"scheduler,omitempty"`

	// Internal exposes the worker-facing scheduler (5050) and observatory
	// gRPC (5049) ports.
	// +optional
	Internal *ServiceConfig `json:"internal,omitempty"`

	// Observatory exposes the observatory dashboard REST port (3001).
	// +optional
	Observatory *ServiceConfig `json:"observatory,omitempty"`
}

// ServiceConfig configures a single Service the operator manages.
type ServiceConfig struct {
	// Type determines how the Service is exposed: ClusterIP, NodePort, or
	// LoadBalancer. More info:
	// https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types
	// +kubebuilder:default="ClusterIP"
	// +optional
	Type *v1.ServiceType `json:"type,omitempty"`

	// Annotations to add to the Service object. Used by some controllers
	// to set up TLS termination or load balancers.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// WorkerPoolDeclaration defines the desired state of a PolarsCluster's worker pool.
type WorkerPoolDeclaration struct {
	// PodTemplate customizes the worker pods; it is merged into the pod
	// the operator composes.
	// +optional
	PodTemplate *v1.PodTemplateSpec `json:"podTemplate,omitempty"`

	// Replicas is the desired number of worker pods.
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`

	// HeartBeatInterval between workers and the scheduler. Defaults to 5s.
	// +optional
	HeartBeatInterval *metav1.Duration `json:"heartBeatInterval,omitempty"`

	// ShuffleData is the ephemeral storage for shuffle data.
	// +optional
	ShuffleData *ShuffleDataSpec `json:"shuffleData,omitempty"`

	// TemporaryData is the ephemeral storage for temporary data used by
	// Polars (e.g. streaming data). Host-local SSD storage is recommended
	// for better performance.
	// +optional
	TemporaryData *TemporaryDataSpec `json:"temporaryData,omitempty"`
}

// HostMetricsSpec controls host metrics collection for the observatory
// dashboard.
type HostMetricsSpec struct {
	// Enabled turns host metrics collection on or off.
	Enabled bool `json:"enabled"`
}

// TemporaryDataSpec configures storage for temporary data.
type TemporaryDataSpec struct {
	// EphemeralVolumeClaim stores temporary data on a per-pod generic
	// ephemeral volume instead of the operator-managed emptyDir.
	// +optional
	EphemeralVolumeClaim *EphemeralVolumeClaimSpec `json:"ephemeralVolumeClaim,omitempty"`
}

// ShuffleDataSpec selects exactly one storage location for shuffle data.
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

	// S3 stores shuffle data in AWS S3.
	// +optional
	S3 *ShuffleDataS3Spec `json:"s3,omitempty"`

	// ABS stores shuffle data in Azure Blob Storage.
	// +optional
	ABS *ShuffleDataABSSpec `json:"abs,omitempty"`

	// GCS stores shuffle data in Google Cloud Storage.
	// +optional
	GCS *ShuffleDataGCSSpec `json:"gcs,omitempty"`
}

// LocalFilesystemSpec configures per-pod local storage at the given path,
// or at an operator-managed emptyDir when path is omitted.
type LocalFilesystemSpec struct {
	// Path on local storage where the data is stored. Must correspond to a
	// volume you mount yourself; when omitted the operator mounts an
	// emptyDir at a default location.
	// +optional
	Path string `json:"path,omitempty"`
}

// EphemeralVolumeClaimSpec configures a per-pod generic ephemeral volume.
type EphemeralVolumeClaimSpec struct {
	// StorageClassName is the name of the StorageClass required by the
	// claim. More info:
	// https://kubernetes.io/docs/concepts/storage/persistent-volumes#class-1
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`

	// Size of the volume requested by the claim. More info:
	// https://kubernetes.io/docs/concepts/storage/persistent-volumes#capacity
	Size resource.Quantity `json:"size"`
}

// ShuffleDataS3Spec stores shuffle data in AWS S3.
type ShuffleDataS3Spec struct {
	// Endpoint is the entire S3 URI. If the storage location requires
	// authentication, provide the credentials in options.
	Endpoint string `json:"endpoint"`

	// PresignDuration is how long shuffle data is presigned for. Defaults
	// to 8h.
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`

	// Options for the S3 storage location. These correspond to Object
	// Store's AmazonS3ConfigKey. More info:
	// https://docs.rs/object_store/latest/object_store/aws/enum.AmazonS3ConfigKey.html
	Options []v1.EnvVar `json:"options,omitempty"`
}

// ShuffleDataABSSpec stores shuffle data in Azure Blob Storage.
type ShuffleDataABSSpec struct {
	// EndpointURL is the entire Azure Blob Storage URI. If the storage
	// location requires authentication, provide the credentials in options.
	EndpointURL string `json:"endpointURL"`

	// PresignDuration is how long shuffle data is presigned for. Defaults
	// to 8h.
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`

	// Options for the Azure Blob Storage location. These correspond to
	// Object Store's AzureConfigKey. More info:
	// https://docs.rs/object_store/latest/object_store/azure/enum.AzureConfigKey.html
	Options []v1.EnvVar `json:"options,omitempty"`
}

// ShuffleDataGCSSpec stores shuffle data in Google Cloud Storage.
type ShuffleDataGCSSpec struct {
	// EndpointURL is the entire Google Cloud Storage URI. If the storage
	// location requires authentication, provide the credentials in options.
	EndpointURL string `json:"endpointURL"`

	// PresignDuration is how long shuffle data is presigned for. Defaults
	// to 8h.
	PresignDuration *metav1.Duration `json:"presignDuration,omitempty"`

	// Options for the Google Cloud Storage location. These correspond to
	// Object Store's GoogleConfigKey. More info:
	// https://docs.rs/object_store/latest/object_store/gcp/enum.GoogleConfigKey.html
	Options []v1.EnvVar `json:"options,omitempty"`
}

// CheckpointDataSpec selects exactly one storage location for checkpoint
// data.
// +kubebuilder:validation:ExactlyOneOf=sharedFilesystem;s3;abs;gcs
type CheckpointDataSpec struct {
	// SharedFilesystem stores checkpoint data on a filesystem path all
	// workers see. Mounting is the user's responsibility.
	// +optional
	SharedFilesystem *SharedFilesystemSpec `json:"sharedFilesystem,omitempty"`

	// S3 stores checkpoint data in AWS S3.
	// +optional
	S3 *CheckpointS3Spec `json:"s3,omitempty"`

	// ABS stores checkpoint data in Azure Blob Storage.
	// +optional
	ABS *CheckpointABSSpec `json:"abs,omitempty"`

	// GCS stores checkpoint data in Google Cloud Storage.
	// +optional
	GCS *CheckpointGCSSpec `json:"gcs,omitempty"`
}

// CheckpointS3Spec stores checkpoint data in AWS S3.
type CheckpointS3Spec struct {
	// Endpoint is the entire S3 URI. If the storage location requires
	// authentication, provide the credentials in options.
	Endpoint string `json:"endpoint"`

	// Options for the S3 storage location. These correspond to Object
	// Store's AmazonS3ConfigKey. More info:
	// https://docs.rs/object_store/latest/object_store/aws/enum.AmazonS3ConfigKey.html
	Options []v1.EnvVar `json:"options,omitempty"`
}

// CheckpointABSSpec stores checkpoint data in Azure Blob Storage.
type CheckpointABSSpec struct {
	// EndpointURL is the entire Azure Blob Storage URI. If the storage
	// location requires authentication, provide the credentials in options.
	EndpointURL string `json:"endpointURL"`

	// Options for the Azure Blob Storage location. These correspond to
	// Object Store's AzureConfigKey. More info:
	// https://docs.rs/object_store/latest/object_store/azure/enum.AzureConfigKey.html
	Options []v1.EnvVar `json:"options,omitempty"`
}

// CheckpointGCSSpec stores checkpoint data in Google Cloud Storage.
type CheckpointGCSSpec struct {
	// EndpointURL is the entire Google Cloud Storage URI. If the storage
	// location requires authentication, provide the credentials in options.
	EndpointURL string `json:"endpointURL"`

	// Options for the Google Cloud Storage location. These correspond to
	// Object Store's GoogleConfigKey. More info:
	// https://docs.rs/object_store/latest/object_store/gcp/enum.GoogleConfigKey.html
	Options []v1.EnvVar `json:"options,omitempty"`
}

// SchedulerStatus is the observed state of a PolarsCluster's scheduler.
type SchedulerStatus struct {
	// Ready reports whether the scheduler pod is Ready.
	Ready bool `json:"ready"`
}

// WorkerPoolStatus is the observed state of a PolarsCluster's worker pool.
type WorkerPoolStatus struct {
	// Replicas is the number of non-terminating worker pods.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas is the number of worker pods that are Ready.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Selector is the label selector matching the pool's worker pods, in
	// string form.
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

	// ObservedGeneration is the most recent spec generation reflected by
	// this status. Clients should treat the status as stale while it trails
	// metadata.generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Scheduler is the observed state of the scheduler.
	// +optional
	Scheduler SchedulerStatus `json:"scheduler,omitempty"`

	// WorkerPool is the observed state of the worker pool.
	// +optional
	WorkerPool WorkerPoolStatus `json:"workerPool,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Whether the scheduler and worker pool are both ready"
// +kubebuilder:printcolumn:name="Scheduler",type=string,JSONPath=".status.conditions[?(@.type=='SchedulerReady')].status",description="Whether the scheduler pod is ready"
// +kubebuilder:printcolumn:name="Workers",type=integer,JSONPath=".spec.workerPool.replicas",description="Desired worker replicas"
// +kubebuilder:printcolumn:name="Available",type=integer,JSONPath=".status.workerPool.readyReplicas",description="Ready worker replicas"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.version",description="Polars on-premises release"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

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
