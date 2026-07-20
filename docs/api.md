# API Reference

## Packages
- [compute.pola.rs/v1alpha1](#computepolarsv1alpha1)


## compute.pola.rs/v1alpha1

Package v1alpha1 contains API Schema definitions for the compute v1alpha1 API group.

### Resource Types
- [PolarsCluster](#polarscluster)



#### AnonymousResultsABSSpec



AnonymousResultsABSSpec stores anonymous results in Azure Blob Storage.



_Appears in:_
- [AnonymousResultsSpec](#anonymousresultsspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpointURL` _string_ | EndpointURL is the entire Azure Blob Storage URI. If the storage<br />location requires authentication, provide the credentials in options. |  |  |
| `presignDuration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#duration-v1-meta)_ | PresignDuration is how long anonymous results are presigned for.<br />Defaults to 8h. |  |  |
| `options` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#envvar-v1-core) array_ | Options for the Azure Blob Storage location. These correspond to<br />Object Store's AzureConfigKey. More info:<br />https://docs.rs/object_store/latest/object_store/azure/enum.AzureConfigKey.html |  |  |


#### AnonymousResultsGCSSpec



AnonymousResultsGCSSpec stores anonymous results in Google Cloud Storage.



_Appears in:_
- [AnonymousResultsSpec](#anonymousresultsspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpointURL` _string_ | EndpointURL is the entire Google Cloud Storage URI. If the storage<br />location requires authentication, provide the credentials in options. |  |  |
| `presignDuration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#duration-v1-meta)_ | PresignDuration is how long anonymous results are presigned for.<br />Defaults to 8h. |  |  |
| `options` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#envvar-v1-core) array_ | Options for the Google Cloud Storage location. These correspond to<br />Object Store's GoogleConfigKey. More info:<br />https://docs.rs/object_store/latest/object_store/gcp/enum.GoogleConfigKey.html |  |  |


#### AnonymousResultsS3Spec



AnonymousResultsS3Spec stores anonymous results in AWS S3.



_Appears in:_
- [AnonymousResultsSpec](#anonymousresultsspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpoint` _string_ | Endpoint is the entire S3 URI. If the storage location requires<br />authentication, provide the credentials in options. |  |  |
| `presignDuration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#duration-v1-meta)_ | PresignDuration is how long anonymous results are presigned for.<br />Defaults to 8h. |  |  |
| `options` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#envvar-v1-core) array_ | Options for the S3 storage location. These correspond to Object<br />Store's AmazonS3ConfigKey. More info:<br />https://docs.rs/object_store/latest/object_store/aws/enum.AmazonS3ConfigKey.html |  |  |


#### AnonymousResultsSpec



AnonymousResultsSpec selects exactly one storage location for anonymous
results.

_Validation:_
- ExactlyOneOf: [s3 abs gcs sharedFilesystem]

_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `s3` _[AnonymousResultsS3Spec](#anonymousresultss3spec)_ | S3 stores anonymous results in AWS S3. |  | Optional: \{\} <br /> |
| `abs` _[AnonymousResultsABSSpec](#anonymousresultsabsspec)_ | ABS stores anonymous results in Azure Blob Storage. |  | Optional: \{\} <br /> |
| `gcs` _[AnonymousResultsGCSSpec](#anonymousresultsgcsspec)_ | GCS stores anonymous results in Google Cloud Storage. |  | Optional: \{\} <br /> |
| `sharedFilesystem` _[SharedFilesystemSpec](#sharedfilesystemspec)_ | SharedFilesystem stores anonymous results on a filesystem path all<br />workers see. The path must match the location mounted on the client,<br />since the cluster returns the exact written paths back to the client. |  | Optional: \{\} <br /> |


#### CheckpointABSSpec



CheckpointABSSpec stores checkpoint data in Azure Blob Storage.



_Appears in:_
- [CheckpointDataSpec](#checkpointdataspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpointURL` _string_ | EndpointURL is the entire Azure Blob Storage URI. If the storage<br />location requires authentication, provide the credentials in options. |  |  |
| `options` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#envvar-v1-core) array_ | Options for the Azure Blob Storage location. These correspond to<br />Object Store's AzureConfigKey. More info:<br />https://docs.rs/object_store/latest/object_store/azure/enum.AzureConfigKey.html |  |  |


#### CheckpointDataSpec



CheckpointDataSpec selects exactly one storage location for checkpoint
data.

_Validation:_
- ExactlyOneOf: [sharedFilesystem s3 abs gcs]

_Appears in:_
- [CheckpointSpec](#checkpointspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `sharedFilesystem` _[SharedFilesystemSpec](#sharedfilesystemspec)_ | SharedFilesystem stores checkpoint data on a filesystem path all<br />workers see. Mounting is the user's responsibility. |  | Optional: \{\} <br /> |
| `s3` _[CheckpointS3Spec](#checkpoints3spec)_ | S3 stores checkpoint data in AWS S3. |  | Optional: \{\} <br /> |
| `abs` _[CheckpointABSSpec](#checkpointabsspec)_ | ABS stores checkpoint data in Azure Blob Storage. |  | Optional: \{\} <br /> |
| `gcs` _[CheckpointGCSSpec](#checkpointgcsspec)_ | GCS stores checkpoint data in Google Cloud Storage. |  | Optional: \{\} <br /> |


#### CheckpointGCSSpec



CheckpointGCSSpec stores checkpoint data in Google Cloud Storage.



_Appears in:_
- [CheckpointDataSpec](#checkpointdataspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpointURL` _string_ | EndpointURL is the entire Google Cloud Storage URI. If the storage<br />location requires authentication, provide the credentials in options. |  |  |
| `options` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#envvar-v1-core) array_ | Options for the Google Cloud Storage location. These correspond to<br />Object Store's GoogleConfigKey. More info:<br />https://docs.rs/object_store/latest/object_store/gcp/enum.GoogleConfigKey.html |  |  |


#### CheckpointS3Spec



CheckpointS3Spec stores checkpoint data in AWS S3.



_Appears in:_
- [CheckpointDataSpec](#checkpointdataspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpoint` _string_ | Endpoint is the entire S3 URI. If the storage location requires<br />authentication, provide the credentials in options. |  |  |
| `options` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#envvar-v1-core) array_ | Options for the S3 storage location. These correspond to Object<br />Store's AmazonS3ConfigKey. More info:<br />https://docs.rs/object_store/latest/object_store/aws/enum.AmazonS3ConfigKey.html |  |  |


#### CheckpointSpec



CheckpointSpec configures checkpointing of queries.



_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `period` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#duration-v1-meta)_ | Period is the period at which checkpoints are created. Once it has<br />passed after a stage completed, a checkpoint is created. | 20m | Optional: \{\} <br /> |
| `data` _[CheckpointDataSpec](#checkpointdataspec)_ | Data is where checkpoint data is stored. |  | ExactlyOneOf: [sharedFilesystem s3 abs gcs] <br /> |


#### ComposedRuntimeSpec



ComposedRuntimeSpec composes the runtime from the Polars distribution image
(copied into an emptyDir by an init container) executed on a Python base
image.



_Appears in:_
- [RuntimeSpec](#runtimespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dist` _[ImageSpec](#imagespec)_ | Dist is the Polars distribution image. Repository defaults to<br />"polarscloud/polars-on-premises" and tag defaults to spec.version. |  | Optional: \{\} <br /> |
| `runtime` _[ImageSpec](#imagespec)_ | Runtime is the Python base image the distribution runs on. Defaults to<br />"python:3.13.9-slim-bookworm". |  | Optional: \{\} <br /> |
| `requirements` _string_ | Requirements is a requirements.txt to install additional Python<br />packages into the runtime. |  | Optional: \{\} <br /> |
| `polarsExtras` _string_ | PolarsExtras are the Polars pip extras to install. Must include<br />"cloudpickle". Defaults to the full extras set. |  | Optional: \{\} <br /> |


#### EphemeralVolumeClaimSpec



EphemeralVolumeClaimSpec configures a per-pod generic ephemeral volume.



_Appears in:_
- [TemporaryDataSpec](#temporarydataspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageClassName` _string_ | StorageClassName is the name of the StorageClass required by the<br />claim. More info:<br />https://kubernetes.io/docs/concepts/storage/persistent-volumes#class-1 |  | Optional: \{\} <br /> |
| `size` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#quantity-resource-api)_ | Size of the volume requested by the claim. More info:<br />https://kubernetes.io/docs/concepts/storage/persistent-volumes#capacity |  |  |


#### HostMetricsSpec



HostMetricsSpec controls host metrics collection for the observatory
dashboard.



_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ | Enabled turns host metrics collection on or off. |  |  |


#### ImageSpec



ImageSpec identifies a container image.



_Appears in:_
- [ComposedRuntimeSpec](#composedruntimespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `repository` _string_ | Repository is the container image name, without a tag. More info:<br />https://kubernetes.io/docs/concepts/containers/images |  | Optional: \{\} <br /> |
| `tag` _string_ | Tag is the container image tag. |  | Optional: \{\} <br /> |
| `pullPolicy` _[PullPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#pullpolicy-v1-core)_ | PullPolicy is the image pull policy: one of Always, Never, or<br />IfNotPresent. Defaults to Always if the tag is "latest", or<br />IfNotPresent otherwise. More info:<br />https://kubernetes.io/docs/concepts/containers/images#updating-images |  | Optional: \{\} <br /> |


#### LicenseOnPremEnterpriseSpec



LicenseOnPremEnterpriseSpec reads the On-Prem Enterprise license key from
a Secret.



_Appears in:_
- [LicenseSpec](#licensespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretName` _string_ | SecretName is the name of the Secret containing the license key. |  |  |
| `secretProperty` _string_ | SecretProperty is the key on the Secret containing the license key. |  |  |


#### LicenseOnPremSpec



LicenseOnPremSpec licenses the cluster with Polars workspace client
credentials.



_Appears in:_
- [LicenseSpec](#licensespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clientID` _[ValueOrSource](#valueorsource)_ | ClientID of the Polars workspace. |  | ExactlyOneOf: [value valueFrom] <br /> |
| `clientSecret` _[ValueOrSource](#valueorsource)_ | ClientSecret of the Polars workspace. |  | ExactlyOneOf: [value valueFrom] <br /> |
| `workspaceID` _[ValueOrSource](#valueorsource)_ | WorkspaceID of the Polars workspace. |  | ExactlyOneOf: [value valueFrom] <br /> |
| `licenseData` _[PersistentVolumeClaimRef](#persistentvolumeclaimref)_ | LicenseData persists the license certificate in an existing<br />PersistentVolumeClaim instead of an emptyDir, so it survives scheduler<br />pod restarts. |  | Optional: \{\} <br /> |


#### LicenseSpec



LicenseSpec selects exactly one way the cluster's Polars license is
provided.

_Validation:_
- ExactlyOneOf: [onPrem onPremEnterprise]

_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `onPrem` _[LicenseOnPremSpec](#licenseonpremspec)_ | OnPrem licenses the cluster with Polars workspace client credentials. |  | Optional: \{\} <br /> |
| `onPremEnterprise` _[LicenseOnPremEnterpriseSpec](#licenseonprementerprisespec)_ | OnPremEnterprise licenses the cluster from a Secret holding an<br />On-Prem Enterprise license key; requires acceptEula to be true. |  | Optional: \{\} <br /> |


#### LineageSpec



LineageSpec exports query lineage events to an external endpoint.



_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `transport` _[LineageTransportSpec](#lineagetransportspec)_ | Transport is the transport used for lineage export. |  |  |


#### LineageTransportHTTPSpec



LineageTransportHTTPSpec exports lineage events over HTTP.



_Appears in:_
- [LineageTransportSpec](#lineagetransportspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpoint` _string_ | Endpoint is the HTTP endpoint lineage events are sent to. |  |  |


#### LineageTransportSpec



LineageTransportSpec selects the transport used for lineage export.



_Appears in:_
- [LineageSpec](#lineagespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `http` _[LineageTransportHTTPSpec](#lineagetransporthttpspec)_ | HTTP exports lineage events over HTTP. |  | Required: \{\} <br /> |


#### LocalFilesystemSpec



LocalFilesystemSpec configures per-pod local storage at the given path,
or at an operator-managed emptyDir when path is omitted.



_Appears in:_
- [ShuffleDataSpec](#shuffledataspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `path` _string_ | Path on local storage where the data is stored. Must correspond to a<br />volume you mount yourself; when omitted the operator mounts an<br />emptyDir at a default location. |  | Optional: \{\} <br /> |


#### LogLevel

_Underlying type:_ _string_

LogLevel is a log verbosity level: off, error, warn, info, debug, or
trace.

_Validation:_
- Enum: [off error warn info debug trace]

_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description |
| --- | --- |
| `off` |  |
| `error` |  |
| `warn` |  |
| `info` |  |
| `debug` |  |
| `trace` |  |


#### ObservatorySpec



ObservatorySpec configures the observatory dashboard.



_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `maxMetricsBytesTotal` _integer_ | MaxMetricsBytesTotal is the maximum number of bytes for host metrics storage. | 104857600 | Optional: \{\} <br /> |
| `persistentVolumeClaim` _[PersistentVolumeClaimRef](#persistentvolumeclaimref)_ | PersistentVolumeClaim persists the observatory database in an existing<br />claim instead of an emptyDir. |  | Optional: \{\} <br /> |


#### PersistentVolumeClaimRef



PersistentVolumeClaimRef references an existing PersistentVolumeClaim.
The operator never creates claims itself — provision the PVC separately
and point this at it.



_Appears in:_
- [LicenseOnPremSpec](#licenseonpremspec)
- [ObservatorySpec](#observatoryspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `claimName` _string_ | ClaimName is the name of the existing PersistentVolumeClaim. |  |  |


#### PolarsCluster



PolarsCluster is the Schema for the polarsclusters API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `compute.pola.rs/v1alpha1` | | |
| `kind` _string_ | `PolarsCluster` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[PolarsClusterSpec](#polarsclusterspec)_ | spec defines the desired state of PolarsCluster |  | Required: \{\} <br /> |
| `status` _[PolarsClusterStatus](#polarsclusterstatus)_ | status defines the observed state of PolarsCluster |  | Optional: \{\} <br /> |


#### PolarsClusterSpec



PolarsClusterSpec defines the desired state of PolarsCluster



_Appears in:_
- [PolarsCluster](#polarscluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `telemetry` _[TelemetrySpec](#telemetryspec)_ | Telemetry configures exporting the cluster's OTLP traces and metrics. |  | Optional: \{\} <br /> |
| `logLevel` _[LogLevel](#loglevel)_ | LogLevel is the log verbosity of the scheduler and workers. | info | Enum: [off error warn info debug trace] <br />Optional: \{\} <br /> |
| `imagePullSecrets` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#localobjectreference-v1-core) array_ | ImagePullSecrets are references to Secrets in the same namespace to<br />use for pulling any of the cluster's images. |  | Optional: \{\} <br /> |
| `clusterID` _string_ | ClusterID uniquely identifies the Polars cluster in a multi-tenant<br />environment. Defaults to "<namespace>/<name>". |  | Optional: \{\} <br /> |
| `acceptEula` _boolean_ | AcceptEula must be set to true to use the On-Prem Enterprise license. | false | Optional: \{\} <br /> |
| `version` _string_ | Version is the Polars on-premises release to run, as a semantic<br />version. The operator enforces its minimum supported release at<br />reconcile time. Version is used as the composed runtime's dist tag<br />unless runtime.composed.dist.tag overrides it; use that override for<br />non-release image tags. | 0.6.3 | MaxLength: 63 <br />Pattern: `^(0\|[1-9]\d*)\.(0\|[1-9]\d*)\.(0\|[1-9]\d*)(?:-((?:0\|[1-9]\d*\|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0\|[1-9]\d*\|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$` <br />Optional: \{\} <br /> |
| `runtime` _[RuntimeSpec](#runtimespec)_ | Runtime composes the scheduler/worker containers from the Polars<br />distribution and a Python base image. When nil, the pod templates must<br />bring their own image. |  | Optional: \{\} <br /> |
| `license` _[LicenseSpec](#licensespec)_ | License selects how the cluster's Polars license is provided. |  | ExactlyOneOf: [onPrem onPremEnterprise] <br /> |
| `allowLocalSinks` _boolean_ | AllowLocalSinks permits workers to write query results to local disk.<br />Disabling this prevents all local writes; it is not possible to allow<br />only specific sink locations. Users can alternatively configure sinks<br />that write to object storage. More info:<br />https://docs.pola.rs/user-guide/io/cloud-storage/#writing-to-cloud-storage | true |  |
| `allowLocalScans` _boolean_ | AllowLocalScans permits workers to read query inputs from local disk.<br />Disabling this prevents all local reads; it is not possible to allow<br />only specific scan locations. Users can alternatively configure scans<br />that read from object storage. More info:<br />https://docs.pola.rs/user-guide/io/cloud-storage/#reading-from-cloud-storage | false |  |
| `allowAnonymousUsers` _boolean_ | AllowAnonymousUsers permits queries without a username. When false,<br />all queries must be sent with a set username. | true |  |
| `anonymousResults` _[AnonymousResultsSpec](#anonymousresultsspec)_ | AnonymousResults is where results of queries that don't specify a<br />result location are stored. Object storage is recommended so results<br />persist; the compute plane never cleans up anonymous results itself. |  | ExactlyOneOf: [s3 abs gcs sharedFilesystem] <br />Optional: \{\} <br /> |
| `checkpoint` _[CheckpointSpec](#checkpointspec)_ | Checkpoint enables checkpointing of queries: the scheduler<br />periodically checkpoints completed stages to the configured data<br />location so queries can resume after failures. |  | Optional: \{\} <br /> |
| `lineage` _[LineageSpec](#lineagespec)_ | Lineage enables exporting query lineage events to an external<br />endpoint. |  | Optional: \{\} <br /> |
| `observatory` _[ObservatorySpec](#observatoryspec)_ | Observatory configures the observatory dashboard's metrics storage. |  | Optional: \{\} <br /> |
| `hostMetrics` _[HostMetricsSpec](#hostmetricsspec)_ | HostMetrics controls host metrics collection on both the scheduler and<br />worker pods. Defaults to enabled when unset. |  | Optional: \{\} <br /> |
| `scheduler` _[SchedulerSpec](#schedulerspec)_ | Scheduler configures the cluster's scheduler pod and Services; all of<br />it is optional when a runtime composes the scheduler container. |  | Optional: \{\} <br /> |
| `workerPool` _[WorkerPoolDeclaration](#workerpooldeclaration)_ | WorkerPool declares the cluster's pool of worker pods. |  |  |


#### PolarsClusterStatus



PolarsClusterStatus defines the observed state of PolarsCluster.



_Appears in:_
- [PolarsCluster](#polarscluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#condition-v1-meta) array_ | conditions represent the current state of the PolarsCluster resource.<br />Each condition has a unique type and reflects the status of a specific aspect of the resource.<br />Standard condition types include:<br />- "Available": the resource is fully functional<br />- "Progressing": the resource is being created or updated<br />- "Degraded": the resource failed to reach or maintain its desired state<br />The status of each condition is one of True, False, or Unknown. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration is the most recent spec generation reflected by<br />this status. Clients should treat the status as stale while it trails<br />metadata.generation. |  | Optional: \{\} <br /> |
| `scheduler` _[SchedulerStatus](#schedulerstatus)_ | Scheduler is the observed state of the scheduler. |  | Optional: \{\} <br /> |
| `workerPool` _[WorkerPoolStatus](#workerpoolstatus)_ | WorkerPool is the observed state of the worker pool. |  | Optional: \{\} <br /> |


#### RuntimeSpec



RuntimeSpec selects how the Polars runtime is provided.



_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `composed` _[ComposedRuntimeSpec](#composedruntimespec)_ | Composed builds the scheduler and worker containers from the Polars<br />distribution image and a Python base image. |  |  |


#### SchedulerServicesSpec



SchedulerServicesSpec configures the Services exposing the scheduler's
ports.



_Appears in:_
- [SchedulerSpec](#schedulerspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `scheduler` _[ServiceConfig](#serviceconfig)_ | Scheduler exposes the client-facing scheduler port (5051). |  | Optional: \{\} <br /> |
| `internal` _[ServiceConfig](#serviceconfig)_ | Internal exposes the worker-facing scheduler (5050) and observatory<br />gRPC (5049) ports. |  | Optional: \{\} <br /> |
| `observatory` _[ServiceConfig](#serviceconfig)_ | Observatory exposes the observatory dashboard REST port (3001). |  | Optional: \{\} <br /> |


#### SchedulerSpec



SchedulerSpec configures the cluster's scheduler pod and its Services.



_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `services` _[SchedulerServicesSpec](#schedulerservicesspec)_ | Services configures the Services exposing the scheduler's ports. |  | Optional: \{\} <br /> |
| `podTemplate` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#podtemplatespec-v1-core)_ | PodTemplate customizes the scheduler pod; it is merged into the pod<br />the operator composes. |  | Optional: \{\} <br /> |


#### SchedulerStatus



SchedulerStatus is the observed state of a PolarsCluster's scheduler.



_Appears in:_
- [PolarsClusterStatus](#polarsclusterstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready reports whether the scheduler pod is Ready. |  |  |


#### ServiceConfig



ServiceConfig configures a single Service the operator manages.



_Appears in:_
- [SchedulerServicesSpec](#schedulerservicesspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[ServiceType](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#servicetype-v1-core)_ | Type determines how the Service is exposed: ClusterIP, NodePort, or<br />LoadBalancer. More info:<br />https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types | ClusterIP | Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ | Annotations to add to the Service object. Used by some controllers<br />to set up TLS termination or load balancers. |  | Optional: \{\} <br /> |


#### SharedFilesystemSpec



SharedFilesystemSpec configures a filesystem path on an already-mounted
shared volume (e.g. an NFS or CSI driver mount supplied via the pod
template's volumes/volumeMounts).



_Appears in:_
- [AnonymousResultsSpec](#anonymousresultsspec)
- [CheckpointDataSpec](#checkpointdataspec)
- [ShuffleDataSpec](#shuffledataspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `path` _string_ | Path on the shared volume where the data is stored. |  |  |


#### ShuffleDataABSSpec



ShuffleDataABSSpec stores shuffle data in Azure Blob Storage.



_Appears in:_
- [ShuffleDataSpec](#shuffledataspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpointURL` _string_ | EndpointURL is the entire Azure Blob Storage URI. If the storage<br />location requires authentication, provide the credentials in options. |  |  |
| `presignDuration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#duration-v1-meta)_ | PresignDuration is how long shuffle data is presigned for. Defaults<br />to 8h. |  |  |
| `options` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#envvar-v1-core) array_ | Options for the Azure Blob Storage location. These correspond to<br />Object Store's AzureConfigKey. More info:<br />https://docs.rs/object_store/latest/object_store/azure/enum.AzureConfigKey.html |  |  |


#### ShuffleDataGCSSpec



ShuffleDataGCSSpec stores shuffle data in Google Cloud Storage.



_Appears in:_
- [ShuffleDataSpec](#shuffledataspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpointURL` _string_ | EndpointURL is the entire Google Cloud Storage URI. If the storage<br />location requires authentication, provide the credentials in options. |  |  |
| `presignDuration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#duration-v1-meta)_ | PresignDuration is how long shuffle data is presigned for. Defaults<br />to 8h. |  |  |
| `options` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#envvar-v1-core) array_ | Options for the Google Cloud Storage location. These correspond to<br />Object Store's GoogleConfigKey. More info:<br />https://docs.rs/object_store/latest/object_store/gcp/enum.GoogleConfigKey.html |  |  |


#### ShuffleDataS3Spec



ShuffleDataS3Spec stores shuffle data in AWS S3.



_Appears in:_
- [ShuffleDataSpec](#shuffledataspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpoint` _string_ | Endpoint is the entire S3 URI. If the storage location requires<br />authentication, provide the credentials in options. |  |  |
| `presignDuration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#duration-v1-meta)_ | PresignDuration is how long shuffle data is presigned for. Defaults<br />to 8h. |  |  |
| `options` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#envvar-v1-core) array_ | Options for the S3 storage location. These correspond to Object<br />Store's AmazonS3ConfigKey. More info:<br />https://docs.rs/object_store/latest/object_store/aws/enum.AmazonS3ConfigKey.html |  |  |


#### ShuffleDataSpec



ShuffleDataSpec selects exactly one storage location for shuffle data.

_Validation:_
- ExactlyOneOf: [local sharedFilesystem s3 abs gcs]

_Appears in:_
- [WorkerPoolDeclaration](#workerpooldeclaration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `local` _[LocalFilesystemSpec](#localfilesystemspec)_ | Local stores shuffle data on per-worker local storage. When path is<br />omitted the operator mounts an emptyDir at a default location; set<br />path to use storage you mount yourself (via the pod template's<br />volumes/volumeMounts, or volumes injected at admission time). |  | Optional: \{\} <br /> |
| `sharedFilesystem` _[SharedFilesystemSpec](#sharedfilesystemspec)_ | SharedFilesystem stores shuffle data on a filesystem path all workers<br />see (e.g. an NFS or CSI mount). Mounting is the user's responsibility. |  | Optional: \{\} <br /> |
| `s3` _[ShuffleDataS3Spec](#shuffledatas3spec)_ | S3 stores shuffle data in AWS S3. |  | Optional: \{\} <br /> |
| `abs` _[ShuffleDataABSSpec](#shuffledataabsspec)_ | ABS stores shuffle data in Azure Blob Storage. |  | Optional: \{\} <br /> |
| `gcs` _[ShuffleDataGCSSpec](#shuffledatagcsspec)_ | GCS stores shuffle data in Google Cloud Storage. |  | Optional: \{\} <br /> |


#### TelemetrySpec



TelemetrySpec configures exporting the cluster's telemetry.



_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `otlpEndpoint` _[ValueOrSource](#valueorsource)_ | OTLPEndpoint is the endpoint to send OTLP traces and metrics to. |  | ExactlyOneOf: [value valueFrom] <br /> |


#### TemporaryDataSpec



TemporaryDataSpec configures storage for temporary data.



_Appears in:_
- [WorkerPoolDeclaration](#workerpooldeclaration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ephemeralVolumeClaim` _[EphemeralVolumeClaimSpec](#ephemeralvolumeclaimspec)_ | EphemeralVolumeClaim stores temporary data on a per-pod generic<br />ephemeral volume instead of the operator-managed emptyDir. |  | Optional: \{\} <br /> |


#### ValueOrSource



ValueOrSource holds either a literal value or a reference to where the
value can be found (Secret key, ConfigMap key, field, ...).

_Validation:_
- ExactlyOneOf: [value valueFrom]

_Appears in:_
- [LicenseOnPremSpec](#licenseonpremspec)
- [TelemetrySpec](#telemetryspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `value` _string_ | Value is the literal value. |  | Optional: \{\} <br /> |
| `valueFrom` _[EnvVarSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#envvarsource-v1-core)_ | ValueFrom references the value from a Secret key, ConfigMap key, pod<br />field, or container resource. |  | Optional: \{\} <br /> |


#### WorkerPoolDeclaration



WorkerPoolDeclaration defines the desired state of a PolarsCluster's worker pool.



_Appears in:_
- [PolarsClusterSpec](#polarsclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `podTemplate` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#podtemplatespec-v1-core)_ | PodTemplate customizes the worker pods; it is merged into the pod<br />the operator composes. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the desired number of worker pods. |  | Minimum: 0 <br /> |
| `heartBeatInterval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#duration-v1-meta)_ | HeartBeatInterval between workers and the scheduler. Defaults to 5s. |  | Optional: \{\} <br /> |
| `shuffleData` _[ShuffleDataSpec](#shuffledataspec)_ | ShuffleData is the ephemeral storage for shuffle data. |  | ExactlyOneOf: [local sharedFilesystem s3 abs gcs] <br />Optional: \{\} <br /> |
| `temporaryData` _[TemporaryDataSpec](#temporarydataspec)_ | TemporaryData is the ephemeral storage for temporary data used by<br />Polars (e.g. streaming data). Host-local SSD storage is recommended<br />for better performance. |  | Optional: \{\} <br /> |


#### WorkerPoolStatus



WorkerPoolStatus is the observed state of a PolarsCluster's worker pool.



_Appears in:_
- [PolarsClusterStatus](#polarsclusterstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `replicas` _integer_ | Replicas is the number of non-terminating worker pods. |  | Optional: \{\} <br /> |
| `readyReplicas` _integer_ | ReadyReplicas is the number of worker pods that are Ready. |  | Optional: \{\} <br /> |
| `selector` _string_ | Selector is the label selector matching the pool's worker pods, in<br />string form. |  | Optional: \{\} <br /> |


