package controller

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, role, func() error {
		role.Labels = standardLabels(cluster, componentScheduler)
		role.Rules = []rbacv1.PolicyRule{{
			APIGroups:     []string{computev1.GroupVersion.Group},
			Resources:     []string{"polarsclusters"},
			ResourceNames: []string{cluster.Name},
			Verbs:         []string{"get", "list", "watch", "patch"},
		}}
		return controllerutil.SetControllerReference(cluster, role, r.Scheme)
	}); err != nil {
		return classifyAPIError("ScalingRBACRejected", err)
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, binding, func() error {
		binding.Labels = standardLabels(cluster, componentScheduler)
		binding.RoleRef = rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name}
		binding.Subjects = []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: schedulerServiceAccount(cluster), Namespace: cluster.Namespace}}
		return controllerutil.SetControllerReference(cluster, binding, r.Scheme)
	}); err != nil {
		return classifyAPIError("ScalingRBACRejected", err)
	}
	return nil
}
