package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1alpha1"
)

// defaultServiceAccountName is the namespace's default ServiceAccount, used
// when reusing (Create: false) and Name is empty.
const defaultServiceAccountName = "default"

// schedulerServiceAccountSpec returns the scheduler's ServiceAccountSpec, or
// nil when scheduler or serviceAccount is unset.
func schedulerServiceAccountSpec(cluster *computev1.PolarsCluster) *computev1.ServiceAccountSpec {
	if s := cluster.Spec.Scheduler; s != nil {
		return s.ServiceAccount
	}
	return nil
}

// workerServiceAccountSpec returns the worker pool's ServiceAccountSpec, or
// nil when unset.
func workerServiceAccountSpec(cluster *computev1.PolarsCluster) *computev1.ServiceAccountSpec {
	return cluster.Spec.WorkerPool.ServiceAccount
}

// schedulerServiceAccountName is the default name the operator creates the
// scheduler's ServiceAccount under when Create is true and Name is empty.
func schedulerServiceAccountName(cluster *computev1.PolarsCluster) string {
	return fmt.Sprintf("%s-scheduler", cluster.Name)
}

// workerServiceAccountName is the default name the operator creates the
// worker pool's ServiceAccount under when Create is true and Name is empty.
func workerServiceAccountName(cluster *computev1.PolarsCluster) string {
	return fmt.Sprintf("%s-worker", cluster.Name)
}

// resolveServiceAccountName is the name a pod runs as: defaultName when spec
// requests creation and Name is empty, "default" when reusing and Name is
// empty, otherwise Name verbatim.
func resolveServiceAccountName(spec *computev1.ServiceAccountSpec, defaultName string) string {
	if spec == nil {
		return defaultServiceAccountName
	}
	if spec.Name != "" {
		return spec.Name
	}
	if spec.Create {
		return defaultName
	}
	return defaultServiceAccountName
}

// reconcileServiceAccount creates and owns the ServiceAccount when spec
// requests it; a no-op (an existing ServiceAccount is left untouched)
// otherwise.
func (r *PolarsClusterReconciler) reconcileServiceAccount(ctx context.Context, cluster *computev1.PolarsCluster, component, name string, spec *computev1.ServiceAccountSpec) error {
	if spec == nil || !spec.Create {
		return nil
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: v1.ObjectMeta{Name: name, Namespace: cluster.Namespace, Labels: standardLabels(cluster, component)},
	}
	if err := ctrl.SetControllerReference(cluster, sa, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, sa); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// reconcilePolarsClusterServiceAccounts ensures the scheduler's and worker
// pool's ServiceAccounts exist when the cluster spec requests the operator
// create them.
func (r *PolarsClusterReconciler) reconcilePolarsClusterServiceAccounts(ctx context.Context, cluster *computev1.PolarsCluster) error {
	schedulerSpec := schedulerServiceAccountSpec(cluster)
	if err := r.reconcileServiceAccount(ctx, cluster, componentScheduler, resolveServiceAccountName(schedulerSpec, schedulerServiceAccountName(cluster)), schedulerSpec); err != nil {
		return err
	}

	workerSpec := workerServiceAccountSpec(cluster)
	return r.reconcileServiceAccount(ctx, cluster, componentWorker, resolveServiceAccountName(workerSpec, workerServiceAccountName(cluster)), workerSpec)
}
