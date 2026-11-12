package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwac "sigs.k8s.io/gateway-api/applyconfiguration/apis/v1"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1"
)

const (
	conditionRoutesReady = "RoutesReady"
	reasonNotAccepted    = "NotAccepted"
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
	route  client.Object
	kind   string
	status *gatewayv1.RouteStatus
	build  func(routeParts) runtime.ApplyConfiguration
	port   gatewayv1.PortNumber
	spec   *computev1.RouteSpec
}

type routeParts struct {
	meta      *metav1ac.ObjectMetaApplyConfiguration
	common    gwac.CommonRouteSpecApplyConfiguration
	hostnames []gatewayv1.Hostname
	backend   gwac.BackendRefApplyConfiguration
}

func routeTargets(cluster *computev1.PolarsCluster) []routeTarget {
	services := schedulerServices(cluster)

	grpc := &gatewayv1.GRPCRoute{ObjectMeta: v1.ObjectMeta{Name: schedulerServiceName(cluster), Namespace: cluster.Namespace}}
	http := &gatewayv1.HTTPRoute{ObjectMeta: v1.ObjectMeta{Name: observatoryServiceName(cluster), Namespace: cluster.Namespace}}

	return []routeTarget{
		{
			route:  grpc,
			kind:   "GRPCRoute",
			status: &grpc.Status.RouteStatus,
			build: func(p routeParts) runtime.ApplyConfiguration {
				route := gwac.GRPCRoute(grpc.Name, grpc.Namespace).WithSpec(&gwac.GRPCRouteSpecApplyConfiguration{
					CommonRouteSpecApplyConfiguration: p.common,
					Hostnames:                         p.hostnames,
					Rules: []gwac.GRPCRouteRuleApplyConfiguration{{
						BackendRefs: []gwac.GRPCBackendRefApplyConfiguration{{BackendRefApplyConfiguration: p.backend}},
					}},
				})
				route.ObjectMetaApplyConfiguration = p.meta
				return route
			},
			port: schedulerPort,
			spec: routeSpec(services.Scheduler),
		},
		{
			route:  http,
			kind:   "HTTPRoute",
			status: &http.Status.RouteStatus,
			build: func(p routeParts) runtime.ApplyConfiguration {
				route := gwac.HTTPRoute(http.Name, http.Namespace).WithSpec(&gwac.HTTPRouteSpecApplyConfiguration{
					CommonRouteSpecApplyConfiguration: p.common,
					Hostnames:                         p.hostnames,
					Rules: []gwac.HTTPRouteRuleApplyConfiguration{{
						BackendRefs: []gwac.HTTPBackendRefApplyConfiguration{{BackendRefApplyConfiguration: p.backend}},
					}},
				})
				route.ObjectMetaApplyConfiguration = p.meta
				return route
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
	retry bool
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
				if err := r.deleteOwned(ctx, cluster, target.route); err != nil {
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
			status.notReady(se.reason, se.Error())
			status.retry = status.retry || errors.Is(se, errConflict) || apierrors.IsForbidden(se)
			continue
		}
		if !routeAccepted(target) {
			status.notReady(reasonNotAccepted, fmt.Sprintf("%s %s: no parent has accepted it yet", target.kind, target.route.GetName()))
		}
	}
	return status, nil
}

func (r *PolarsClusterReconciler) applyRoute(ctx context.Context, cluster *computev1.PolarsCluster, target routeTarget) error {
	// Converted generically, so fields gateway-api adds to ParentReference
	// carry over.
	parentRefs, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&gatewayv1.CommonRouteSpec{ParentRefs: target.spec.ParentRefs})
	if err != nil {
		return err
	}
	var common gwac.CommonRouteSpecApplyConfiguration
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(parentRefs, &common); err != nil {
		return err
	}

	route := target.route
	return r.applyOwned(ctx, cluster, route, target.build(routeParts{
		meta: metav1ac.ObjectMeta().
			WithName(route.GetName()).
			WithNamespace(route.GetNamespace()).
			WithLabels(target.spec.Labels).
			WithLabels(schedulerObjectLabels(cluster)).
			WithAnnotations(target.spec.Annotations).
			WithOwnerReferences(controllerRef(cluster)),
		common:    common,
		hostnames: target.spec.Hostnames,
		backend: *gwac.BackendRef().
			WithName(gatewayv1.ObjectName(route.GetName())).
			WithPort(target.port),
	}), "Route")
}

// deleteOwned deletes obj if it exists and cluster controls it.
func (r *PolarsClusterReconciler) deleteOwned(ctx context.Context, cluster *computev1.PolarsCluster, obj client.Object) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !v1.IsControlledBy(obj, cluster) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, obj))
}

func routeAccepted(target routeTarget) bool {
	return slices.ContainsFunc(target.status.Parents, func(parent gatewayv1.RouteParentStatus) bool {
		return meta.IsStatusConditionTrue(parent.Conditions, string(gatewayv1.RouteConditionAccepted)) &&
			meta.IsStatusConditionTrue(parent.Conditions, string(gatewayv1.RouteConditionResolvedRefs))
	})
}
