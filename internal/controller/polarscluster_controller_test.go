package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
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
			if err != nil && errors.IsNotFound(err) {
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
			for _, condType := range []string{"Ready", "SchedulerReady", "WorkerPoolReady"} {
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

		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		cond := meta.FindStatusCondition(cluster.Status.Conditions, "Ready")
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

		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
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

		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		for _, saName := range []string{name.Name + "-scheduler", name.Name + "-worker"} {
			var sa corev1.ServiceAccount
			err := k8sClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: name.Namespace}, &sa)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "expected no ServiceAccount %s to be created", saName)
		}
	})
})
