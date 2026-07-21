package controller

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1alpha1"
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
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{Name: "scheduler", Image: testStandInImage},
									},
								},
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
				Recorder: record.NewFakeRecorder(10),
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
			}

			By("verifying the scheduler pod is created with a template hash")
			var schedulerPod corev1.Pod
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-scheduler", Namespace: resourceNamespace,
			}, &schedulerPod)).To(Succeed())
			Expect(schedulerPod.Labels).To(HaveKey(templateHashLabel))

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

		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(10)}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		cond := meta.FindStatusCondition(cluster.Status.Conditions, conditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("InvalidVersion"))
		Expect(cond.Message).To(ContainSubstring("requires at least 0.6.3"))

		var pods corev1.PodList
		Expect(k8sClient.List(ctx, &pods, client.InNamespace(name.Namespace),
			client.MatchingLabels{clusterLabel: name.Name})).To(Succeed())
		Expect(pods.Items).To(BeEmpty(), "no pods should be composed for an unsupported version")
	})

	It("should reject a non-semver version", func() {
		cluster := &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "validation-version", Namespace: validationNamespace},
			Spec: computev1.PolarsClusterSpec{
				Version: "latest",
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

		recorder := record.NewFakeRecorder(10)
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

		recorder := record.NewFakeRecorder(10)
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

		recorder := record.NewFakeRecorder(10)
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

		By("verifying no scheduler pod was composed since Services never finished reconciling")
		var schedulerPod corev1.Pod
		err = k8sClient.Get(ctx, types.NamespacedName{Name: longName + "-scheduler", Namespace: validationNamespace}, &schedulerPod)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
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

		recorder := record.NewFakeRecorder(10)
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
			Message: "containers with unready status: [scheduler]",
		}}
		Expect(k8sClient.Status().Update(ctx, &schedulerPod)).To(Succeed())

		By("reconciling the Ready to not-Ready transition")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		for _, condType := range []string{conditionSchedulerReady, conditionReady} {
			cond := meta.FindStatusCondition(cluster.Status.Conditions, condType)
			Expect(cond).NotTo(BeNil(), "expected condition %s", condType)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("NotReady"))
			Expect(cond.Message).To(Equal("containers with unready status: [scheduler]"))
		}

		var event string
		Eventually(recorder.Events).Should(Receive(&event))
		Expect(event).To(ContainSubstring("NotReady"))
		Expect(event).To(ContainSubstring("containers with unready status: [scheduler]"))
	})
})

// drainEvents returns every event currently buffered on recorder's channel,
// without blocking once it's empty.
func drainEvents(recorder *record.FakeRecorder) []string {
	var events []string
	for {
		select {
		case e := <-recorder.Events:
			events = append(events, e)
		default:
			return events
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

	It("should record Normal events for pod creation, template-change recreation, and scale-down", func() {
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

		recorder := record.NewFakeRecorder(20)
		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: recorder}

		By("reconciling the initial creation")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		events := drainEvents(recorder)
		Expect(events).To(ContainElement(ContainSubstring("SchedulerPodCreated")))
		workerCreated := 0
		for _, e := range events {
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

		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(10)}
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

		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(10)}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		for _, saName := range []string{name.Name + "-scheduler", name.Name + "-worker"} {
			var sa corev1.ServiceAccount
			err := k8sClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: name.Namespace}, &sa)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected no ServiceAccount %s to be created", saName)
		}
	})
})
