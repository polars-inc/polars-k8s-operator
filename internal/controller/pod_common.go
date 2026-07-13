package controller

import (
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	computev1 "polars-inc/k8s-operator/api/v1"
)

const (
	appName = "polars"

	defaultDistRepository    = "polarscloud/polars-on-premises"
	defaultRuntimeRepository = "python"
	defaultRuntimeTag        = "3.13.9-slim-bookworm"
	defaultPolarsExtras      = "cloudpickle"

	releaseDataVolumeName     = "release-data"
	releaseDataMountPath      = "/emptydir"
	tmpDataVolumeName         = "tmp-data"
	requirementsPath          = "/emptydir/requirements.txt"
	enterpriseLicenseVolume   = "license"
	enterpriseLicenseMountDir = "/mnt/license/"
	enterpriseLicensePath     = "/mnt/license/license.json"

	defaultHeartBeatInterval = "5s"
	defaultCheckpointPeriod  = "20m"
	defaultMetricsBytesTotal = int64(104857600)
)

// resolveClusterID returns the configured cluster ID or the
// "<namespace>/<name>" default.
func resolveClusterID(cluster *computev1.PolarsCluster) string {
	if cluster.Spec.ClusterID != "" {
		return cluster.Spec.ClusterID
	}
	return fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
}

// resolveClusterDomain returns the cluster DNS domain, defaulting to "cluster.local".
func resolveClusterDomain(cluster *computev1.PolarsCluster) string {
	if cluster.Spec.ClusterDomain != "" {
		return cluster.Spec.ClusterDomain
	}
	return "cluster.local"
}

// resolveLogLevel returns the configured log level, defaulting to "info".
func resolveLogLevel(cluster *computev1.PolarsCluster) string {
	if cluster.Spec.LogLevel != nil {
		return string(*cluster.Spec.LogLevel)
	}
	return "info"
}

// hostMetricsDisabled reports whether host metrics collection was explicitly
// disabled. Mirrors the chart's disableHostMetrics: absent means enabled, and
// the env var this drives is only ever set to signal "disabled".
func hostMetricsDisabled(cluster *computev1.PolarsCluster) bool {
	hm := cluster.Spec.HostMetrics
	return hm != nil && !hm.Enabled
}

// distTag returns the composed runtime's dist tag, or "" when no runtime is configured.
func distTag(cluster *computev1.PolarsCluster) string {
	if cluster.Spec.Runtime == nil {
		return ""
	}
	return cluster.Spec.Runtime.Composed.Dist.Tag
}

// standardLabels returns the app.kubernetes.io labels set on every managed object.
func standardLabels(cluster *computev1.PolarsCluster, component string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       appName,
		"app.kubernetes.io/instance":   cluster.Name,
		"app.kubernetes.io/component":  component,
		"app.kubernetes.io/managed-by": "polars-operator",
	}
	if tag := distTag(cluster); tag != "" {
		labels["app.kubernetes.io/version"] = tag
	}
	return labels
}

// applyPodLabels adds the selector and app.kubernetes.io labels to pod.
func applyPodLabels(pod *corev1.Pod, cluster *computev1.PolarsCluster, component string) {
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	pod.Labels[clusterLabel] = cluster.Name
	pod.Labels[componentLabel] = component
	maps.Copy(pod.Labels, standardLabels(cluster, component))
}

// composedRuntimeConfig builds the init containers, main-container overrides,
// env, and volumes for the composed runtime; zero values when no runtime is
// configured.
func composedRuntimeConfig(cluster *computev1.PolarsCluster) (initContainers []corev1.Container, container corev1.Container, env []corev1.EnvVar, volumes []corev1.Volume, mounts []corev1.VolumeMount) {
	if cluster.Spec.Runtime == nil {
		return nil, corev1.Container{}, nil, nil, nil
	}
	composed := cluster.Spec.Runtime.Composed

	distRepository := composed.Dist.Repository
	if distRepository == "" {
		distRepository = defaultDistRepository
	}
	distPullPolicy := composed.Dist.PullPolicy
	if distPullPolicy == "" {
		distPullPolicy = corev1.PullIfNotPresent
	}

	runtimeRepository, runtimeTag := defaultRuntimeRepository, defaultRuntimeTag
	var runtimePullPolicy corev1.PullPolicy
	if composed.Runtime != nil {
		if composed.Runtime.Repository != "" {
			runtimeRepository = composed.Runtime.Repository
		}
		if composed.Runtime.Tag != "" {
			runtimeTag = composed.Runtime.Tag
		}
		runtimePullPolicy = composed.Runtime.PullPolicy
	}

	extras := composed.PolarsExtras
	if extras == "" {
		extras = defaultPolarsExtras
	}

	initContainers = []corev1.Container{{
		Name:            "release",
		Image:           fmt.Sprintf("%s:%s", distRepository, composed.Dist.Tag),
		ImagePullPolicy: distPullPolicy,
		Command:         []string{"/bin/cp"},
		Args:            []string{"-a", "/opt/.", releaseDataMountPath},
		VolumeMounts: []corev1.VolumeMount{
			{Name: releaseDataVolumeName, MountPath: releaseDataMountPath},
		},
	}}

	container = corev1.Container{
		Image:           fmt.Sprintf("%s:%s", runtimeRepository, runtimeTag),
		ImagePullPolicy: runtimePullPolicy,
		Command:         []string{releaseDataMountPath + "/setup.sh"},
		Args:            []string{releaseDataMountPath + "/bin/pc-cublet", "service"},
		WorkingDir:      "/tmp",
	}

	env = []corev1.EnvVar{
		{Name: "POLARS_EXTRAS", Value: extras},
		{Name: "POLARS_WHL_DIR", Value: releaseDataMountPath + "/whl"},
		{Name: "UV_PATH", Value: releaseDataMountPath + "/bin/uv"},
	}

	volumes = []corev1.Volume{
		{Name: tmpDataVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: releaseDataVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	mounts = []corev1.VolumeMount{
		{Name: tmpDataVolumeName, MountPath: "/tmp"},
		{Name: releaseDataVolumeName, MountPath: releaseDataMountPath},
	}

	if composed.Requirements != "" {
		initContainers = append(initContainers, corev1.Container{
			Name:            "requirements",
			Image:           fmt.Sprintf("%s:%s", runtimeRepository, runtimeTag),
			ImagePullPolicy: runtimePullPolicy,
			Command:         []string{"/bin/sh", "-c", `printf '%s' "$PYTHON_REQUIREMENTS_CONTENT" > ` + requirementsPath},
			Env: []corev1.EnvVar{
				{Name: "PYTHON_REQUIREMENTS_CONTENT", Value: composed.Requirements},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: releaseDataVolumeName, MountPath: releaseDataMountPath},
			},
		})
		env = append(env, corev1.EnvVar{Name: "PYTHON_REQUIREMENTS", Value: requirementsPath})
	}

	return initContainers, container, env, volumes, mounts
}

// enterpriseLicenseConfig returns the Secret volume + mount for the enterprise license file.
func enterpriseLicenseConfig(cluster *computev1.PolarsCluster) ([]corev1.Volume, []corev1.VolumeMount) {
	enterprise := cluster.Spec.License.OnPremEnterprise
	if enterprise == nil {
		return nil, nil
	}

	volumes := []corev1.Volume{{
		Name: enterpriseLicenseVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: enterprise.SecretName,
				Items:      []corev1.KeyToPath{{Key: enterprise.SecretProperty, Path: "license.json"}},
			},
		},
	}}
	mounts := []corev1.VolumeMount{{Name: enterpriseLicenseVolume, MountPath: enterpriseLicenseMountDir}}
	return volumes, mounts
}

// defaultSchedulerReadinessProbe is injected on port when the scheduler's
// first container doesn't define its own. tcpSocket, not grpc: the
// scheduler doesn't serve a gRPC health check on its client-facing port.
func defaultSchedulerReadinessProbe(port int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)},
		},
		InitialDelaySeconds: 1,
		PeriodSeconds:       10,
		FailureThreshold:    25,
	}
}

// defaultWorkerReadinessProbe is the gRPC readiness probe injected on port
// when the worker's first container doesn't define its own.
func defaultWorkerReadinessProbe(port int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			GRPC: &corev1.GRPCAction{Port: port},
		},
		InitialDelaySeconds: 1,
		PeriodSeconds:       10,
		FailureThreshold:    25,
	}
}

// checkpointPeriod returns the checkpoint period, defaulting to 20m.
func checkpointPeriod(cluster *computev1.PolarsCluster) string {
	if cluster.Spec.CheckpointPeriod != nil {
		return cluster.Spec.CheckpointPeriod.Duration.String()
	}
	return defaultCheckpointPeriod
}

func schedulerServiceName(cluster *computev1.PolarsCluster) string {
	return fmt.Sprintf("%s-scheduler", cluster.Name)
}

func schedulerInternalServiceName(cluster *computev1.PolarsCluster) string {
	return fmt.Sprintf("%s-scheduler-internal", cluster.Name)
}

func observatoryServiceName(cluster *computev1.PolarsCluster) string {
	return fmt.Sprintf("%s-observatory", cluster.Name)
}

// pvcRefVolume builds a volume backed by the referenced claim.
func pvcRefVolume(name string, ref *computev1.PersistentVolumeClaimRef) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: ref.ClaimName},
		},
	}
}
