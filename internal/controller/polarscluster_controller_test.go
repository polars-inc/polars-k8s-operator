package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	computev1 "polars-inc/k8s-operator/api/v1"
)

var _ = Describe("PolarsCluster Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
			licenseSecretName = "test-license"
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
									Key:                  "client-id",
								}}},
								ClientSecret: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
									Key:                  "client-secret",
								}}},
								WorkspaceID: computev1.ValueOrSource{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: licenseSecretName},
									Key:                  "workspace-id",
								}}},
							},
						},
						SchedulerSpec: computev1.SchedulerSpec{
							PodTemplate: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{Name: "scheduler", Image: "busybox"},
									},
								},
							},
						},
						WorkerPool: computev1.WorkerPoolDeclaration{
							PodTemplate: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{Name: "worker", Image: "busybox"},
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
		})

		It("should delete pods listed in WorkersToDelete and clear the field", func() {
			controllerReconciler := &PolarsClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("scaling the worker pool to 1 replica and reconciling to create the worker pod")
			Expect(k8sClient.Get(ctx, typeNamespacedName, polarscluster)).To(Succeed())
			polarscluster.Spec.WorkerPool.Replicas = 1
			Expect(k8sClient.Update(ctx, polarscluster)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var pods corev1.PodList
			Expect(k8sClient.List(ctx, &pods,
				client.MatchingLabels{clusterLabel: resourceName, componentLabel: componentWorker},
				client.InNamespace(resourceNamespace),
			)).To(Succeed())
			Expect(pods.Items).To(HaveLen(1))
			workerPodName := pods.Items[0].Name

			By("requesting deletion of the worker pod via WorkersToDelete")
			Expect(k8sClient.Get(ctx, typeNamespacedName, polarscluster)).To(Succeed())
			polarscluster.Spec.WorkerPool.WorkersToDelete = []string{workerPodName}
			Expect(k8sClient.Update(ctx, polarscluster)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the pod was deleted (or marked for deletion) and the field was cleared")
			var deletedPod corev1.Pod
			getErr := k8sClient.Get(ctx, types.NamespacedName{Name: workerPodName, Namespace: resourceNamespace}, &deletedPod)
			if getErr == nil {
				// envtest has no kubelet to finalize pod termination, so the
				// object may still be present but marked for deletion.
				Expect(deletedPod.DeletionTimestamp).NotTo(BeNil())
			} else {
				Expect(errors.IsNotFound(getErr)).To(BeTrue())
			}

			Expect(k8sClient.Get(ctx, typeNamespacedName, polarscluster)).To(Succeed())
			Expect(polarscluster.Spec.WorkerPool.WorkersToDelete).To(BeEmpty())
		})
	})
})
