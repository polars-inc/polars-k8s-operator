package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1alpha1"
)

const (
	clusterLabel      = "compute.pola.rs/cluster"
	componentLabel    = "compute.pola.rs/component"
	templateHashLabel = "compute.pola.rs/template-hash"

	componentScheduler = "scheduler"
	componentWorker    = "worker"
)

// PolarsClusterReconciler reconciles a PolarsCluster object
type PolarsClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=compute.pola.rs,resources=polarsclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.pola.rs,resources=polarsclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=compute.pola.rs,resources=polarsclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PolarsCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
func (r *PolarsClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	cluster := &computev1.PolarsCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fail fast on an uninterpretable version before composing any pod spec
	// from it. No requeue: only a spec change can fix it.
	if _, err := clusterVersion(cluster); err != nil {
		meta.SetStatusCondition(&cluster.Status.Conditions, v1.Condition{
			Type:               "Ready",
			Status:             v1.ConditionFalse,
			Reason:             "InvalidVersion",
			Message:            err.Error(),
			ObservedGeneration: cluster.Generation,
		})
		cluster.Status.ObservedGeneration = cluster.Generation
		if err := r.Status().Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if err := r.reconcileServices(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcilePolarsClusterServiceAccounts(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}

	schedulerStatus, err := r.reconcileScheduler(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	cluster.Status.Scheduler = schedulerStatus
	setReadyCondition(cluster, "SchedulerReady", schedulerStatus.Ready, "Reconciled", "NotReady")

	workerPoolStatus, err := r.reconcileWorkerPool(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	cluster.Status.WorkerPool = workerPoolStatus
	workerPoolReady := workerPoolStatus.Replicas == cluster.Spec.WorkerPool.Replicas &&
		workerPoolStatus.ReadyReplicas == workerPoolStatus.Replicas
	setReadyCondition(cluster, "WorkerPoolReady", workerPoolReady, "Reconciled", "NotReady")

	setReadyCondition(cluster, "Ready", schedulerStatus.Ready && workerPoolReady, "Reconciled", "NotReady")

	cluster.Status.ObservedGeneration = cluster.Generation

	if err := r.Status().Update(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// setReadyCondition upserts a True/False condition of the given type on the
// cluster, choosing the reason based on whether ready is true.
func setReadyCondition(cluster *computev1.PolarsCluster, condType string, ready bool, readyReason, notReadyReason string) {
	status := v1.ConditionFalse
	reason := notReadyReason
	if ready {
		status = v1.ConditionTrue
		reason = readyReason
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, v1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		ObservedGeneration: cluster.Generation,
	})
}

// podTemplateHash returns a stable hash of the pod spec, used to detect when
// pods must be recreated.
func podTemplateHash(spec *corev1.PodSpec) (string, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write(data)
	return fmt.Sprintf("%08x", hasher.Sum32()), nil
}

// reconcileScheduler creates the cluster's singleton scheduler pod if it
// doesn't exist yet, recreates it when the computed template changed, and
// reports whether it's currently Ready.
func (r *PolarsClusterReconciler) reconcileScheduler(ctx context.Context, cluster *computev1.PolarsCluster) (computev1.SchedulerStatus, error) {
	pod, err := BuildSchedulerPodTemplate(cluster)
	if err != nil {
		return computev1.SchedulerStatus{}, err
	}
	hash, err := podTemplateHash(&pod.Spec)
	if err != nil {
		return computev1.SchedulerStatus{}, err
	}
	pod.Labels[templateHashLabel] = hash

	var existing corev1.Pod
	err = r.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, &existing)
	if errors.IsNotFound(err) {
		if err := controllerutil.SetControllerReference(cluster, &pod, r.Scheme); err != nil {
			return computev1.SchedulerStatus{}, err
		}

		if err := r.Create(ctx, &pod); err != nil {
			return computev1.SchedulerStatus{}, err
		}

		return computev1.SchedulerStatus{Ready: false}, nil
	} else if err != nil {
		return computev1.SchedulerStatus{}, err
	}

	if existing.DeletionTimestamp.IsZero() && existing.Labels[templateHashLabel] != hash {
		if err := r.Delete(ctx, &existing); err != nil && !errors.IsNotFound(err) {
			return computev1.SchedulerStatus{}, err
		}
		// recreated on the next reconcile once the old pod is gone
		return computev1.SchedulerStatus{Ready: false}, nil
	}

	return computev1.SchedulerStatus{Ready: podIsReady(&existing)}, nil
}

// reconcileWorkerPool creates/deletes pods for cluster.Spec.WorkerPool to
// match its desired replica count, recreating pods whose computed template
// changed, and returns the resulting observed status.
func (r *PolarsClusterReconciler) reconcileWorkerPool(ctx context.Context, cluster *computev1.PolarsCluster) (computev1.WorkerPoolStatus, error) {
	wp := &cluster.Spec.WorkerPool

	podTemplate, err := BuildWorkerPodTemplate(cluster)
	if err != nil {
		return computev1.WorkerPoolStatus{}, err
	}
	hash, err := podTemplateHash(&podTemplate.Spec)
	if err != nil {
		return computev1.WorkerPoolStatus{}, err
	}
	podTemplate.Labels[templateHashLabel] = hash

	if err := controllerutil.SetControllerReference(cluster, &podTemplate, r.Scheme); err != nil {
		return computev1.WorkerPoolStatus{}, err
	}

	// we may temporarily have more pods than allowed due to not waiting for the grace period
	var managedPods corev1.PodList
	if err := r.List(ctx, &managedPods,
		client.MatchingLabels{clusterLabel: cluster.Name, componentLabel: componentWorker},
		client.InNamespace(cluster.Namespace),
	); err != nil {
		return computev1.WorkerPoolStatus{}, err
	}

	var activeManagedPods []corev1.Pod
	for _, pod := range managedPods.Items {
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}
		if pod.Labels[templateHashLabel] != hash {
			if err := r.Delete(ctx, &pod); err != nil && !errors.IsNotFound(err) {
				return computev1.WorkerPoolStatus{}, err
			}
			continue
		}
		activeManagedPods = append(activeManagedPods, pod)
	}

	var lackingPodCount = int(wp.Replicas) - len(activeManagedPods)
	if lackingPodCount > 0 {
		for range lackingPodCount {
			pod := podTemplate.DeepCopy()

			if err := r.Create(ctx, pod); err != nil {
				return computev1.WorkerPoolStatus{}, err
			}

			activeManagedPods = append(activeManagedPods, *pod)
		}
	} else if lackingPodCount < 0 {
		var numPodsToDelete = -lackingPodCount
		for i := range numPodsToDelete {
			pod := &activeManagedPods[i]
			if err := r.Delete(ctx, pod); err != nil {
				if errors.IsNotFound(err) {
					continue
				}

				return computev1.WorkerPoolStatus{}, err
			}
		}
		activeManagedPods = activeManagedPods[numPodsToDelete:]
	}

	var readyReplicas int32
	for _, pod := range activeManagedPods {
		if podIsReady(&pod) {
			readyReplicas++
		}
	}

	return computev1.WorkerPoolStatus{
		Replicas:      int32(len(activeManagedPods)),
		ReadyReplicas: readyReplicas,
		Selector:      fmt.Sprintf("%s=%s,%s=%s", clusterLabel, cluster.Name, componentLabel, componentWorker),
	}, nil
}

// reconcileServices creates or updates the scheduler, internal, and
// observatory Services.
func (r *PolarsClusterReconciler) reconcileServices(ctx context.Context, cluster *computev1.PolarsCluster) error {
	var services computev1.SchedulerServicesSpec
	if cluster.Spec.Scheduler != nil && cluster.Spec.Scheduler.Services != nil {
		services = *cluster.Spec.Scheduler.Services
	}

	desired := []struct {
		name   string
		config *computev1.ServiceConfig
		ports  []corev1.ServicePort
	}{
		{
			name:   schedulerServiceName(cluster),
			config: services.Scheduler,
			ports: []corev1.ServicePort{
				{Name: "sched", Port: 5051, Protocol: corev1.ProtocolTCP},
			},
		},
		{
			name:   schedulerInternalServiceName(cluster),
			config: services.Internal,
			ports: []corev1.ServicePort{
				{Name: "worker-sched", Port: 5050, Protocol: corev1.ProtocolTCP},
				{Name: "obser-grpc", Port: 5049, Protocol: corev1.ProtocolTCP},
			},
		},
		{
			name:   observatoryServiceName(cluster),
			config: services.Observatory,
			ports: []corev1.ServicePort{
				{Name: "obser-rest", Port: 3001, Protocol: corev1.ProtocolTCP},
			},
		},
	}

	selector := map[string]string{
		clusterLabel:   cluster.Name,
		componentLabel: componentScheduler,
	}

	for _, svc := range desired {
		serviceType := corev1.ServiceTypeClusterIP
		var annotations map[string]string
		if svc.config != nil {
			if svc.config.Type != nil && *svc.config.Type != "" {
				serviceType = *svc.config.Type
			}
			annotations = svc.config.Annotations
		}

		var existing corev1.Service
		err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: svc.name}, &existing)
		if errors.IsNotFound(err) {
			service := corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Name:        svc.name,
					Namespace:   cluster.Namespace,
					Labels:      standardLabels(cluster, componentScheduler),
					Annotations: annotations,
				},
				Spec: corev1.ServiceSpec{
					Type:     serviceType,
					Ports:    svc.ports,
					Selector: selector,
				},
			}
			service.Labels[clusterLabel] = cluster.Name

			if err := controllerutil.SetControllerReference(cluster, &service, r.Scheme); err != nil {
				return err
			}
			if err := r.Create(ctx, &service); err != nil {
				return err
			}
			continue
		} else if err != nil {
			return err
		}

		existing.Annotations = annotations
		existing.Spec.Type = serviceType
		existing.Spec.Ports = svc.ports
		existing.Spec.Selector = selector
		if err := r.Update(ctx, &existing); err != nil {
			return err
		}
	}

	return nil
}

func podIsReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// schedulerPodName is stable (no GenerateName): the scheduler is a singleton
// and reconcileScheduler Gets it by name.
func schedulerPodName(cluster *computev1.PolarsCluster) string {
	return fmt.Sprintf("%s-scheduler", cluster.Name)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PolarsClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&computev1.PolarsCluster{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Named("polarscluster").
		Complete(r)
}
