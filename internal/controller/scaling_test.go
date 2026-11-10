package controller

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1"
)

var _ = Describe("PolarsCluster autoscaling", func() {
	newCluster := func(name string) *computev1.PolarsCluster {
		return &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: validationNamespace},
			Spec: computev1.PolarsClusterSpec{
				Version: "0.8.4",
				License: computev1.LicenseSpec{
					OnPrem: &computev1.LicenseOnPremSpec{
						ClientID:     computev1.ValueOrSource{Value: "id"},
						ClientSecret: computev1.ValueOrSource{Value: "secret"},
						WorkspaceID:  computev1.ValueOrSource{Value: "workspace"},
					},
				},
				Scheduler: &computev1.SchedulerSpec{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentScheduler, Image: testStandInImage}}},
					},
				},
				WorkerPool: computev1.WorkerPoolDeclaration{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: componentWorker, Image: testStandInImage}}},
					},
					Replicas:    3,
					MinReplicas: ptr.To[int32](0),
				},
				WorkersPerQuery: &computev1.WorkersPerQuerySpec{Default: ptr.To[int32](1)},
			},
		}
	}

	It("should let the scheduler scale its pool, spare the workers it keeps, and revoke its access once autoscaling is off", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "autoscaling", Namespace: validationNamespace}
		cluster := newCluster(name.Name)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		reconciler := &PolarsClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: events.NewFakeRecorder(20)}
		reconcileCluster := func() {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())
		}
		reconcileCluster()

		By("binding a Role scoped to this cluster to the scheduler's own ServiceAccount")
		rbacName := types.NamespacedName{Name: name.Name + "-scheduler-scaling", Namespace: name.Namespace}
		var role rbacv1.Role
		Expect(k8sClient.Get(ctx, rbacName, &role)).To(Succeed())
		Expect(role.Rules).To(ConsistOf(rbacv1.PolicyRule{
			APIGroups:     []string{"compute.pola.rs"},
			Resources:     []string{"polarsclusters"},
			ResourceNames: []string{name.Name},
			Verbs:         []string{"get", "list", "watch", "patch"},
		}))
		var binding rbacv1.RoleBinding
		Expect(k8sClient.Get(ctx, rbacName, &binding)).To(Succeed())
		Expect(binding.Subjects).To(ConsistOf(rbacv1.Subject{Kind: rbacv1.ServiceAccountKind, Name: name.Name + "-scheduler", Namespace: name.Namespace}))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name.Name + "-scheduler", Namespace: name.Namespace}, &corev1.ServiceAccount{})).To(Succeed())

		By("allowing the scheduler the calls its autoscaler makes, on its own PolarsCluster only")
		asScheduler := rest.CopyConfig(cfg)
		asScheduler.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:" + name.Namespace + ":" + name.Name + "-scheduler"}
		schedulerClient, err := client.NewWithWatch(asScheduler, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		Expect(schedulerClient.Get(ctx, name, &computev1.PolarsCluster{})).To(Succeed())
		byName := client.MatchingFields{"metadata.name": name.Name}
		Expect(schedulerClient.List(ctx, &computev1.PolarsClusterList{}, client.InNamespace(name.Namespace), byName)).To(Succeed())
		watch, err := schedulerClient.Watch(ctx, &computev1.PolarsClusterList{}, client.InNamespace(name.Namespace), byName)
		Expect(err).NotTo(HaveOccurred())
		watch.Stop()
		Expect(schedulerClient.Patch(ctx, cluster, client.RawPatch(types.MergePatchType, []byte(`{"spec":{"workerPool":{"replicas":3}}}`)))).To(Succeed())
		err = schedulerClient.List(ctx, &computev1.PolarsClusterList{}, client.InNamespace(name.Namespace))
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected listing other PolarsClusters to be forbidden, got %v", err)

		By("scaling down with the autoscaler's patch, sparing the workers it keeps")
		workers := func() []string {
			var pods corev1.PodList
			Expect(k8sClient.List(ctx, &pods, client.InNamespace(name.Namespace),
				client.MatchingLabels{clusterLabel: name.Name, componentLabel: componentWorker})).To(Succeed())
			names := make([]string, 0, len(pods.Items))
			for _, pod := range pods.Items {
				names = append(names, pod.Name)
			}
			return names
		}
		before := workers()
		Expect(before).To(HaveLen(3))
		// The first pods listed are the ones a scale-down picks without workersToKeep.
		keep := before[:2]
		patch := fmt.Sprintf(`{"spec":{"workerPool":{"replicas":2,"workersToKeep":[%q,%q]}}}`, keep[0], keep[1])
		Expect(k8sClient.Patch(ctx, cluster, client.RawPatch(types.MergePatchType, []byte(patch)))).To(Succeed())
		reconcileCluster()
		Expect(workers()).To(ConsistOf(keep))

		By("revoking the scheduler's access once autoscaling is turned off")
		Expect(k8sClient.Get(ctx, name, cluster)).To(Succeed())
		cluster.Spec.WorkerPool.MinReplicas = nil
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
		reconcileCluster()
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, rbacName, &rbacv1.Role{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, rbacName, &rbacv1.RoleBinding{}))).To(BeTrue())
	})

	DescribeTable("rejecting inconsistent autoscaling settings",
		func(mutate func(*computev1.PolarsCluster), message string) {
			cluster := newCluster("autoscaling-invalid")
			mutate(cluster)
			Expect(k8sClient.Create(context.Background(), cluster)).To(MatchError(ContainSubstring(message)))
		},
		Entry("without a default worker count", func(c *computev1.PolarsCluster) {
			c.Spec.WorkersPerQuery = nil
		}, "workersPerQuery.default is required"),
		Entry("with a maximum but no minimum", func(c *computev1.PolarsCluster) {
			c.Spec.WorkerPool.MinReplicas = nil
			c.Spec.WorkerPool.MaxReplicas = ptr.To[int32](4)
		}, "maxReplicas requires minReplicas"),
		Entry("with the minimum above the maximum", func(c *computev1.PolarsCluster) {
			c.Spec.WorkerPool.MinReplicas = ptr.To[int32](5)
			c.Spec.WorkerPool.MaxReplicas = ptr.To[int32](4)
		}, "minReplicas must not exceed maxReplicas"),
	)
})
