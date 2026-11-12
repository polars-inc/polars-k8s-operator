package controller

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwac "sigs.k8s.io/gateway-api/applyconfiguration/apis/v1"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1"
)

const testGatewayName = "compute"

var _ = Describe("PolarsCluster Gateway API routes", func() {
	gateway := gatewayv1.ParentReference{
		Name:        testGatewayName,
		Namespace:   ptr.To(gatewayv1.Namespace("gateway-system")),
		SectionName: ptr.To(gatewayv1.SectionName("https")),
	}

	foreignController := []metav1.OwnerReference{{
		APIVersion: "v1", Kind: "ConfigMap", Name: "other", UID: "other-uid", Controller: ptr.To(true),
	}}

	routedCluster := func(name string) *computev1.PolarsCluster {
		return &computev1.PolarsCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: validationNamespace},
			Spec: computev1.PolarsClusterSpec{
				AcceptEula: true,
				License: computev1.LicenseSpec{
					OnPremEnterprise: &computev1.LicenseOnPremEnterpriseSpec{SecretName: licenseSecretName, SecretProperty: "routes-license"},
				},
				Scheduler: &computev1.SchedulerSpec{
					Services: &computev1.SchedulerServicesSpec{
						Scheduler: &computev1.ExposedServiceConfig{
							ServiceConfig: computev1.ServiceConfig{
								Annotations: map[string]string{"example.com/service": "exposed"},
							},
							Route: &computev1.RouteSpec{
								ParentRefs:  []gatewayv1.ParentReference{gateway},
								Hostnames:   []gatewayv1.Hostname{gatewayv1.Hostname(name + "-scheduler.example.com")},
								Labels:      map[string]string{"example.com/policy": "streaming"},
								Annotations: map[string]string{"example.com/route": "grpc"},
							},
						},
						Observatory: &computev1.ExposedServiceConfig{
							Route: &computev1.RouteSpec{
								ParentRefs: []gatewayv1.ParentReference{gateway},
								Hostnames:  []gatewayv1.Hostname{gatewayv1.Hostname(name + "-observatory.example.com")},
							},
						},
					},
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
	}

	reconcileCluster := func(ctx context.Context, cluster *computev1.PolarsCluster, gatewayAPIEnabled bool) reconcile.Result {
		reconciler := &PolarsClusterReconciler{
			Client:            k8sClient,
			Scheme:            k8sClient.Scheme(),
			Recorder:          events.NewFakeRecorder(10),
			GatewayAPIEnabled: gatewayAPIEnabled,
		}
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
		return result
	}

	expectRoutesReady := func(cluster *computev1.PolarsCluster, status metav1.ConditionStatus, reason string) *metav1.Condition {
		cond := meta.FindStatusCondition(cluster.Status.Conditions, conditionRoutesReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(status))
		Expect(cond.Reason).To(Equal(reason))
		return cond
	}

	backendRef := func(name string, port gatewayv1.PortNumber) gatewayv1.BackendRef {
		return gatewayv1.BackendRef{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Group: ptr.To(gatewayv1.Group("")),
				Kind:  ptr.To(gatewayv1.Kind("Service")),
				Name:  gatewayv1.ObjectName(name),
				Port:  ptr.To(port),
			},
			Weight: ptr.To[int32](1),
		}
	}

	It("should create and own a GRPCRoute and an HTTPRoute in front of the scheduler and observatory Services", func() {
		ctx := context.Background()
		cluster := routedCluster("routes-create")
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		reconcileCluster(ctx, cluster, true)

		var grpcRoute gatewayv1.GRPCRoute
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "routes-create-scheduler", Namespace: validationNamespace}, &grpcRoute)).To(Succeed())
		Expect(metav1.IsControlledBy(&grpcRoute, cluster)).To(BeTrue())
		Expect(grpcRoute.Labels).To(HaveKeyWithValue("example.com/policy", "streaming"))
		Expect(grpcRoute.Labels).To(HaveKeyWithValue(clusterLabel, cluster.Name))
		Expect(grpcRoute.Annotations).To(Equal(map[string]string{"example.com/route": "grpc"}))
		Expect(grpcRoute.Spec.ParentRefs).To(HaveLen(1))
		Expect(grpcRoute.Spec.ParentRefs[0].Name).To(Equal(gateway.Name))
		Expect(grpcRoute.Spec.ParentRefs[0].SectionName).To(Equal(gateway.SectionName))
		Expect(grpcRoute.Spec.Hostnames).To(ConsistOf(gatewayv1.Hostname("routes-create-scheduler.example.com")))
		Expect(grpcRoute.Spec.Rules).To(HaveLen(1))
		Expect(grpcRoute.Spec.Rules[0].BackendRefs).To(ConsistOf(
			gatewayv1.GRPCBackendRef{BackendRef: backendRef("routes-create-scheduler", 5051)}))

		var httpRoute gatewayv1.HTTPRoute
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "routes-create-observatory", Namespace: validationNamespace}, &httpRoute)).To(Succeed())
		Expect(metav1.IsControlledBy(&httpRoute, cluster)).To(BeTrue())
		Expect(httpRoute.Spec.Hostnames).To(ConsistOf(gatewayv1.Hostname("routes-create-observatory.example.com")))
		Expect(httpRoute.Spec.Rules).To(HaveLen(1))
		Expect(httpRoute.Spec.Rules[0].BackendRefs).To(ConsistOf(
			gatewayv1.HTTPBackendRef{BackendRef: backendRef("routes-create-observatory", 3001)}))

		By("verifying the scheduler Service still takes its ServiceConfig")
		var service corev1.Service
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "routes-create-scheduler", Namespace: validationNamespace}, &service)).To(Succeed())
		Expect(service.Annotations).To(HaveKeyWithValue("example.com/service", "exposed"))
		Expect(service.Spec.Ports).To(HaveLen(1))
		Expect(service.Spec.Ports[0].AppProtocol).To(HaveValue(Equal("kubernetes.io/h2c")))

		cond := expectRoutesReady(cluster, metav1.ConditionFalse, "NotAccepted")
		Expect(cond.Message).To(ContainSubstring("GRPCRoute routes-create-scheduler"))

		By("verifying a repeated reconcile doesn't write the unchanged routes")
		reconcileCluster(ctx, cluster, true)
		for _, route := range []client.Object{&grpcRoute, &httpRoute} {
			resourceVersion := route.GetResourceVersion()
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(route), route)).To(Succeed())
			Expect(route.GetResourceVersion()).To(Equal(resourceVersion), "%T was rewritten", route)
		}
	})

	It("should keep annotations added by others to its Service and route, and remove ones dropped from the spec", func() {
		ctx := context.Background()
		cluster := routedCluster("foreign-annotations")
		cluster.Spec.Scheduler.Services.Scheduler.Annotations["example.com/dropped"] = "soon"
		cluster.Spec.Scheduler.Services.Scheduler.Route.Annotations["example.com/dropped"] = "soon"
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()
		reconcileCluster(ctx, cluster, true)

		By("annotating the Service and GRPCRoute outside the operator")
		key := types.NamespacedName{Name: "foreign-annotations-scheduler", Namespace: validationNamespace}
		service, route := &corev1.Service{}, &gatewayv1.GRPCRoute{}
		for _, obj := range []client.Object{service, route} {
			Expect(k8sClient.Get(ctx, key, obj)).To(Succeed())
			patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
			obj.GetAnnotations()["example.com/external"] = "kept"
			Expect(k8sClient.Patch(ctx, obj, patch)).To(Succeed())
		}

		By("dropping an annotation from the spec")
		delete(cluster.Spec.Scheduler.Services.Scheduler.Annotations, "example.com/dropped")
		delete(cluster.Spec.Scheduler.Services.Scheduler.Route.Annotations, "example.com/dropped")
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
		reconcileCluster(ctx, cluster, true)

		for _, obj := range []client.Object{service, route} {
			Expect(k8sClient.Get(ctx, key, obj)).To(Succeed())
			Expect(obj.GetAnnotations()).To(HaveKeyWithValue("example.com/external", "kept"))
			Expect(obj.GetAnnotations()).NotTo(HaveKey("example.com/dropped"))
		}
	})

	It("should mark RoutesReady once every parent accepts both routes", func() {
		ctx := context.Background()
		cluster := routedCluster("routes-accepted")
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		reconcileCluster(ctx, cluster, true)

		var grpcRoute gatewayv1.GRPCRoute
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "routes-accepted-scheduler", Namespace: validationNamespace}, &grpcRoute)).To(Succeed())
		grpcRoute.Status.Parents = []gatewayv1.RouteParentStatus{routeParentStatus(gateway, metav1.ConditionTrue, metav1.ConditionTrue)}
		Expect(k8sClient.Status().Update(ctx, &grpcRoute)).To(Succeed())

		reconcileCluster(ctx, cluster, true)
		cond := expectRoutesReady(cluster, metav1.ConditionFalse, "NotAccepted")
		Expect(cond.Message).To(ContainSubstring("HTTPRoute routes-accepted-observatory"))

		var httpRoute gatewayv1.HTTPRoute
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "routes-accepted-observatory", Namespace: validationNamespace}, &httpRoute)).To(Succeed())
		httpRoute.Status.Parents = []gatewayv1.RouteParentStatus{routeParentStatus(gateway, metav1.ConditionTrue, metav1.ConditionTrue)}
		Expect(k8sClient.Status().Update(ctx, &httpRoute)).To(Succeed())

		reconcileCluster(ctx, cluster, true)
		expectRoutesReady(cluster, metav1.ConditionTrue, "Reconciled")
	})

	It("should delete the routes it owns once they are no longer requested, and leave others alone", func() {
		ctx := context.Background()
		cluster := routedCluster("routes-removed")
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		reconcileCluster(ctx, cluster, true)

		By("replacing the operator's HTTPRoute with one it does not own")
		unowned := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "routes-removed-observatory", Namespace: validationNamespace}}
		Expect(k8sClient.Delete(ctx, unowned)).To(Succeed())
		Expect(k8sClient.Create(ctx, unowned)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, unowned) }()

		cluster.Spec.Scheduler.Services = nil
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
		reconcileCluster(ctx, cluster, true)

		err := k8sClient.Get(ctx, types.NamespacedName{Name: "routes-removed-scheduler", Namespace: validationNamespace}, &gatewayv1.GRPCRoute{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected the GRPCRoute to be deleted, got %v", err)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(unowned), unowned)).To(Succeed())
		Expect(meta.FindStatusCondition(cluster.Status.Conditions, conditionRoutesReady)).To(BeNil())
	})

	It("should report GatewayAPIUnavailable without failing the reconcile when the Gateway API is not installed", func() {
		ctx := context.Background()
		cluster := routedCluster("routes-unavailable")
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		reconcileCluster(ctx, cluster, false)

		expectRoutesReady(cluster, metav1.ConditionFalse, "GatewayAPIUnavailable")
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "routes-unavailable-scheduler", Namespace: validationNamespace}, &gatewayv1.GRPCRoute{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected no GRPCRoute, got %v", err)
	})

	It("should report a route owned by another controller as a RouteConflict and retry, without blocking the scheduler or the other route", func() {
		ctx := context.Background()
		cluster := routedCluster("routes-conflict")
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		foreign := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{
			Name:            "routes-conflict-observatory",
			Namespace:       validationNamespace,
			OwnerReferences: foreignController,
		}}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, foreign) }()

		result := reconcileCluster(ctx, cluster, true)
		Expect(result.RequeueAfter).To(Equal(retryInterval))

		cond := expectRoutesReady(cluster, metav1.ConditionFalse, "RouteConflict")
		Expect(cond.Message).To(Equal("HTTPRoute routes-conflict-observatory is already controlled by another object: ConfigMap other"))
		Expect(meta.IsStatusConditionFalse(cluster.Status.Conditions, conditionReady)).To(BeTrue())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "routes-conflict-scheduler", Namespace: validationNamespace}, &corev1.Pod{})).
			To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "routes-conflict-scheduler", Namespace: validationNamespace}, &gatewayv1.GRPCRoute{})).
			To(Succeed())
	})

	It("should report a Forbidden route as RouteRejected and retry", func() {
		ctx := context.Background()
		cluster := routedCluster("routes-forbidden")
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		watchClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		rejectRoutes := interceptor.NewClient(watchClient, interceptor.Funcs{
			Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				if _, ok := obj.(*gwac.HTTPRouteApplyConfiguration); ok {
					return apierrors.NewForbidden(gatewayv1.Resource("httproutes"), "routes-forbidden-observatory", errors.New("denied by policy"))
				}
				return c.Apply(ctx, obj, opts...)
			},
		})

		reconciler := &PolarsClusterReconciler{Client: rejectRoutes, Scheme: k8sClient.Scheme(), Recorder: events.NewFakeRecorder(10), GatewayAPIEnabled: true}
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(retryInterval))
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
		expectRoutesReady(cluster, metav1.ConditionFalse, "RouteRejected")
	})

	It("should report a Service owned by another controller as a ServiceConflict and retry", func() {
		ctx := context.Background()
		cluster := routedCluster("service-conflict")
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, cluster) }()

		foreign := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "service-conflict-scheduler-internal",
				Namespace:       validationNamespace,
				OwnerReferences: foreignController,
			},
			Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
		}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, foreign) }()

		result := reconcileCluster(ctx, cluster, true)
		Expect(result.RequeueAfter).To(Equal(retryInterval))

		cond := meta.FindStatusCondition(cluster.Status.Conditions, conditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("ServiceConflict"))
		Expect(cond.Message).To(Equal("Service service-conflict-scheduler-internal is already controlled by another object: ConfigMap other"))
	})

	It("should reject a route without parentRefs", func() {
		cluster := routedCluster("routes-no-parents")
		cluster.Spec.Scheduler.Services.Observatory.Route.ParentRefs = nil
		err := k8sClient.Create(context.Background(), cluster)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected an Invalid error, got %v", err)
	})

	It("should detect the Gateway API route CRDs", func() {
		httpClient, err := rest.HTTPClientFor(cfg)
		Expect(err).NotTo(HaveOccurred())
		mapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(GatewayAPIInstalled(mapper)).To(BeTrue())

		Expect(GatewayAPIInstalled(meta.NewDefaultRESTMapper(nil))).To(BeFalse())
	})
})

var _ = Describe("routeAccepted", func() {
	gateway := gatewayv1.ParentReference{Name: testGatewayName}
	other := gatewayv1.ParentReference{Name: "other"}

	DescribeTable("readiness",
		func(want bool, parents ...gatewayv1.RouteParentStatus) {
			route := &gatewayv1.HTTPRoute{}
			route.Status.Parents = parents
			Expect(routeAccepted(routeTarget{route: route, status: &route.Status.RouteStatus})).To(Equal(want))
		},
		Entry("no parent status", false),
		Entry("not accepted", false, routeParentStatus(gateway, metav1.ConditionFalse, metav1.ConditionTrue)),
		Entry("unresolved backend", false, routeParentStatus(gateway, metav1.ConditionTrue, metav1.ConditionFalse)),
		Entry("accepted", true, routeParentStatus(gateway, metav1.ConditionTrue, metav1.ConditionTrue)),
		Entry("accepted by one of two parents", true,
			routeParentStatus(other, metav1.ConditionFalse, metav1.ConditionFalse),
			routeParentStatus(gateway, metav1.ConditionTrue, metav1.ConditionTrue)),
	)
})

func routeParentStatus(ref gatewayv1.ParentReference, accepted, resolved metav1.ConditionStatus) gatewayv1.RouteParentStatus {
	return gatewayv1.RouteParentStatus{
		ParentRef:      ref,
		ControllerName: "example.com/gateway-controller",
		Conditions: []metav1.Condition{
			{Type: string(gatewayv1.RouteConditionAccepted), Status: accepted, Reason: "Test", LastTransitionTime: metav1.Now()},
			{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: resolved, Reason: "Test", LastTransitionTime: metav1.Now()},
		},
	}
}
