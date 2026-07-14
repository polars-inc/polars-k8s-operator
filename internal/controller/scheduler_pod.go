package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	computev1 "polars-inc/k8s-operator/api/v1"
)

const (
	envValueTrue = "true"

	licenseDataVolumeName = "license-data"
	licenseDataMountPath  = "/mnt/license_data"

	observatoryDataVolumeName = "observatory-data"
	observatoryDataMountPath  = "/app/observatory_data"

	// fieldPathMetadataName is shared with worker_pod.go's downward-API env vars.
	fieldPathMetadataName = "metadata.name"
)

// BuildSchedulerPodTemplate builds the scheduler pod: computed env/volumes/
// ports are strategic-merged onto the first container of
// scheduler.podTemplate. The template is optional when a runtime is
// configured. Callers set the owner reference before creating.
func BuildSchedulerPodTemplate(cluster *computev1.PolarsCluster) (corev1.Pod, error) {
	var base corev1.PodTemplateSpec
	if s := cluster.Spec.Scheduler; s != nil && s.PodTemplate != nil {
		base = *s.PodTemplate
	}

	containerName := componentScheduler
	userProbe := false
	if len(base.Spec.Containers) > 0 {
		containerName = base.Spec.Containers[0].Name
		userProbe = base.Spec.Containers[0].ReadinessProbe != nil
	} else if cluster.Spec.Runtime == nil {
		return corev1.Pod{}, fmt.Errorf("scheduler.podTemplate must include at least one container when no runtime is configured")
	}

	mergedSpec, err := mergePodSpec(base.Spec, computedSchedulerPodSpec(cluster, containerName, !userProbe))
	if err != nil {
		return corev1.Pod{}, err
	}

	pod := corev1.Pod{
		ObjectMeta: base.ObjectMeta,
		Spec:       mergedSpec,
	}

	applyPodLabels(&pod, cluster, componentScheduler)
	pod.Namespace = cluster.Namespace
	pod.Name = schedulerPodName(cluster)

	return pod, nil
}

// computedSchedulerPodSpec builds the merge patch derived from the cluster spec.
func computedSchedulerPodSpec(cluster *computev1.PolarsCluster, containerName string, injectProbe bool) corev1.PodSpec {
	spec := cluster.Spec

	b := newEnvBuilder()
	b.String("RUST_BACKTRACE", "full")

	cublet := b.Section("PC_CUBLET")
	scheduler := cublet.Section("scheduler")
	staticLeader := cublet.Section("static_leader")
	observatory := cublet.Section("observatory")

	scheduler.Bool("enabled", true)
	cublet.String("cluster_id", resolveClusterID(cluster))
	cublet.FieldRef("instance_id", fieldPathMetadataName)
	// A scheduler will always be a leader, so hardcode this to equal instance_id
	staticLeader.FieldRef("leader_instance_id", fieldPathMetadataName)
	staticLeader.String("scheduler_service__public_addr__ip", "127.0.0.1")
	staticLeader.String("scheduler_service__public_addr__port", "5050")
	staticLeader.String("observatory_service__public_addr__ip", "127.0.0.1")
	staticLeader.String("observatory_service__public_addr__port", "5049")

	observatory.Bool("enabled", true)
	observatory.Int("max_metrics_bytes_total", observatoryMaxMetricsBytes(cluster))
	observatory.String("database_path", observatoryDataMountPath+"/observatory.db")
	observatory.String("cluster_mode", "kubernetes")
	observatory.Section("rest_api").Bool("enabled", true)

	monitoring := cublet.Section("monitoring")
	monitoring.Bool("enabled", true)
	if hostMetricsDisabled(cluster) {
		monitoring.Section("host_metrics").Bool("enabled", false)
	}
	scheduler.Bool("allow_local_sinks", spec.AllowLocalSinks)
	scheduler.Bool("allow_local_scans", spec.AllowLocalScans)
	scheduler.Bool("deny_anonymous_users", !spec.AllowAnonymousUsers)
	b.String("OTEL_SERVICE_NAME", schedulerServiceName(cluster))

	if spec.AcceptEula {
		b.String("POLARS_EULA_ACCEPTED", "yes")
	}

	licenseVolumes, licenseMounts := schedulerLicenseConfig(cublet, cluster)

	volumes := append([]corev1.Volume{observatoryDataVolume(cluster)}, licenseVolumes...)
	volumeMounts := append([]corev1.VolumeMount{
		{Name: observatoryDataVolumeName, MountPath: observatoryDataMountPath},
	}, licenseMounts...)

	if spec.RequireFreeWorkers != nil {
		count := uint32(spec.WorkerPool.Replicas) //nolint:gosec // replicas is CEL- and marker-validated >= 0
		if spec.RequireFreeWorkers.Count != nil {
			count = *spec.RequireFreeWorkers.Count
		}
		scheduler.Int("n_workers", int64(count))
	}

	computedAnonymousResultsEnv(scheduler.Section("anonymous_result_location"), spec.AnonymousResults)

	if spec.Checkpoint != nil {
		checkpoint := scheduler.Section("checkpoint")
		checkpoint.Bool("enabled", true)
		checkpoint.String("period", checkpointPeriod(*spec.Checkpoint))
	}

	if spec.Lineage != nil {
		lineage := cublet.Section("lineage")
		lineage.Bool("enabled", true)
		if http := spec.Lineage.Transport.HTTP; http != nil {
			lineage.String("transport__http__endpoint", http.Endpoint)
		}
	}

	if spec.Telemetry != nil {
		b.ValueOrSource("OTLP_ENDPOINT", spec.Telemetry.OTLPEndpoint)
	}

	b.String("PLC_LOG_LEVEL", resolveLogLevel(cluster))

	initContainers, runtimeContainer, runtimeEnv, runtimeVolumes, runtimeMounts := composedRuntimeConfig(cluster)
	env := append(b.Vars(), runtimeEnv...)
	volumes = append(volumes, runtimeVolumes...)
	volumeMounts = append(volumeMounts, runtimeMounts...)

	container := runtimeContainer
	container.Name = containerName
	container.Ports = []corev1.ContainerPort{
		{Name: "sched", ContainerPort: 5051, Protocol: corev1.ProtocolTCP},
		{Name: "worker-sched", ContainerPort: 5050, Protocol: corev1.ProtocolTCP},
		{Name: "obser-grpc", ContainerPort: 5049, Protocol: corev1.ProtocolTCP},
		{Name: "obser-rest", ContainerPort: 3001, Protocol: corev1.ProtocolTCP},
	}
	container.Env = env
	container.VolumeMounts = volumeMounts
	if injectProbe {
		container.ReadinessProbe = defaultSchedulerReadinessProbe(5051)
	}

	return corev1.PodSpec{
		ImagePullSecrets: spec.ImagePullSecrets,
		InitContainers:   initContainers,
		Containers:       []corev1.Container{container},
		Volumes:          volumes,
	}
}

// schedulerLicenseConfig maps the license config to scheduler env (appended
// to cublet) and volumes/mounts.
func schedulerLicenseConfig(cublet *envBuilder, cluster *computev1.PolarsCluster) ([]corev1.Volume, []corev1.VolumeMount) {
	licenseSpec := cluster.Spec.License
	license := cublet.Section("license")

	switch {
	case licenseSpec.OnPrem != nil:
		onPrem := licenseSpec.OnPrem
		onPremEnv := license.Section("on_prem")
		onPremEnv.ValueOrSource("client_id", onPrem.ClientID)
		onPremEnv.ValueOrSource("client_secret", onPrem.ClientSecret)
		onPremEnv.ValueOrSource("workspace_id", onPrem.WorkspaceID)
		onPremEnv.String("cert_dir", licenseDataMountPath)

		volume := corev1.Volume{
			Name:         licenseDataVolumeName,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		}
		if onPrem.LicenseData != nil {
			volume = pvcRefVolume(licenseDataVolumeName, onPrem.LicenseData)
		}

		mounts := []corev1.VolumeMount{
			{Name: licenseDataVolumeName, MountPath: licenseDataMountPath},
		}
		return []corev1.Volume{volume}, mounts

	case licenseSpec.OnPremEnterprise != nil:
		license.Section("on_prem_enterprise").String("license_path", enterpriseLicensePath)
		return enterpriseLicenseConfig(cluster)

	case licenseSpec.LicenseServer != nil:
		license.Section("license_server").String("uri", licenseSpec.LicenseServer.URI)
		return nil, nil
	}

	return nil, nil
}

// observatoryDataVolume backs /app/observatory_data with the referenced PVC
// when configured, else an emptyDir.
func observatoryDataVolume(cluster *computev1.PolarsCluster) corev1.Volume {
	if o := cluster.Spec.Observatory; o != nil && o.PersistentVolumeClaim != nil {
		return pvcRefVolume(observatoryDataVolumeName, o.PersistentVolumeClaim)
	}
	return corev1.Volume{
		Name:         observatoryDataVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
}

func observatoryMaxMetricsBytes(cluster *computev1.PolarsCluster) int64 {
	if o := cluster.Spec.Observatory; o != nil && o.MaxMetricsBytesTotal != 0 {
		return o.MaxMetricsBytesTotal
	}
	return defaultMetricsBytesTotal
}

// computedAnonymousResultsEnv appends AnonymousResultsSpec's env vars under b
// (scoped to "...__anonymous_result_location").
func computedAnonymousResultsEnv(b *envBuilder, anonymousResults *computev1.AnonymousResultsSpec) {
	if anonymousResults == nil {
		return
	}

	switch {
	case anonymousResults.S3 != nil:
		s3 := b.Section("s3")
		s3.String("url", anonymousResults.S3.Endpoint)
		if d := anonymousResults.S3.PresignDuration; d != nil {
			s3.Duration("presign_duration", d.Duration)
		}
		s3.Options(anonymousResults.S3.Options)

	case anonymousResults.ABS != nil:
		abs := b.Section("abs")
		abs.String("url", anonymousResults.ABS.EndpointURL)
		if d := anonymousResults.ABS.PresignDuration; d != nil {
			abs.Duration("presign_duration", d.Duration)
		}
		abs.Options(anonymousResults.ABS.Options)

	case anonymousResults.GCS != nil:
		gcs := b.Section("gcs")
		gcs.String("url", anonymousResults.GCS.EndpointURL)
		if d := anonymousResults.GCS.PresignDuration; d != nil {
			gcs.Duration("presign_duration", d.Duration)
		}
		gcs.Options(anonymousResults.GCS.Options)

	case anonymousResults.SharedFilesystem != nil:
		b.Section("local").String("path", anonymousResults.SharedFilesystem.Path)
	}
}
