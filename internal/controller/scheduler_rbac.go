package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	computev1 "polars-inc/k8s-operator/api/v1"
)

// schedulerServiceAccountName is the per-cluster ServiceAccount the scheduler
// pod runs as. Shared name for the SA, Role, and RoleBinding.
func schedulerServiceAccountName(cluster *computev1.PolarsCluster) string {
	return fmt.Sprintf("%s-scheduler", cluster.Name)
}

// reconcilePolarsClusterRBAC ensures the per-cluster ServiceAccount, Role, and
// RoleBinding exist, owned by the cluster so they're garbage-collected with
// it. The Role grants get/update/patch on ONLY this cluster's PolarsCluster CR
// (and status), scoped by resourceNames, so the cublet's Kubernetes scaler can
// read the CR and update replicas without seeing any other cluster.
// resourceNames only restricts single-object verbs; it cannot scope
// list/watch/create, so the cublet must fetch by name, not via an informer.
func (r *PolarsClusterReconciler) reconcilePolarsClusterRBAC(ctx context.Context, cluster *computev1.PolarsCluster) error {
	name := schedulerServiceAccountName(cluster)
	labels := standardLabels(cluster, componentScheduler)

	objects := []client.Object{
		&corev1.ServiceAccount{
			ObjectMeta: v1.ObjectMeta{Name: name, Namespace: cluster.Namespace, Labels: labels},
		},
		&rbacv1.Role{
			ObjectMeta: v1.ObjectMeta{Name: name, Namespace: cluster.Namespace, Labels: labels},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups:     []string{computev1.GroupVersion.Group},
					Resources:     []string{"polarsclusters", "polarsclusters/status"},
					ResourceNames: []string{cluster.Name},
					Verbs:         []string{"get", "update", "patch"},
				},
			},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: v1.ObjectMeta{Name: name, Namespace: cluster.Namespace, Labels: labels},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "Role",
				Name:     name,
			},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.ServiceAccountKind, Name: name, Namespace: cluster.Namespace},
			},
		},
	}

	for _, obj := range objects {
		if err := ctrl.SetControllerReference(cluster, obj, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, obj); err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}
