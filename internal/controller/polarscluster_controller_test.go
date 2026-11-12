package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1"
)

const (
	testStandInImage    = "busybox"
	validationNamespace = "default"
	licenseSecretName   = "test-license"

	testClientIDKey     = "client-id"
	testClientSecretKey = "client-secret"
	testWorkspaceIDKey  = "workspace-id"
)

var _ = Describe("PolarsCluster Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		polarscluster := &computev1.PolarsCluster{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind PolarsCluster")
			err := k8sClient.Get(ctx, typeNamespacedName, polarscluster)
			if err != nil && apierrors.IsNotFound(err) {
				resource := &computev1.PolarsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: computev1.PolarsClusterSpec{
						License: computev1.LicenseSpec{
							OnPrem: &computev1.LicenseOnPremSpec{
								ClientID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
									Key:                  testClientIDKey,
								}}},
								ClientSecret: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
									Key:                  testClientSecretKey,
								}}},
								WorkspaceID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
									Key:                  testWorkspaceIDKey,
								}}},
							},
						},
						Scheduler: &computev1.SchedulerSpec{
							PodTemplate: &corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Annotations: map[string]string{"karpenter.sh/do-not-disrupt": "true"},
								},
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{Name: "scheduler", Image: testStandInImage},
									},
								},
							},
							Services: &computev1.SchedulerServicesSpec{
								Internal: &computev1.ServiceConfig{ClusterIP: corev1.ClusterIPNone},
							},
						},
						WorkerPool: computev1.WorkerPoolDeclaration{
							PodTemplate: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{Name: componentWorker, Image: testStandInImage},
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &computev1.PolarsCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance PolarsCluster")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &PolarsClusterReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: events.NewFakeRecorder(10),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the three scheduler Services are created and owned")
			for _, name := range []string{
				resourceName + "-scheduler",
				resourceName + "-scheduler-internal",
				resourceName + "-observatory",
			} {
				var service corev1.Service
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: resourceNamespace}, &service)).To(Succeed())
				Expect(service.OwnerReferences).To(HaveLen(1))
				Expect(service.OwnerReferences[0].Kind).To(Equal("PolarsCluster"))
				Expect(service.Spec.Selector).To(HaveKeyWithValue(componentLabel, componentScheduler))
				if name == resourceName+"-scheduler-internal" {
					Expect(service.Spec.ClusterIP).To(Equal(corev1.ClusterIPNone))
				} else {
					Expect(service.Spec.ClusterIP).NotTo(Equal(corev1.ClusterIPNone))
				}
			}

			By("verifying the scheduler pod is created with a template hash")
			var schedulerPod corev1.Pod
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-scheduler", Namespace: resourceNamespace,
			}, &schedulerPod)).To(Succeed())
			Expect(schedulerPod.Labels).To(HaveKey(templateHashLabel))
			Expect(schedulerPod.Annotations).To(HaveKeyWithValue("karpenter.sh/do-not-disrupt", "true"))

			By("verifying the status reflects the reconciled generation")
			Expect(k8sClient.Get(ctx, typeNamespacedName, polarscluster)).To(Succeed())
			Expect(polarscluster.Status.ObservedGeneration).To(Equal(polarscluster.Generation))
			for _, condType := range []string{conditionReady, conditionSchedulerReady, conditionWorkerPoolReady} {
				cond := meta.FindStatusCondition(polarscluster.Status.Conditions, condType)
				Expect(cond).NotTo(BeNil(), "expected condition %s", condType)
				Expect(cond.ObservedGeneration).To(Equal(polarscluster.Generation))
			}
		})

	})
})

var _ = Describe("PolarsCluster validation", func() {
	It("should default version to the operator's default release", func() {
		ctx := context.Background()
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "validation-version-default", Namespace: validationNamespace},
			Spec: computev1.PolarsClusterSpec{
				License: computev1.LicenseSpec{
					OnPrem: &computev1.LicenseOnPremSpec{
						ClientID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
							Key:                  testClientIDKey,
						}}},
						ClientSecret: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
							Key:                  testClientSecretKey,
						}}},
						WorkspaceID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
							Key:                  testWorkspaceIDKey,
						}}},
					},
				},
				WorkerPool: computev1.WorkerPoolDeclaration{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentWorker, Image: testStandInImage}}},
					},
					Replicas: 1,
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		Expect(cluster.Spec.Version).To(Equal(computev1.DefaultVersion),
			"the API server should stamp the default into the spec")
	})

	It("should mark a cluster below the supported minimum version not Ready", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "validation-version-minimum", Namespace: validationNamespace}
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: computev1.PolarsClusterSpec{
				// Semver-valid, so it passes admission; only the controller
				// enforces the minimum.
				Version: "0.5.0",
				License: computev1.LicenseSpec{

					OnPrem: &computev1.LicenseOnPremSpec{
						ClientID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
							Key:                  testClientIDKey,
						}}},
						ClientSecret: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
							Key:                  testClientSecretKey,
						}}},
						WorkspaceID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
							Key:                  testWorkspaceIDKey,
						}}},
					},
				},
				WorkerPool: computev1.WorkerPoolDeclaration{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentWorker, Image: testStandInImage}}},
					},
					Replicas: 1,
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: events.NewFakeRecorder(10)}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		cond := meta.FindStatusCondition(cluster.Status.Conditions, conditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("InvalidVersion"))
		Expect(cond.Message).To(ContainSubstring("requires at least 0.7.1"))

		var pods corev1.PodList
		Expect(k8sClient.List(ctx, &pods, client.InNamespace(name.Namespace),
			client.MatchingLabels{clusterLabel: name.Name})).To(Succeed())
		Expect(pods.Items).To(BeEmpty(), "no pods should be composed for an unsupported version")
	})

	It("should reject a non-semver version other than latest", func() {
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "validation-version", Namespace: validationNamespace},
			Spec: computev1.PolarsClusterSpec{
				Version: "nightly",
				License: computev1.LicenseSpec{
					OnPrem: &computev1.LicenseOnPremSpec{
						ClientID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
							Key:                  testClientIDKey,
						}}},
						ClientSecret: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
							Key:                  testClientSecretKey,
						}}},
						WorkspaceID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
							Key:                  testWorkspaceIDKey,
						}}},
					},
				},
				WorkerPool: computev1.WorkerPoolDeclaration{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentWorker, Image: testStandInImage}}},
					},
					Replicas: 1,
				},
			},
		}

		err := k8sClient.Create(context.Background(), cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.version"))

		cluster.Spec.Version = computev1.LatestVersion
		Expect(k8sClient.Create(context.Background(), cluster)).To(Succeed())
		_ = k8sClient.Delete(context.Background(), cluster)
	})
})

var _ = Describe("PolarsCluster spec-error handling", func() {
	licenseSpec := computev1.LicenseSpec{
		OnPrem: &computev1.LicenseOnPremSpec{
			ClientID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
				Key:                  testClientIDKey,
			}}},
			ClientSecret: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
				Key:                  testClientSecretKey,
			}}},
			WorkspaceID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
				Key:                  testWorkspaceIDKey,
			}}},
		},
	}

	It("should mark an empty scheduler podTemplate not Ready without returning an error", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "spec-error-scheduler", Namespace: validationNamespace}
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: computev1.PolarsClusterSpec{
				License:   licenseSpec,
				Scheduler: &computev1.SchedulerSpec{},
				WorkerPool: computev1.WorkerPoolDeclaration{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentWorker, Image: testStandInImage}}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		recorder := events.NewFakeRecorder(10)
		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: recorder}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		for _, condType := range []string{conditionSchedulerReady, conditionReady} {
			cond := meta.FindStatusCondition(cluster.Status.Conditions, condType)
			Expect(cond).NotTo(BeNil(), "expected condition %s", condType)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("InvalidPodTemplate"))
			Expect(cond.Message).To(ContainSubstring("scheduler.podTemplate"))
		}

		Eventually(recorder.Events).Should(Receive(ContainSubstring("InvalidPodTemplate")))
	})

	It("should mark an empty worker podTemplate not Ready without returning an error", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "spec-error-worker", Namespace: validationNamespace}
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: computev1.PolarsClusterSpec{
				License: licenseSpec,
				Scheduler: &computev1.SchedulerSpec{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentScheduler, Image: testStandInImage}}},
					},
				},
				WorkerPool: computev1.WorkerPoolDeclaration{},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		recorder := events.NewFakeRecorder(10)
		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: recorder}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		for _, condType := range []string{conditionWorkerPoolReady, conditionReady} {
			cond := meta.FindStatusCondition(cluster.Status.Conditions, condType)
			Expect(cond).NotTo(BeNil(), "expected condition %s", condType)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("InvalidPodTemplate"))
			Expect(cond.Message).To(ContainSubstring("workerPool.podTemplate"))
		}

		Eventually(recorder.Events).Should(Receive(ContainSubstring("InvalidPodTemplate")))
	})

	It("should record a Warning event and leave Ready not Ready when the API server genuinely rejects a computed Service", func() {
		ctx := context.Background()
		// The "-scheduler-internal" Service name suffix is 20 characters;
		// a 50-character cluster name pushes it past the DNS label's real
		// 63-character limit, so the API server itself rejects the Create
		// with a genuine IsInvalid — not a synthetic/mocked one.
		longName := strings.Repeat("a", 50)
		name := types.NamespacedName{Name: longName, Namespace: validationNamespace}
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: longName, Namespace: validationNamespace},
			Spec: computev1.PolarsClusterSpec{
				License: licenseSpec,
				Scheduler: &computev1.SchedulerSpec{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentScheduler, Image: testStandInImage}}},
					},
				},
				WorkerPool: computev1.WorkerPoolDeclaration{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentWorker, Image: testStandInImage}}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		recorder := events.NewFakeRecorder(10)
		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: recorder}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		cond := meta.FindStatusCondition(cluster.Status.Conditions, conditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("ServiceRejected"))
		Expect(cond.Message).To(ContainSubstring("scheduler-internal"))

		Eventually(recorder.Events).Should(Receive(ContainSubstring("ServiceRejected")))

		By("verifying the scheduler pod was composed since it reconciles before Services")
		var schedulerPod corev1.Pod
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: longName + "-scheduler", Namespace: validationNamespace}, &schedulerPod)).To(Succeed())
	})

	It("should record and requeue a pod create that admission rejects as Forbidden", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "forbidden-retry", Namespace: validationNamespace}
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: computev1.PolarsClusterSpec{
				License: licenseSpec,
				Scheduler: &computev1.SchedulerSpec{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentScheduler, Image: testStandInImage}}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		// envtest disables the ServiceAccount admission plugin, so fake its
		// rejection of a pod naming a ServiceAccount that doesn't exist yet.
		watchingClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		rejectPods := interceptor.NewClient(watchingClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*corev1.Pod); ok {
					return apierrors.NewForbidden(corev1.Resource("pods"), "", errors.New(`error looking up service account default/ext-sa: serviceaccount "ext-sa" not found`))
				}
				return c.Create(ctx, obj, opts...)
			},
		})

		reconciler := &PolarsClusterReconciler{Client: rejectPods, Scheme: k8sClient.Scheme(), Recorder: events.NewFakeRecorder(10)}
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected the Forbidden error to be returned for a requeue, got %v", err)

		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		cond := meta.FindStatusCondition(cluster.Status.Conditions, conditionSchedulerReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("SchedulerPodRejected"))
	})

	It("should emit a Normal event on the not-Ready to Ready transition, and a Warning event with a diagnostic message on the reverse", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "spec-error-ready-transition", Namespace: validationNamespace}
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: computev1.PolarsClusterSpec{
				License: licenseSpec,
				Scheduler: &computev1.SchedulerSpec{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentScheduler, Image: testStandInImage}}},
					},
				},
				WorkerPool: computev1.WorkerPoolDeclaration{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentWorker, Image: testStandInImage}}},
					},
					Replicas: 0,
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		recorder := events.NewFakeRecorder(10)
		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: recorder}

		By("reconciling before the scheduler pod is Ready")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Eventually(recorder.Events).Should(Receive(ContainSubstring("SchedulerPodCreated")))

		By("marking the scheduler pod Ready")
		var schedulerPod corev1.Pod
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name.Name + "-scheduler", Namespace: name.Namespace}, &schedulerPod)).To(Succeed())
		schedulerPod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		Expect(k8sClient.Status().Update(ctx, &schedulerPod)).To(Succeed())

		By("reconciling the not-Ready to Ready transition")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(cluster.Status.Conditions, conditionReady)).To(BeTrue())
		Eventually(recorder.Events).Should(Receive(ContainSubstring("Reconciled")))

		By("reconciling again while already Ready")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(recorder.Events).NotTo(Receive())

		By("marking the scheduler pod not Ready again, mimicking a crash loop")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name.Name + "-scheduler", Namespace: name.Namespace}, &schedulerPod)).To(Succeed())
		schedulerPod.Status.Conditions = []corev1.PodCondition{{
			Type: corev1.PodReady, Status: corev1.ConditionFalse,
			Reason: "ContainersNotReady", Message: "containers with unready status: [scheduler]",
		}}
		schedulerPod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: componentScheduler, Image: testStandInImage,
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
				Reason: "CrashLoopBackOff", Message: "back-off 10s restarting failed container=scheduler",
			}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				Reason: "OOMKilled", ExitCode: 137,
			}},
		}}
		Expect(k8sClient.Status().Update(ctx, &schedulerPod)).To(Succeed())
		const crashLoopMessage = "container scheduler is waiting: CrashLoopBackOff: back-off 10s restarting failed container=scheduler; last terminated: OOMKilled (exit code 137)"

		By("reconciling the Ready to not-Ready transition")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		for _, condType := range []string{conditionSchedulerReady, conditionReady} {
			cond := meta.FindStatusCondition(cluster.Status.Conditions, condType)
			Expect(cond).NotTo(BeNil(), "expected condition %s", condType)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("NotReady"))
			Expect(cond.Message).To(Equal(crashLoopMessage))
		}

		var event string
		Eventually(recorder.Events).Should(Receive(&event))
		Expect(event).To(ContainSubstring("NotReady"))
		Expect(event).To(ContainSubstring(crashLoopMessage))
	})
})

// drainEvents returns every event currently buffered on recorder's channel,
// without blocking once it's empty.
func drainEvents(recorder *events.FakeRecorder) []string {
	var drained []string
	for {
		select {
		case e := <-recorder.Events:
			drained = append(drained, e)
		default:
			return drained
		}
	}
}

var _ = Describe("PolarsCluster lifecycle events", func() {
	licenseSpec := computev1.LicenseSpec{
		OnPrem: &computev1.LicenseOnPremSpec{
			ClientID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
				Key:                  testClientIDKey,
			}}},
			ClientSecret: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
				Key:                  testClientSecretKey,
			}}},
			WorkspaceID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
				Key:                  testWorkspaceIDKey,
			}}},
		},
	}

	It("should record events for pod creation, template-change recreation, scale-down, and stopped-pod replacement", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "lifecycle-events", Namespace: validationNamespace}
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: computev1.PolarsClusterSpec{
				License: licenseSpec,
				Scheduler: &computev1.SchedulerSpec{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentScheduler, Image: testStandInImage}}},
					},
				},
				WorkerPool: computev1.WorkerPoolDeclaration{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentWorker, Image: testStandInImage}}},
					},
					Replicas: 2,
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		recorder := events.NewFakeRecorder(20)
		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: recorder}

		By("reconciling the initial creation")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		emitted := drainEvents(recorder)
		Expect(emitted).To(ContainElement(ContainSubstring("SchedulerPodCreated")))
		workerCreated := 0
		for _, e := range emitted {
			if strings.Contains(e, "WorkerPodCreated") {
				workerCreated++
			}
		}
		Expect(workerCreated).To(Equal(2), "expected one WorkerPodCreated event per desired replica")

		By("changing the scheduler podTemplate to trigger a template-change recreate")
		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		cluster.Spec.Scheduler.PodTemplate.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "MARKER", Value: "1"}}
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(drainEvents(recorder)).To(ContainElement(ContainSubstring("SchedulerPodRecreating")))

		By("scaling the worker pool down to 1 replica")
		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		cluster.Spec.WorkerPool.Replicas = 1
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(drainEvents(recorder)).To(ContainElement(ContainSubstring("WorkerPodDeleted")))

		By("failing the remaining worker, which is replaced and left in place")
		workerLabels := client.MatchingLabels{clusterLabel: name.Name, componentLabel: componentWorker}
		var workers corev1.PodList
		Expect(k8sClient.List(ctx, &workers, client.InNamespace(name.Namespace), workerLabels)).To(Succeed())
		Expect(workers.Items).To(HaveLen(1))
		failed := workers.Items[0]
		failed.Status.Phase = corev1.PodFailed
		Expect(k8sClient.Status().Update(ctx, &failed)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(drainEvents(recorder)).To(ContainElement(ContainSubstring("WorkerPodCreated")))
		Expect(k8sClient.List(ctx, &workers, client.InNamespace(name.Namespace), workerLabels)).To(Succeed())
		Expect(workers.Items).To(HaveLen(2))
		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		Expect(cluster.Status.WorkerPool.Replicas).To(Equal(int32(1)))

		By("failing the scheduler pod, which is deleted and then recreated")
		var schedulerPod corev1.Pod
		schedulerKey := types.NamespacedName{Name: name.Name + "-scheduler", Namespace: name.Namespace}
		Expect(k8sClient.Get(ctx, schedulerKey, &schedulerPod)).To(Succeed())
		schedulerPod.Status.Phase = corev1.PodFailed
		Expect(k8sClient.Status().Update(ctx, &schedulerPod)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(drainEvents(recorder)).To(ContainElement(ContainSubstring("Deleted stopped scheduler pod")))
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(drainEvents(recorder)).To(ContainElement(ContainSubstring("SchedulerPodCreated")))

		By("rejecting a restartPolicy other than Always")
		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		cluster.Spec.Scheduler.PodTemplate.Spec.RestartPolicy = corev1.RestartPolicyNever
		cluster.Spec.WorkerPool.PodTemplate.Spec.RestartPolicy = corev1.RestartPolicyNever
		Expect(k8sClient.Update(ctx, cluster)).To(MatchError(And(
			ContainSubstring("spec.scheduler.podTemplate"),
			ContainSubstring("spec.workerPool.podTemplate"),
			ContainSubstring("spec.restartPolicy must be Always"),
		)))
	})
})

var _ = Describe("PolarsCluster ServiceAccount reconciliation", func() {
	licenseSpec := computev1.LicenseSpec{
		OnPrem: &computev1.LicenseOnPremSpec{
			ClientID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
				Key:                  testClientIDKey,
			}}},
			ClientSecret: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
				Key:                  testClientSecretKey,
			}}},
			WorkspaceID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
				Key:                  testWorkspaceIDKey,
			}}},
		},
	}

	It("should create and own the ServiceAccount when create is true", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "sa-create", Namespace: validationNamespace}
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: computev1.PolarsClusterSpec{
				License: licenseSpec,
				Scheduler: &computev1.SchedulerSpec{
					ServiceAccount: &computev1.ServiceAccountSpec{Create: true},
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentScheduler, Image: testStandInImage}}},
					},
				},
				WorkerPool: computev1.WorkerPoolDeclaration{
					ServiceAccount: &computev1.ServiceAccountSpec{Create: true},
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentWorker, Image: testStandInImage}}},
					},
					Replicas: 1,
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: events.NewFakeRecorder(10)}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		for _, saName := range []string{name.Name + "-scheduler", name.Name + "-worker"} {
			var sa corev1.ServiceAccount
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: name.Namespace}, &sa)).To(Succeed())
			Expect(sa.OwnerReferences).To(HaveLen(1))
			Expect(sa.OwnerReferences[0].Kind).To(Equal("PolarsCluster"))
		}
	})

	It("should not create a ServiceAccount when unset", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "sa-default", Namespace: validationNamespace}
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: computev1.PolarsClusterSpec{
				License: licenseSpec,
				Scheduler: &computev1.SchedulerSpec{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentScheduler, Image: testStandInImage}}},
					},
				},
				WorkerPool: computev1.WorkerPoolDeclaration{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentWorker, Image: testStandInImage}}},
					},
					Replicas: 1,
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: events.NewFakeRecorder(10)}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		for _, saName := range []string{name.Name + "-scheduler", name.Name + "-worker"} {
			var sa corev1.ServiceAccount
			err := k8sClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: name.Namespace}, &sa)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected no ServiceAccount %s to be created", saName)
		}
	})
})

func TestPodNotReadyMessage(t *testing.T) {
	containersNotReady := corev1.PodCondition{
		Type: corev1.PodReady, Status: corev1.ConditionFalse,
		Reason: "ContainersNotReady", Message: "containers with unready status: [polars]",
	}
	waiting := func(name, reason, message string) corev1.ContainerStatus {
		return corev1.ContainerStatus{Name: name, State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: message},
		}}
	}
	terminated := func(name, reason string, exitCode int32) corev1.ContainerStatus {
		return corev1.ContainerStatus{Name: name, State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{Reason: reason, ExitCode: exitCode},
		}}
	}

	tests := map[string]struct {
		status corev1.PodStatus
		want   string
	}{
		"unschedulable": {
			status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable",
				Message: "0/3 nodes are available: 3 node(s) didn't match Pod's node affinity/selector.",
			}}},
			want: "Unschedulable: 0/3 nodes are available: 3 node(s) didn't match Pod's node affinity/selector.",
		},
		"evicted": {
			status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted", Message: "The node was low on resource: memory."},
			want:   "Evicted: The node was low on resource: memory.",
		},
		"image pull past a completed init container outranks PodReady": {
			status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{containersNotReady},
				InitContainerStatuses: []corev1.ContainerStatus{
					terminated("release", "Completed", 0),
					waiting("requirements", "ImagePullBackOff", `Back-off pulling image "python:9.9"`),
				},
				ContainerStatuses: []corev1.ContainerStatus{waiting("polars", "PodInitializing", "")},
			},
			want: `init container requirements is waiting: ImagePullBackOff: Back-off pulling image "python:9.9"`,
		},
		"exited app container": {
			status: corev1.PodStatus{
				Conditions:        []corev1.PodCondition{containersNotReady},
				ContainerStatuses: []corev1.ContainerStatus{terminated("polars", "Completed", 0)},
			},
			want: "container polars terminated: Completed (exit code 0)",
		},
		"running but failing readiness falls back to PodReady": {
			status: corev1.PodStatus{
				Conditions:        []corev1.PodCondition{containersNotReady},
				ContainerStatuses: []corev1.ContainerStatus{{Name: "polars", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
			},
			want: "ContainersNotReady: containers with unready status: [polars]",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := podNotReadyMessage(&corev1.Pod{Status: tt.status}); got != tt.want {
				t.Fatalf("podNotReadyMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
