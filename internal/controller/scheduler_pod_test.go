package controller

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	computev1 "polars-inc/k8s-operator/api/v1"
)

const (
	// not "scheduler"/"worker": the builders target the first container regardless of name
	testMainContainerName = "app"

	testClusterName      = "analytics"
	testClusterNamespace = "polars"

	testCustomMountName   = "custom-mount"
	testCustomMountPath   = "/custom"
	testSidecarName       = "sidecar"
	testOptionRegion      = "region"
	testOptionRegionValue = "us-east-1"
)

func schedulerCluster(extra corev1.Container) *computev1.PolarsCluster {
	extra.Name = testMainContainerName
	return &computev1.PolarsCluster{
		ObjectMeta: metav1.ObjectMeta{Name: testClusterName, Namespace: testClusterNamespace},
		Spec: computev1.PolarsClusterSpec{
			SchedulerSpec: computev1.SchedulerSpec{
				PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{extra},
					},
				},
			},
		},
	}
}

func findEnv(env []corev1.EnvVar, name string) (corev1.EnvVar, bool) {
	for _, e := range env {
		if e.Name == name {
			return e, true
		}
	}
	return corev1.EnvVar{}, false
}

func findVolume(volumes []corev1.Volume, name string) (corev1.Volume, bool) {
	for _, v := range volumes {
		if v.Name == name {
			return v, true
		}
	}
	return corev1.Volume{}, false
}

func TestBuildSchedulerPodTemplate_LicenseOnPrem(t *testing.T) {
	g := NewWithT(t)

	cluster := schedulerCluster(corev1.Container{})
	cluster.Spec.License.OnPrem = &computev1.LicenseOnPremSpec{
		ClientID:     computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Key: "client-id"}}},
		ClientSecret: computev1.ValueOrSource{Value: "literal-secret"},
		WorkspaceID:  computev1.ValueOrSource{Value: "workspace-1"},
	}

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	scheduler := result.Spec.Containers[0]

	clientID, ok := findEnv(scheduler.Env, "PC_CUBLET__license__on_prem__client_id")
	g.Expect(ok).To(BeTrue())
	g.Expect(clientID.ValueFrom.SecretKeyRef.Key).To(Equal("client-id"))

	clientSecret, ok := findEnv(scheduler.Env, "PC_CUBLET__license__on_prem__client_secret")
	g.Expect(ok).To(BeTrue())
	g.Expect(clientSecret.Value).To(Equal("literal-secret"))
	g.Expect(clientSecret.ValueFrom).To(BeNil())

	certDir, ok := findEnv(scheduler.Env, "PC_CUBLET__license__on_prem__cert_dir")
	g.Expect(ok).To(BeTrue())
	g.Expect(certDir.Value).To(Equal(licenseDataMountPath))

	_, ok = findVolume(result.Spec.Volumes, licenseDataVolumeName)
	g.Expect(ok).To(BeTrue())

	g.Expect(scheduler.VolumeMounts).To(ContainElement(corev1.VolumeMount{
		Name:      licenseDataVolumeName,
		MountPath: licenseDataMountPath,
	}))
}

func TestBuildSchedulerPodTemplate_PreservesUserFields(t *testing.T) {
	g := NewWithT(t)

	cluster := schedulerCluster(corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
		},
		Env: []corev1.EnvVar{{Name: "MY_CUSTOM_ENV", Value: "custom-value"}},
		VolumeMounts: []corev1.VolumeMount{
			{Name: testCustomMountName, MountPath: testCustomMountPath},
		},
	})

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	scheduler := result.Spec.Containers[0]

	g.Expect(scheduler.Name).To(Equal(testMainContainerName))

	g.Expect(scheduler.Resources.Limits[corev1.ResourceMemory]).To(Equal(resource.MustParse("1Gi")))

	custom, ok := findEnv(scheduler.Env, "MY_CUSTOM_ENV")
	g.Expect(ok).To(BeTrue())
	g.Expect(custom.Value).To(Equal("custom-value"))

	g.Expect(scheduler.VolumeMounts).To(ContainElement(corev1.VolumeMount{Name: testCustomMountName, MountPath: testCustomMountPath}))

	_, ok = findEnv(scheduler.Env, "RUST_BACKTRACE")
	g.Expect(ok).To(BeTrue())
}

func TestBuildSchedulerPodTemplate_PreservesSidecarContainer(t *testing.T) {
	g := NewWithT(t)

	cluster := &computev1.PolarsCluster{
		Spec: computev1.PolarsClusterSpec{
			SchedulerSpec: computev1.SchedulerSpec{
				PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: testMainContainerName},
							{Name: testSidecarName, Image: "sidecar:latest"},
						},
					},
				},
			},
		},
	}

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(result.Spec.Containers).To(HaveLen(2))

	_, ok := findEnv(result.Spec.Containers[0].Env, "RUST_BACKTRACE")
	g.Expect(ok).To(BeTrue())

	var sidecar *corev1.Container
	for i := range result.Spec.Containers {
		if result.Spec.Containers[i].Name == testSidecarName {
			sidecar = &result.Spec.Containers[i]
		}
	}
	g.Expect(sidecar).NotTo(BeNil())
	g.Expect(sidecar.Image).To(Equal("sidecar:latest"))
	_, ok = findEnv(sidecar.Env, "RUST_BACKTRACE")
	g.Expect(ok).To(BeFalse())
}

func TestBuildSchedulerPodTemplate_AnonymousResultsS3Options(t *testing.T) {
	g := NewWithT(t)

	cluster := schedulerCluster(corev1.Container{})
	cluster.Spec.AnonymousResults = &computev1.AnonymousResultsSpec{
		S3: &computev1.AnonymousResultsS3Spec{
			Endpoint:        "s3://example-bucket/results",
			PresignDuration: &metav1.Duration{Duration: 0},
			Options: []corev1.EnvVar{
				{Name: testOptionRegion, Value: testOptionRegionValue},
			},
		},
	}

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	env := result.Spec.Containers[0].Env

	url, ok := findEnv(env, "PC_CUBLET__scheduler__anonymous_result_location__s3__url")
	g.Expect(ok).To(BeTrue())
	g.Expect(url.Value).To(Equal("s3://example-bucket/results"))

	region, ok := findEnv(env, "PC_CUBLET__scheduler__anonymous_result_location__s3__region")
	g.Expect(ok).To(BeTrue())
	g.Expect(region.Value).To(Equal(testOptionRegionValue))

	count := 0
	for _, e := range env {
		if e.Name == "PC_CUBLET__scheduler__anonymous_result_location__s3__url" {
			count++
		}
	}
	g.Expect(count).To(Equal(1))
}

func TestBuildSchedulerPodTemplate_NoContainers(t *testing.T) {
	g := NewWithT(t)

	cluster := &computev1.PolarsCluster{
		Spec: computev1.PolarsClusterSpec{
			SchedulerSpec: computev1.SchedulerSpec{
				PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{}},
				},
			},
		},
	}

	_, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).To(HaveOccurred())
}

func TestBuildSchedulerPodTemplate_ComposedRuntime(t *testing.T) {
	g := NewWithT(t)

	cluster := &computev1.PolarsCluster{
		ObjectMeta: metav1.ObjectMeta{Name: testClusterName, Namespace: testClusterNamespace},
		Spec: computev1.PolarsClusterSpec{
			Runtime: &computev1.RuntimeSpec{
				Composed: computev1.ComposedRuntimeSpec{
					Dist:         computev1.ImageSpec{Tag: "0.6.3"},
					Requirements: "some-package==1.0",
				},
			},
		},
	}

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(result.Spec.InitContainers).To(HaveLen(2))
	release := result.Spec.InitContainers[0]
	g.Expect(release.Name).To(Equal("release"))
	g.Expect(release.Image).To(Equal("polarscloud/polars-on-premises:0.6.3"))
	g.Expect(release.Args).To(Equal([]string{"-a", "/opt/.", "/emptydir"}))

	requirementsInit := result.Spec.InitContainers[1]
	g.Expect(requirementsInit.Name).To(Equal("requirements"))
	g.Expect(requirementsInit.Env).To(ContainElement(corev1.EnvVar{
		Name: "PYTHON_REQUIREMENTS_CONTENT", Value: "some-package==1.0",
	}))

	scheduler := result.Spec.Containers[0]
	g.Expect(scheduler.Name).To(Equal(componentScheduler))
	g.Expect(scheduler.Image).To(Equal("python:3.13.9-slim-bookworm"))
	g.Expect(scheduler.Command).To(Equal([]string{"/emptydir/setup.sh"}))
	g.Expect(scheduler.Args).To(Equal([]string{"/emptydir/bin/pc-cublet", "service"}))
	g.Expect(scheduler.WorkingDir).To(Equal("/tmp"))

	extras, ok := findEnv(scheduler.Env, "POLARS_EXTRAS")
	g.Expect(ok).To(BeTrue())
	g.Expect(extras.Value).To(ContainSubstring("cloudpickle"))

	requirements, ok := findEnv(scheduler.Env, "PYTHON_REQUIREMENTS")
	g.Expect(ok).To(BeTrue())
	g.Expect(requirements.Value).To(Equal("/emptydir/requirements.txt"))

	for _, name := range []string{"tmp-data", "release-data"} {
		_, ok := findVolume(result.Spec.Volumes, name)
		g.Expect(ok).To(BeTrue(), "expected volume %s", name)
	}

	g.Expect(result.Labels["app.kubernetes.io/version"]).To(Equal("0.6.3"))
}

func TestBuildSchedulerPodTemplate_ComposedRuntimeVersionFallback(t *testing.T) {
	g := NewWithT(t)

	cluster := &computev1.PolarsCluster{
		ObjectMeta: metav1.ObjectMeta{Name: testClusterName, Namespace: testClusterNamespace},
		Spec: computev1.PolarsClusterSpec{
			Version: "0.7.0",
			Runtime: &computev1.RuntimeSpec{Composed: computev1.ComposedRuntimeSpec{}},
		},
	}

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(result.Spec.InitContainers).To(HaveLen(1))
	g.Expect(result.Spec.InitContainers[0].Image).To(Equal("polarscloud/polars-on-premises:0.7.0"))
	g.Expect(result.Labels["app.kubernetes.io/version"]).To(Equal("0.7.0"))

	cluster.Spec.Runtime.Composed.Dist.Tag = "0.6.3"
	result, err = BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(result.Spec.InitContainers[0].Image).To(Equal("polarscloud/polars-on-premises:0.6.3"),
		"dist.tag should override spec.version")
	g.Expect(result.Labels["app.kubernetes.io/version"]).To(Equal("0.6.3"))
}

func TestBuildSchedulerPodTemplate_ClusterIDAndEULA(t *testing.T) {
	g := NewWithT(t)

	cluster := schedulerCluster(corev1.Container{})
	cluster.Spec.AcceptEula = true

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	env := result.Spec.Containers[0].Env

	clusterID, ok := findEnv(env, "PC_CUBLET__cluster_id")
	g.Expect(ok).To(BeTrue())
	g.Expect(clusterID.Value).To(Equal("polars/analytics"))

	eula, ok := findEnv(env, "POLARS_EULA_ACCEPTED")
	g.Expect(ok).To(BeTrue())
	g.Expect(eula.Value).To(Equal("yes"))

	cluster.Spec.ClusterID = "custom-id"
	result, err = BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())
	clusterID, _ = findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__cluster_id")
	g.Expect(clusterID.Value).To(Equal("custom-id"))
}

func TestBuildSchedulerPodTemplate_CheckpointEnabledByCheckpointData(t *testing.T) {
	g := NewWithT(t)

	cluster := schedulerCluster(corev1.Container{})

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())
	_, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__scheduler__checkpoint__enabled")
	g.Expect(ok).To(BeFalse())

	cluster.Spec.CheckpointData = &computev1.CheckpointDataSpec{
		S3: &computev1.CheckpointS3Spec{Endpoint: "s3://checkpoints"},
	}
	result, err = BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	env := result.Spec.Containers[0].Env
	enabled, ok := findEnv(env, "PC_CUBLET__scheduler__checkpoint__enabled")
	g.Expect(ok).To(BeTrue())
	g.Expect(enabled.Value).To(Equal(envValueTrue))

	period, ok := findEnv(env, "PC_CUBLET__scheduler__checkpoint__period")
	g.Expect(ok).To(BeTrue())
	g.Expect(period.Value).To(Equal("20m"))
}

func TestBuildSchedulerPodTemplate_ObservatoryVolume(t *testing.T) {
	g := NewWithT(t)

	cluster := schedulerCluster(corev1.Container{})

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	volume, ok := findVolume(result.Spec.Volumes, observatoryDataVolumeName)
	g.Expect(ok).To(BeTrue())
	g.Expect(volume.EmptyDir).NotTo(BeNil())

	g.Expect(result.Spec.Containers[0].VolumeMounts).To(ContainElement(corev1.VolumeMount{
		Name:      observatoryDataVolumeName,
		MountPath: observatoryDataMountPath,
	}))

	cluster.Spec.Observatory = &computev1.ObservatorySpec{
		PersistentVolumeClaim: &computev1.PersistentVolumeClaimRef{ClaimName: "observatory-data-claim"},
	}
	result, err = BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	volume, ok = findVolume(result.Spec.Volumes, observatoryDataVolumeName)
	g.Expect(ok).To(BeTrue())
	g.Expect(volume.PersistentVolumeClaim).NotTo(BeNil())
	g.Expect(volume.PersistentVolumeClaim.ClaimName).To(Equal("observatory-data-claim"))
}

func TestBuildSchedulerPodTemplate_DefaultProbeAndUserProbeWins(t *testing.T) {
	g := NewWithT(t)

	result, err := BuildSchedulerPodTemplate(schedulerCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())

	probe := result.Spec.Containers[0].ReadinessProbe
	g.Expect(probe).NotTo(BeNil())
	g.Expect(probe.TCPSocket).NotTo(BeNil())
	g.Expect(probe.TCPSocket.Port.IntVal).To(Equal(int32(5051)))

	userProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: []string{envValueTrue}},
		},
	}
	result, err = BuildSchedulerPodTemplate(schedulerCluster(corev1.Container{ReadinessProbe: userProbe}))
	g.Expect(err).NotTo(HaveOccurred())

	probe = result.Spec.Containers[0].ReadinessProbe
	g.Expect(probe.Exec).NotTo(BeNil())
	g.Expect(probe.TCPSocket).To(BeNil())
}

func TestBuildSchedulerPodTemplate_EnterpriseLicense(t *testing.T) {
	g := NewWithT(t)

	cluster := schedulerCluster(corev1.Container{})
	cluster.Spec.License.OnPremEnterprise = &computev1.LicenseOnPremEnterpriseSpec{
		SecretName:     "polars-license",
		SecretProperty: "license-key",
	}

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	scheduler := result.Spec.Containers[0]

	path, ok := findEnv(scheduler.Env, "PC_CUBLET__license__on_prem_enterprise__license_path")
	g.Expect(ok).To(BeTrue())
	g.Expect(path.Value).To(Equal("/mnt/license/license.json"))

	volume, ok := findVolume(result.Spec.Volumes, enterpriseLicenseVolume)
	g.Expect(ok).To(BeTrue())
	g.Expect(volume.Secret.SecretName).To(Equal("polars-license"))
	g.Expect(volume.Secret.Items).To(Equal([]corev1.KeyToPath{{Key: "license-key", Path: "license.json"}}))

	g.Expect(scheduler.VolumeMounts).To(ContainElement(corev1.VolumeMount{
		Name:      enterpriseLicenseVolume,
		MountPath: enterpriseLicenseMountDir,
	}))
}

func TestBuildSchedulerPodTemplate_RequireFreeWorkersDefaultsToReplicas(t *testing.T) {
	g := NewWithT(t)

	cluster := schedulerCluster(corev1.Container{})
	cluster.Spec.WorkerPool.Replicas = 4
	cluster.Spec.RequireFreeWorkers = &computev1.RequireFreeWorkersSpec{}

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	nWorkers, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__scheduler__n_workers")
	g.Expect(ok).To(BeTrue())
	g.Expect(nWorkers.Value).To(Equal("4"))

	count := uint32(7)
	cluster.Spec.RequireFreeWorkers.Count = &count
	result, err = BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())
	nWorkers, _ = findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__scheduler__n_workers")
	g.Expect(nWorkers.Value).To(Equal("7"))
}

func TestBuildSchedulerPodTemplate_LicenseServer(t *testing.T) {
	g := NewWithT(t)

	cluster := schedulerCluster(corev1.Container{})
	cluster.Spec.License.LicenseServer = &computev1.LicenseServerSpec{
		URI: "https://license-server.polars.svc.cluster.local:50051",
	}

	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	uri, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__license__license_server__uri")
	g.Expect(ok).To(BeTrue())
	g.Expect(uri.Value).To(Equal("https://license-server.polars.svc.cluster.local:50051"))
}

func TestBuildSchedulerPodTemplate_HostMetricsDisabled(t *testing.T) {
	g := NewWithT(t)

	cluster := schedulerCluster(corev1.Container{})
	result, err := BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())
	_, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__monitoring__host_metrics__enabled")
	g.Expect(ok).To(BeFalse())

	cluster.Spec.HostMetrics = &computev1.HostMetricsSpec{Enabled: false}
	result, err = BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	hostMetrics, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__monitoring__host_metrics__enabled")
	g.Expect(ok).To(BeTrue())
	g.Expect(hostMetrics.Value).To(Equal("false"))
}

func TestBuildSchedulerPodTemplate_LogLevelDefault(t *testing.T) {
	g := NewWithT(t)

	result, err := BuildSchedulerPodTemplate(schedulerCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())

	logLevel, ok := findEnv(result.Spec.Containers[0].Env, "PLC_LOG_LEVEL")
	g.Expect(ok).To(BeTrue())
	g.Expect(logLevel.Value).To(Equal("info"))

	cluster := schedulerCluster(corev1.Container{})
	warn := computev1.LogLevelWarn
	cluster.Spec.LogLevel = &warn
	result, err = BuildSchedulerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())
	logLevel, _ = findEnv(result.Spec.Containers[0].Env, "PLC_LOG_LEVEL")
	g.Expect(logLevel.Value).To(Equal("warn"))
}
