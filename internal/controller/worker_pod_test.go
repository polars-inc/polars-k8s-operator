package controller

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1alpha1"
)

func polarsCluster(extra corev1.Container) *computev1.PolarsCluster {
	extra.Name = testMainContainerName
	return &computev1.PolarsCluster{
		ObjectMeta: metav1.ObjectMeta{Name: testClusterName, Namespace: testClusterNamespace},
		Spec: computev1.PolarsClusterSpec{
			WorkerPool: computev1.WorkerPoolDeclaration{
				PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{extra},
					},
				},
			},
		},
	}
}

func TestBuildWorkerPodTemplate_ComposedRuntimeWithoutTemplate(t *testing.T) {
	g := NewWithT(t)

	cluster := &computev1.PolarsCluster{
		ObjectMeta: metav1.ObjectMeta{Name: testClusterName, Namespace: testClusterNamespace},
		Spec: computev1.PolarsClusterSpec{
			Version: "0.7.0",
			Runtime: &computev1.RuntimeSpec{Composed: computev1.ComposedRuntimeSpec{}},
		},
	}

	result, err := BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	worker := result.Spec.Containers[0]
	g.Expect(worker.Name).To(Equal(componentWorker))
	g.Expect(result.Spec.InitContainers).To(HaveLen(1))
	g.Expect(worker.ReadinessProbe).NotTo(BeNil())
	g.Expect(worker.ReadinessProbe.GRPC).NotTo(BeNil())
}

func TestBuildWorkerPodTemplate_PodNaming(t *testing.T) {
	g := NewWithT(t)

	result, err := BuildWorkerPodTemplate(polarsCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(result.Name).To(BeEmpty())
	g.Expect(result.GenerateName).To(Equal(testClusterName + "-worker-"))
	g.Expect(result.Namespace).To(Equal(testClusterNamespace))
}

func TestBuildWorkerPodTemplate_DefaultShuffleData(t *testing.T) {
	g := NewWithT(t)

	result, err := BuildWorkerPodTemplate(polarsCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())

	worker := result.Spec.Containers[0]

	localPath, ok := findEnv(worker.Env, "PC_CUBLET__worker__shuffle_location__local__path")
	g.Expect(ok).To(BeTrue())
	g.Expect(localPath.Value).To(Equal(shuffleDataMountPath + "/shuffle_data"))

	_, ok = findVolume(result.Spec.Volumes, shuffleDataVolumeName)
	g.Expect(ok).To(BeTrue())

	g.Expect(worker.VolumeMounts).To(ContainElement(corev1.VolumeMount{
		Name:      shuffleDataVolumeName,
		MountPath: shuffleDataMountPath,
	}))

	g.Expect(worker.Ports).To(ContainElement(corev1.ContainerPort{
		Name: workerFlightPortName, ContainerPort: 5053, Protocol: corev1.ProtocolTCP,
	}))

	temporaryData, ok := findVolume(result.Spec.Volumes, temporaryDataVolumeName)
	g.Expect(ok).To(BeTrue())
	g.Expect(temporaryData.EmptyDir).NotTo(BeNil())
}

func TestBuildWorkerPodTemplate_PreservesUserFields(t *testing.T) {
	g := NewWithT(t)

	cluster := polarsCluster(corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
		},
		Env: []corev1.EnvVar{{Name: "MY_CUSTOM_ENV", Value: "custom-value"}},
		VolumeMounts: []corev1.VolumeMount{
			{Name: testCustomMountName, MountPath: testCustomMountPath},
		},
	})

	result, err := BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	worker := result.Spec.Containers[0]

	g.Expect(worker.Name).To(Equal(testMainContainerName))

	g.Expect(worker.Resources.Limits[corev1.ResourceMemory]).To(Equal(resource.MustParse("1Gi")))

	custom, ok := findEnv(worker.Env, "MY_CUSTOM_ENV")
	g.Expect(ok).To(BeTrue())
	g.Expect(custom.Value).To(Equal("custom-value"))

	g.Expect(worker.VolumeMounts).To(ContainElement(corev1.VolumeMount{Name: testCustomMountName, MountPath: testCustomMountPath}))

	_, ok = findEnv(worker.Env, "RUST_BACKTRACE")
	g.Expect(ok).To(BeTrue())
}

func TestBuildWorkerPodTemplate_PreservesSidecarContainer(t *testing.T) {
	g := NewWithT(t)

	cluster := &computev1.PolarsCluster{
		Spec: computev1.PolarsClusterSpec{
			WorkerPool: computev1.WorkerPoolDeclaration{
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

	result, err := BuildWorkerPodTemplate(cluster)
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

func TestBuildWorkerPodTemplate_ShuffleDataS3Options(t *testing.T) {
	g := NewWithT(t)

	cluster := polarsCluster(corev1.Container{})
	cluster.Spec.WorkerPool.ShuffleData = &computev1.ShuffleDataSpec{
		S3: &computev1.ShuffleDataS3Spec{
			Endpoint: "s3://example-bucket/shuffle",
			Options: []corev1.EnvVar{
				{Name: testOptionRegion, Value: testOptionRegionValue},
			},
		},
	}

	result, err := BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	env := result.Spec.Containers[0].Env

	url, ok := findEnv(env, "PC_CUBLET__worker__shuffle_location__s3__url")
	g.Expect(ok).To(BeTrue())
	g.Expect(url.Value).To(Equal("s3://example-bucket/shuffle"))

	region, ok := findEnv(env, "PC_CUBLET__worker__shuffle_location__s3__region")
	g.Expect(ok).To(BeTrue())
	g.Expect(region.Value).To(Equal(testOptionRegionValue))

	_, ok = findVolume(result.Spec.Volumes, shuffleDataVolumeName)
	g.Expect(ok).To(BeFalse())
}

func TestBuildWorkerPodTemplate_CheckpointDataS3(t *testing.T) {
	g := NewWithT(t)

	cluster := polarsCluster(corev1.Container{})
	cluster.Spec.Checkpoint = &computev1.CheckpointSpec{
		Data: computev1.CheckpointDataSpec{
			S3: &computev1.CheckpointS3Spec{
				Endpoint: "s3://example-bucket/checkpoints",
				Options: []corev1.EnvVar{
					{Name: testOptionRegion, Value: "us-west-2"},
				},
			},
		},
	}

	result, err := BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	env := result.Spec.Containers[0].Env

	url, ok := findEnv(env, "PC_CUBLET__worker__checkpoint_location__s3__url")
	g.Expect(ok).To(BeTrue())
	g.Expect(url.Value).To(Equal("s3://example-bucket/checkpoints"))

	region, ok := findEnv(env, "PC_CUBLET__worker__checkpoint_location__s3__region")
	g.Expect(ok).To(BeTrue())
	g.Expect(region.Value).To(Equal("us-west-2"))
}

func TestBuildWorkerPodTemplate_TemporaryDataEphemeral(t *testing.T) {
	g := NewWithT(t)

	cluster := polarsCluster(corev1.Container{})
	cluster.Spec.WorkerPool.TemporaryData = &computev1.TemporaryDataSpec{
		EphemeralVolumeClaim: &computev1.EphemeralVolumeClaimSpec{
			StorageClassName: "fast-disks",
			Size:             resource.MustParse("10Gi"),
		},
	}

	result, err := BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	temporaryData, ok := findVolume(result.Spec.Volumes, temporaryDataVolumeName)
	g.Expect(ok).To(BeTrue())
	g.Expect(temporaryData.Ephemeral).NotTo(BeNil())

	claimSpec := temporaryData.Ephemeral.VolumeClaimTemplate.Spec
	g.Expect(*claimSpec.StorageClassName).To(Equal("fast-disks"))
	g.Expect(claimSpec.Resources.Requests[corev1.ResourceStorage]).To(Equal(resource.MustParse("10Gi")))
}

func TestBuildWorkerPodTemplate_HeartBeatInterval(t *testing.T) {
	g := NewWithT(t)

	cluster := polarsCluster(corev1.Container{})
	cluster.Spec.WorkerPool.HeartBeatInterval = &metav1.Duration{Duration: 30 * time.Second}

	result, err := BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	heartbeat, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__worker__heartbeat_period")
	g.Expect(ok).To(BeTrue())
	g.Expect(heartbeat.Value).To(Equal("30s"))
}

func TestBuildWorkerPodTemplate_HostMetricsDisabled(t *testing.T) {
	g := NewWithT(t)

	cluster := polarsCluster(corev1.Container{})
	result, err := BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())
	_, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__monitoring__host_metrics__enabled")
	g.Expect(ok).To(BeFalse())

	cluster.Spec.HostMetrics = &computev1.HostMetricsSpec{Enabled: false}
	result, err = BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	hostMetrics, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__monitoring__host_metrics__enabled")
	g.Expect(ok).To(BeTrue())
	g.Expect(hostMetrics.Value).To(Equal("false"))
}

func TestBuildWorkerPodTemplate_NoContainers(t *testing.T) {
	g := NewWithT(t)

	cluster := &computev1.PolarsCluster{
		Spec: computev1.PolarsClusterSpec{
			WorkerPool: computev1.WorkerPoolDeclaration{
				PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{}},
				},
			},
		},
	}

	_, err := BuildWorkerPodTemplate(cluster)
	g.Expect(err).To(HaveOccurred())
}

func TestBuildWorkerPodTemplate_DiscoveryHostnames(t *testing.T) {
	g := NewWithT(t)

	result, err := BuildWorkerPodTemplate(polarsCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())

	env := result.Spec.Containers[0].Env

	// Bare Service name: same-namespace resolution via the pod's first DNS
	// search domain, no cluster-domain dependency.
	scheduler, ok := findEnv(env, "PC_CUBLET__static_leader__scheduler_service__public_addr__hostname")
	g.Expect(ok).To(BeTrue())
	g.Expect(scheduler.Value).To(Equal("analytics-scheduler-internal"))

	observatory, ok := findEnv(env, "PC_CUBLET__static_leader__observatory_service__public_addr__hostname")
	g.Expect(ok).To(BeTrue())
	g.Expect(observatory.Value).To(Equal(scheduler.Value))
}

func TestBuildWorkerPodTemplate_CpuReserved(t *testing.T) {
	g := NewWithT(t)

	result, err := BuildWorkerPodTemplate(polarsCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())
	_, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__cpu_reserved")
	g.Expect(ok).To(BeFalse())

	cluster := polarsCluster(corev1.Container{
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1500m")},
		},
	})
	result, err = BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	cpuReserved, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__cpu_reserved")
	g.Expect(ok).To(BeTrue())
	g.Expect(cpuReserved.Value).To(Equal("1500m"))
}

func TestBuildWorkerPodTemplate_DefaultHeartBeat(t *testing.T) {
	g := NewWithT(t)

	result, err := BuildWorkerPodTemplate(polarsCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())

	heartbeat, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__worker__heartbeat_period")
	g.Expect(ok).To(BeTrue())
	g.Expect(heartbeat.Value).To(Equal("5s"))
}

func TestBuildWorkerPodTemplate_LocalPathShuffle(t *testing.T) {
	g := NewWithT(t)

	cluster := polarsCluster(corev1.Container{})
	cluster.Spec.WorkerPool.ShuffleData = &computev1.ShuffleDataSpec{
		Local: &computev1.LocalFilesystemSpec{Path: "/mnt/fast-ssd/shuffle"},
	}

	result, err := BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	worker := result.Spec.Containers[0]

	path, ok := findEnv(worker.Env, "PC_CUBLET__worker__shuffle_location__local__path")
	g.Expect(ok).To(BeTrue())
	g.Expect(path.Value).To(Equal("/mnt/fast-ssd/shuffle"))

	_, ok = findVolume(result.Spec.Volumes, shuffleDataVolumeName)
	g.Expect(ok).To(BeFalse())

	g.Expect(worker.Ports).To(ContainElement(corev1.ContainerPort{
		Name: workerFlightPortName, ContainerPort: 5053, Protocol: corev1.ProtocolTCP,
	}))
}

func TestBuildWorkerPodTemplate_SharedFilesystemShuffle(t *testing.T) {
	g := NewWithT(t)

	cluster := polarsCluster(corev1.Container{})
	cluster.Spec.WorkerPool.ShuffleData = &computev1.ShuffleDataSpec{
		SharedFilesystem: &computev1.SharedFilesystemSpec{Path: "/mnt/nfs/shuffle"},
	}

	result, err := BuildWorkerPodTemplate(cluster)
	g.Expect(err).NotTo(HaveOccurred())

	path, ok := findEnv(result.Spec.Containers[0].Env, "PC_CUBLET__worker__shuffle_location__shared_filesystem__path")
	g.Expect(ok).To(BeTrue())
	g.Expect(path.Value).To(Equal("/mnt/nfs/shuffle"))

	_, ok = findVolume(result.Spec.Volumes, shuffleDataVolumeName)
	g.Expect(ok).To(BeFalse())
}

func TestBuildWorkerPodTemplate_DefaultProbeAndUserProbeWins(t *testing.T) {
	g := NewWithT(t)

	result, err := BuildWorkerPodTemplate(polarsCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())

	probe := result.Spec.Containers[0].ReadinessProbe
	g.Expect(probe).NotTo(BeNil())
	g.Expect(probe.GRPC).NotTo(BeNil())
	g.Expect(probe.GRPC.Port).To(Equal(int32(5052)))

	userProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: []string{envValueTrue}},
		},
	}
	result, err = BuildWorkerPodTemplate(polarsCluster(corev1.Container{ReadinessProbe: userProbe}))
	g.Expect(err).NotTo(HaveOccurred())

	probe = result.Spec.Containers[0].ReadinessProbe
	g.Expect(probe.Exec).NotTo(BeNil())
	g.Expect(probe.GRPC).To(BeNil())
}

func TestPodTemplateHash_ChangesWithSpec(t *testing.T) {
	g := NewWithT(t)

	first, err := BuildWorkerPodTemplate(polarsCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())
	second, err := BuildWorkerPodTemplate(polarsCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())

	firstHash, err := podTemplateHash(&first.Spec)
	g.Expect(err).NotTo(HaveOccurred())
	secondHash, err := podTemplateHash(&second.Spec)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(firstHash).To(Equal(secondHash), "same spec must produce the same hash")

	changed, err := BuildWorkerPodTemplate(polarsCluster(corev1.Container{
		Env: []corev1.EnvVar{{Name: "EXTRA", Value: "changed"}},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	changedHash, err := podTemplateHash(&changed.Spec)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(changedHash).NotTo(Equal(firstHash), "changed spec must produce a different hash")
}

func TestBuildWorkerPodTemplate_LogLevelDefault(t *testing.T) {
	g := NewWithT(t)

	result, err := BuildWorkerPodTemplate(polarsCluster(corev1.Container{}))
	g.Expect(err).NotTo(HaveOccurred())

	logLevel, ok := findEnv(result.Spec.Containers[0].Env, "PLC_LOG_LEVEL")
	g.Expect(ok).To(BeTrue())
	g.Expect(logLevel.Value).To(Equal("info"))
}
