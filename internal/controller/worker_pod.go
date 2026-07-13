package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	computev1 "polars-inc/k8s-operator/api/v1"
)

const (
	shuffleDataVolumeName   = "shuffle-data"
	shuffleDataMountPath    = "/app/shuffle_data"
	temporaryDataVolumeName = "temporary-data"
	temporaryDataMountPath  = "/app/temporary_data"

	workerFlightPortName = "worker-flight"
)

// BuildWorkerPodTemplate builds a worker pod: computed env/volumes/ports are
// strategic-merged onto the first container of workerPool.podTemplate. The
// template is optional when a runtime is configured. Callers set the owner
// reference before creating.
func BuildWorkerPodTemplate(cluster *computev1.PolarsCluster) (corev1.Pod, error) {
	base := cluster.Spec.WorkerPool.PodTemplate

	containerName := componentWorker
	userProbe := false
	var baseContainer corev1.Container
	if len(base.Spec.Containers) > 0 {
		baseContainer = base.Spec.Containers[0]
		containerName = baseContainer.Name
		userProbe = baseContainer.ReadinessProbe != nil
	} else if cluster.Spec.Runtime == nil {
		return corev1.Pod{}, fmt.Errorf("workerPool.podTemplate must include at least one container when no runtime is configured")
	}

	mergedSpec, err := mergePodSpec(base.Spec, computedWorkerPodSpec(cluster, containerName, baseContainer, !userProbe))
	if err != nil {
		return corev1.Pod{}, err
	}

	pod := corev1.Pod{
		ObjectMeta: base.ObjectMeta,
		Spec:       mergedSpec,
	}

	applyPodLabels(&pod, cluster, componentWorker)
	pod.Namespace = cluster.Namespace
	pod.Name = ""
	pod.GenerateName = fmt.Sprintf("%s-worker-", cluster.Name)

	return pod, nil
}

// computedWorkerPodSpec builds the merge patch derived from the cluster spec.
func computedWorkerPodSpec(cluster *computev1.PolarsCluster, containerName string, baseContainer corev1.Container, injectProbe bool) corev1.PodSpec {
	spec := cluster.Spec
	wp := spec.WorkerPool
	internalHostname := fmt.Sprintf("%s.%s.svc.%s", schedulerInternalServiceName(cluster), cluster.Namespace, resolveClusterDomain(cluster))

	b := newEnvBuilder()
	b.String("RUST_BACKTRACE", "full")

	cublet := b.Section("PC_CUBLET")
	monitoring := cublet.Section("monitoring")
	worker := cublet.Section("worker")
	staticLeader := cublet.Section("static_leader")

	monitoring.Bool("enabled", true)
	if hostMetricsDisabled(cluster) {
		monitoring.Section("host_metrics").Bool("enabled", false)
	}
	worker.Bool("enabled", true)
	cublet.String("cluster_id", resolveClusterID(cluster))
	cublet.FieldRef("instance_id", fieldPathMetadataName)
	// A worker will never be a leader, so hardcode this to a no-op value.
	staticLeader.String("leader_instance_id", "not-me")
	staticLeader.String("scheduler_service__public_addr__hostname", internalHostname)
	staticLeader.String("scheduler_service__public_addr__port", "5050")
	staticLeader.String("observatory_service__public_addr__hostname", internalHostname)
	staticLeader.String("observatory_service__public_addr__port", "5049")
	worker.FieldRef("task_service__public_addr__ip", "status.podIP")
	worker.FieldRef("shuffle_service__public_addr__ip", "status.podIP")
	worker.String("heartbeat_period", workerHeartBeatInterval(wp))
	b.String("POLARS_TEMP_DIR", temporaryDataMountPath+"/temporary_data")
	b.String("OTEL_SERVICE_NAME", fmt.Sprintf("%s-worker", cluster.Name))

	if spec.AcceptEula {
		b.String("POLARS_EULA_ACCEPTED", "yes")
	}

	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	license := cublet.Section("license")
	switch {
	case spec.License.OnPrem != nil:
		license.Section("on_prem").Bool("enabled", true)
	case spec.License.OnPremEnterprise != nil:
		license.Section("on_prem_enterprise").String("license_path", enterpriseLicensePath)
		licenseVolumes, licenseMounts := enterpriseLicenseConfig(cluster)
		volumes = append(volumes, licenseVolumes...)
		volumeMounts = append(volumeMounts, licenseMounts...)
	case spec.License.LicenseServer != nil:
		license.Section("license_server").String("address", spec.License.LicenseServer.URI)
	}

	if spec.Telemetry != nil {
		b.ValueOrSource("OTLP_ENDPOINT", spec.Telemetry.OTLPEndpoint)
	}

	b.String("PLC_LOG_LEVEL", resolveLogLevel(cluster))

	if cpu, ok := baseContainer.Resources.Requests[corev1.ResourceCPU]; ok {
		cublet.String("cpu_reserved", cpu.String())
	}

	shuffleVolumes, shuffleMounts, shufflePorts := computedShuffleDataConfig(worker.Section("shuffle_location"), cluster)
	volumes = append(volumes, shuffleVolumes...)
	volumeMounts = append(volumeMounts, shuffleMounts...)

	computedCheckpointDataEnv(worker.Section("checkpoint_location"), cluster)

	initContainers, runtimeContainer, runtimeEnv, runtimeVolumes, runtimeMounts := composedRuntimeConfig(cluster)
	env := append(b.Vars(), runtimeEnv...)
	volumes = append(volumes, runtimeVolumes...)
	volumeMounts = append(volumeMounts, runtimeMounts...)

	ports := append([]corev1.ContainerPort{
		{Name: "worker-service", ContainerPort: 5052, Protocol: corev1.ProtocolTCP},
	}, shufflePorts...)

	volumes = append(volumes, computedTemporaryDataVolume(wp.TemporaryData))
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name: temporaryDataVolumeName, MountPath: temporaryDataMountPath,
	})

	container := runtimeContainer
	container.Name = containerName
	container.Ports = ports
	container.Env = env
	container.VolumeMounts = volumeMounts
	if injectProbe {
		container.ReadinessProbe = defaultWorkerReadinessProbe(5052)
	}

	return corev1.PodSpec{
		ImagePullSecrets: spec.ImagePullSecrets,
		InitContainers:   initContainers,
		Containers:       []corev1.Container{container},
		Volumes:          volumes,
	}
}

func workerHeartBeatInterval(wp computev1.WorkerPoolDeclaration) string {
	if wp.HeartBeatInterval != nil {
		return wp.HeartBeatInterval.Duration.String()
	}
	return defaultHeartBeatInterval
}

// computedShuffleDataConfig appends ShuffleDataSpec's env vars under b
// (scoped to "...__shuffle_location") and returns its volumes/mounts/ports.
// Explicit local/sharedFilesystem paths point at user-mounted storage; only
// the default local case gets an operator-managed emptyDir. The
// worker-flight port is opened for every local case (default and explicit
// path); the chart only opens it for its ephemeralVolumeClaim variant, a
// distinction this CRD no longer has a field for.
func computedShuffleDataConfig(b *envBuilder, cluster *computev1.PolarsCluster) ([]corev1.Volume, []corev1.VolumeMount, []corev1.ContainerPort) {
	shuffleData := cluster.Spec.WorkerPool.ShuffleData

	if shuffleData != nil {
		switch {
		case shuffleData.S3 != nil:
			s3 := b.Section("s3")
			s3.String("url", shuffleData.S3.Endpoint)
			s3.Options(shuffleData.S3.Options)
			return nil, nil, nil

		case shuffleData.ABS != nil:
			abs := b.Section("abs")
			abs.String("url", shuffleData.ABS.EndpointURL)
			abs.Options(shuffleData.ABS.Options)
			return nil, nil, nil

		case shuffleData.GCS != nil:
			gcs := b.Section("gcs")
			gcs.String("url", shuffleData.GCS.EndpointURL)
			gcs.Options(shuffleData.GCS.Options)
			return nil, nil, nil

		case shuffleData.SharedFilesystem != nil:
			b.Section("shared_filesystem").String("path", shuffleData.SharedFilesystem.Path)
			return nil, nil, nil
		}
	}

	ports := []corev1.ContainerPort{{Name: workerFlightPortName, ContainerPort: 5053, Protocol: corev1.ProtocolTCP}}

	if shuffleData != nil && shuffleData.Local != nil && shuffleData.Local.Path != "" {
		b.Section("local").String("path", shuffleData.Local.Path)
		return nil, nil, ports
	}

	b.Section("local").String("path", shuffleDataMountPath+"/shuffle_data")
	volumes := []corev1.Volume{{
		Name:         shuffleDataVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}}
	mounts := []corev1.VolumeMount{{Name: shuffleDataVolumeName, MountPath: shuffleDataMountPath}}
	return volumes, mounts, ports
}

// computedCheckpointDataEnv appends CheckpointDataSpec's env vars under b
// (scoped to "...__checkpoint_location").
func computedCheckpointDataEnv(b *envBuilder, cluster *computev1.PolarsCluster) {
	checkpointData := cluster.Spec.CheckpointData
	if checkpointData == nil {
		return
	}

	switch {
	case checkpointData.S3 != nil:
		s3 := b.Section("s3")
		s3.String("url", checkpointData.S3.Endpoint)
		s3.Options(checkpointData.S3.Options)

	case checkpointData.ABS != nil:
		abs := b.Section("abs")
		abs.String("url", checkpointData.ABS.EndpointURL)
		abs.Options(checkpointData.ABS.Options)

	case checkpointData.GCS != nil:
		gcs := b.Section("gcs")
		gcs.String("url", checkpointData.GCS.EndpointURL)
		gcs.Options(checkpointData.GCS.Options)

	case checkpointData.SharedFilesystem != nil:
		b.Section("shared_filesystem").String("path", checkpointData.SharedFilesystem.Path)
	}
}

// computedTemporaryDataVolume maps TemporaryDataSpec to the "temporary-data" volume.
func computedTemporaryDataVolume(temporaryData *computev1.TemporaryDataSpec) corev1.Volume {
	if temporaryData != nil && temporaryData.EphemeralVolumeClaim != nil {
		return ephemeralClaimVolume(temporaryDataVolumeName, temporaryData.EphemeralVolumeClaim)
	}

	return corev1.Volume{
		Name:         temporaryDataVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
}

// ephemeralClaimVolume builds a generic ephemeral volume.
func ephemeralClaimVolume(name string, evc *computev1.EphemeralVolumeClaimSpec) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Ephemeral: &corev1.EphemeralVolumeSource{
				VolumeClaimTemplate: &corev1.PersistentVolumeClaimTemplate{
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: stringPtrOrNil(evc.StorageClassName),
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceStorage: evc.Size},
						},
					},
				},
			},
		},
	}
}

func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
