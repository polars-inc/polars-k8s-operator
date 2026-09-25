package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1alpha1"
)

const (
	conditionRoutesReady = "RoutesReady"
	reasonNotAccepted    = "NotAccepted"
	reasonRouteConflict  = "RouteConflict"

	// routeConflictRetryInterval is how often a cluster whose route name is
	// taken by an object it doesn't control checks again: that object going
	// away doesn't trigger a reconcile.
	routeConflictRetryInterval = time.Minute
)

func GatewayAPIInstalled(mapper meta.RESTMapper) (bool, error) {
	for _, kind := range []string{"GRPCRoute", "HTTPRoute"} {
		_, err := mapper.RESTMapping(schema.GroupKind{Group: gatewayv1.GroupName, Kind: kind}, gatewayv1.GroupVersion.Version)
		if meta.IsNoMatchError(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

type routeTarget struct {
	route     client.Object
	kind      string
	common    *gatewayv1.CommonRouteSpec
	hostnames *[]gatewayv1.Hostname
	status    *gatewayv1.RouteStatus
	setRules  func(gatewayv1.BackendRef)
	port      gatewayv1.PortNumber
	spec      *computev1.RouteSpec
}

func routeTargets(cluster *computev1.PolarsCluster) []routeTarget {
	services := schedulerServices(cluster)

	grpc := &gatewayv1.GRPCRoute{ObjectMeta: v1.ObjectMeta{Name: schedulerServiceName(cluster), Namespace: cluster.Namespace}}
	http := &gatewayv1.HTTPRoute{ObjectMeta: v1.ObjectMeta{Name: observatoryServiceName(cluster), Namespace: cluster.Namespace}}

	return []routeTarget{
		{
			route:     grpc,
			kind:      "GRPCRoute",
			common:    &grpc.Spec.CommonRouteSpec,
			hostnames: &grpc.Spec.Hostnames,
			status:    &grpc.Status.RouteStatus,
			setRules: func(backend gatewayv1.BackendRef) {
				grpc.Spec.Rules = []gatewayv1.GRPCRouteRule{{BackendRefs: []gatewayv1.GRPCBackendRef{{BackendRef: backend}}}}
			},
			port: schedulerPort,
			spec: routeSpec(services.Scheduler),
		},
		{
			route:     http,
			kind:      "HTTPRoute",
			common:    &http.Spec.CommonRouteSpec,
			hostnames: &http.Spec.Hostnames,
			status:    &http.Status.RouteStatus,
			setRules: func(backend gatewayv1.BackendRef) {
				http.Spec.Rules = []gatewayv1.HTTPRouteRule{{
					Matches: []gatewayv1.HTTPRouteMatch{{Path: &gatewayv1.HTTPPathMatch{
						Type:  ptr.To(gatewayv1.PathMatchPathPrefix),
						Value: ptr.To("/"),
					}}},
					BackendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: backend}},
				}}
			},
			port: observatoryRESTPort,
			spec: routeSpec(services.Observatory),
		},
	}
}

func routeSpec(config *computev1.ExposedServiceConfig) *computev1.RouteSpec {
	if config == nil {
		return nil
	}
	return config.Route
}

type routesStatus struct {
	requested bool
	ready     bool
	reason    string
	message   string
	// Tracked apart from reason, which keeps only the first failure.
	conflict bool
}

func (s *routesStatus) notReady(reason, message string) {
	if s.ready || (s.reason == reasonNotAccepted && reason != reasonNotAccepted) {
		s.ready = false
		s.reason = reason
		s.message = message
	}
}

func (r *PolarsClusterReconciler) reconcileRoutes(ctx context.Context, cluster *computev1.PolarsCluster) (routesStatus, error) {
	status := routesStatus{ready: true}
	for _, target := range routeTargets(cluster) {
		if target.spec == nil {
			if r.GatewayAPIEnabled {
				if err := r.deleteOwnedRoute(ctx, cluster, target.route); err != nil {
					return routesStatus{}, err
				}
			}
			continue
		}

		if !r.GatewayAPIEnabled {
			return routesStatus{
				requested: true,
				reason:    "GatewayAPIUnavailable",
				message:   "the Gateway API v1 GRPCRoute and HTTPRoute CRDs were not installed when the operator started",
			}, nil
		}
		status.requested = true

		if err := r.applyRoute(ctx, cluster, target); err != nil {
			var se *specError
			if !errors.As(err, &se) {
				return routesStatus{}, err
			}
			status.notReady(se.reason, routeMessage(target, se.Error()))
			status.conflict = status.conflict || se.reason == reasonRouteConflict
			continue
		}
		if !routeAccepted(target) {
			status.notReady(reasonNotAccepted, routeMessage(target, "no parent has accepted it yet"))
		}
	}
	return status, nil
}

// applyRoute spells out the fields the Gateway API CRDs default, so an
// unchanged route isn't rewritten on every reconcile.
func (r *PolarsClusterReconciler) applyRoute(ctx context.Context, cluster *computev1.PolarsCluster, target routeTarget) error {
	route := target.route
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
		labels := map[string]string{}
		maps.Copy(labels, target.spec.Labels)
		maps.Copy(labels, schedulerObjectLabels(cluster))
		route.SetLabels(labels)
		route.SetAnnotations(target.spec.Annotations)

		target.common.ParentRefs = target.spec.ParentRefs
		*target.hostnames = target.spec.Hostnames
		target.setRules(gatewayv1.BackendRef{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Group: ptr.To(gatewayv1.Group("")),
				Kind:  ptr.To(gatewayv1.Kind("Service")),
				Name:  gatewayv1.ObjectName(route.GetName()),
				Port:  &target.port,
			},
			Weight: ptr.To[int32](1),
		})
		return controllerutil.SetControllerReference(cluster, route, r.Scheme)
	})
	if alreadyOwned := (*controllerutil.AlreadyOwnedError)(nil); errors.As(err, &alreadyOwned) {
		return &specError{reason: reasonRouteConflict, err: err}
	}
	return classifyAPIError("RouteRejected", err)
}

func routeMessage(target routeTarget, detail string) string {
	return fmt.Sprintf("%s %s: %s", target.kind, target.route.GetName(), detail)
}

func (r *PolarsClusterReconciler) deleteOwnedRoute(ctx context.Context, cluster *computev1.PolarsCluster, route client.Object) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(route), route); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !v1.IsControlledBy(route, cluster) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, route))
}

func routeAccepted(target routeTarget) bool {
	return slices.ContainsFunc(target.status.Parents, func(parent gatewayv1.RouteParentStatus) bool {
		return meta.IsStatusConditionTrue(parent.Conditions, string(gatewayv1.RouteConditionAccepted)) &&
			meta.IsStatusConditionTrue(parent.Conditions, string(gatewayv1.RouteConditionResolvedRefs))
	})
}
