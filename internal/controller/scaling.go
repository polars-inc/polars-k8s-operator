package controller

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rbacv1ac "k8s.io/client-go/applyconfigurations/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1"
)

// autoscalingEnabled reports whether the scheduler resizes the worker pool
// itself, which setting workerPool.minReplicas opts into.
func autoscalingEnabled(cluster *computev1.PolarsCluster) bool {
	return cluster.Spec.WorkerPool.MinReplicas != nil
}

// scalingRBACName names the Role and RoleBinding that let the scheduler
// resize its own worker pool.
func scalingRBACName(cluster *computev1.PolarsCluster) string {
	return fmt.Sprintf("%s-scheduler-scaling", cluster.Name)
}

// reconcileScalingRBAC lets the scheduler's ServiceAccount get, list, watch
// and patch this PolarsCluster, as its autoscaler does, and removes that
// access once autoscaling is off. Every verb is limited to the cluster's
// name, which works for list and watch because the autoscaler watches with a
// metadata.name field selector.
func (r *PolarsClusterReconciler) reconcileScalingRBAC(ctx context.Context, cluster *computev1.PolarsCluster) error {
	meta := v1.ObjectMeta{Name: scalingRBACName(cluster), Namespace: cluster.Namespace}
	role := &rbacv1.Role{ObjectMeta: meta}
	binding := &rbacv1.RoleBinding{ObjectMeta: meta}

	if !autoscalingEnabled(cluster) {
		for _, obj := range []client.Object{binding, role} {
			if err := r.deleteOwned(ctx, cluster, obj); err != nil {
				return err
			}
		}
		return nil
	}

	labels := standardLabels(cluster, componentScheduler)
	if err := r.applyOwned(ctx, cluster, role, rbacv1ac.Role(meta.Name, meta.Namespace).
		WithLabels(labels).
		WithOwnerReferences(controllerRef(cluster)).
		WithRules(rbacv1ac.PolicyRule().
			WithAPIGroups(computev1.GroupVersion.Group).
			WithResources("polarsclusters").
			WithResourceNames(cluster.Name).
			WithVerbs("get", "list", "watch", "patch")),
		"ScalingRBAC"); err != nil {
		return err
	}

	return r.applyOwned(ctx, cluster, binding, rbacv1ac.RoleBinding(meta.Name, meta.Namespace).
		WithLabels(labels).
		WithOwnerReferences(controllerRef(cluster)).
		WithRoleRef(rbacv1ac.RoleRef().WithAPIGroup(rbacv1.GroupName).WithKind("Role").WithName(meta.Name)).
		WithSubjects(rbacv1ac.Subject().
			WithKind(rbacv1.ServiceAccountKind).
			WithName(schedulerServiceAccount(cluster)).
			WithNamespace(cluster.Namespace)),
		"ScalingRBAC")
}
