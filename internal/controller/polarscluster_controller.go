package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
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

	conditionReady           = "Ready"
	conditionSchedulerReady  = "SchedulerReady"
	conditionWorkerPoolReady = "WorkerPoolReady"

	// Event actions: what the controller did to the cluster it reports on.
	actionReconcile = "Reconcile"
	actionCreatePod = "CreatePod"
	actionDeletePod = "DeletePod"
)

// PolarsClusterReconciler reconciles a PolarsCluster object
type PolarsClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=compute.pola.rs,resources=polarsclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.pola.rs,resources=polarsclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=compute.pola.rs,resources=polarsclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

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

	wasReady := meta.IsStatusConditionTrue(cluster.Status.Conditions, conditionReady)

	// Fail fast on an uninterpretable version before composing any pod spec
	// from it. No requeue: only a spec change can fix it.
	if _, err := clusterVersion(cluster); err != nil {
		return r.recordSpecError(ctx, cluster, conditionReady, specErrorf("InvalidVersion", "%s", err.Error()))
	}

	if err := r.reconcileServices(ctx, cluster); err != nil {
		var se *specError
		if errors.As(err, &se) {
			return r.recordSpecError(ctx, cluster, conditionReady, se)
		}
		return ctrl.Result{}, err
	}

	if err := r.reconcilePolarsClusterServiceAccounts(ctx, cluster); err != nil {
		var se *specError
		if errors.As(err, &se) {
			return r.recordSpecError(ctx, cluster, conditionReady, se)
		}
		return ctrl.Result{}, err
	}

	schedulerStatus, schedulerMessage, err := r.reconcileScheduler(ctx, cluster)
	if err != nil {
		var se *specError
		if errors.As(err, &se) {
			return r.recordSpecError(ctx, cluster, conditionSchedulerReady, se)
		}
		return ctrl.Result{}, err
	}
	cluster.Status.Scheduler = schedulerStatus
	setReadyCondition(cluster, conditionSchedulerReady, schedulerStatus.Ready, "Reconciled", "NotReady", schedulerMessage)

	workerPoolStatus, workerMessage, err := r.reconcileWorkerPool(ctx, cluster)
	if err != nil {
		var se *specError
		if errors.As(err, &se) {
			return r.recordSpecError(ctx, cluster, conditionWorkerPoolReady, se)
		}
		return ctrl.Result{}, err
	}
	cluster.Status.WorkerPool = workerPoolStatus
	workerPoolReady := workerPoolStatus.Replicas == cluster.Spec.WorkerPool.Replicas &&
		workerPoolStatus.ReadyReplicas == workerPoolStatus.Replicas
	setReadyCondition(cluster, conditionWorkerPoolReady, workerPoolReady, "Reconciled", "NotReady", workerMessage)

	// The scheduler blocking readiness is reported first: a down scheduler
	// affects the whole cluster regardless of the worker pool's state.
	readyMessage := schedulerMessage
	if schedulerStatus.Ready {
		readyMessage = workerMessage
	}
	setReadyCondition(cluster, conditionReady, schedulerStatus.Ready && workerPoolReady, "Reconciled", "NotReady", readyMessage)

	cluster.Status.ObservedGeneration = cluster.Generation

	if err := r.Status().Update(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}

	switch nowReady := meta.IsStatusConditionTrue(cluster.Status.Conditions, conditionReady); {
	case !wasReady && nowReady:
		r.Recorder.Eventf(cluster, nil, corev1.EventTypeNormal, "Reconciled", actionReconcile, "PolarsCluster is Ready")
	case wasReady && !nowReady:
		r.Recorder.Eventf(cluster, nil, corev1.EventTypeWarning, "NotReady", actionReconcile, "%s", readyMessage)
	}

	return ctrl.Result{}, nil
}

// recordSpecError surfaces a spec-caused error as a False condition of
// condType (mirrored onto conditionReady when condType differs) plus a
// Warning Event, and returns nil so the caller doesn't trigger the default
// requeue-with-backoff: only a spec change (or the underlying rejection
// clearing) can fix it, and that already re-triggers reconciliation.
func (r *PolarsClusterReconciler) recordSpecError(ctx context.Context, cluster *computev1.PolarsCluster, condType string, se *specError) (ctrl.Result, error) {
	meta.SetStatusCondition(&cluster.Status.Conditions, v1.Condition{
		Type:               condType,
		Status:             v1.ConditionFalse,
		Reason:             se.reason,
		Message:            se.Error(),
		ObservedGeneration: cluster.Generation,
	})
	if condType != conditionReady {
		meta.SetStatusCondition(&cluster.Status.Conditions, v1.Condition{
			Type:               conditionReady,
			Status:             v1.ConditionFalse,
			Reason:             se.reason,
			Message:            se.Error(),
			ObservedGeneration: cluster.Generation,
		})
	}
	r.Recorder.Eventf(cluster, nil, corev1.EventTypeWarning, se.reason, actionReconcile, "%s", se.Error())

	cluster.Status.ObservedGeneration = cluster.Generation
	if err := r.Status().Update(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// setReadyCondition upserts a True/False condition of the given type on the
// cluster, choosing the reason based on whether ready is true. notReadyMessage
// is dropped when ready, so a condition never carries a stale explanation
// from a prior NotReady state.
func setReadyCondition(cluster *computev1.PolarsCluster, condType string, ready bool, readyReason, notReadyReason, notReadyMessage string) {
	status := v1.ConditionFalse
	reason := notReadyReason
	message := notReadyMessage
	if ready {
		status = v1.ConditionTrue
		reason = readyReason
		message = ""
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, v1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
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
// reports whether it's currently Ready plus a message explaining why when
// it's not.
func (r *PolarsClusterReconciler) reconcileScheduler(ctx context.Context, cluster *computev1.PolarsCluster) (computev1.SchedulerStatus, string, error) {
	pod, err := BuildSchedulerPodTemplate(cluster)
	if err != nil {
		return computev1.SchedulerStatus{}, "", err
	}
	hash, err := podTemplateHash(&pod.Spec)
	if err != nil {
		return computev1.SchedulerStatus{}, "", err
	}
	pod.Labels[templateHashLabel] = hash

	var existing corev1.Pod
	err = r.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, &existing)
	if apierrors.IsNotFound(err) {
		if err := controllerutil.SetControllerReference(cluster, &pod, r.Scheme); err != nil {
			return computev1.SchedulerStatus{}, "", err
		}

		if err := r.Create(ctx, &pod); err != nil {
			return computev1.SchedulerStatus{}, "", classifyAPIError("SchedulerPodRejected", err)
		}
		r.Recorder.Eventf(cluster, nil, corev1.EventTypeNormal, "SchedulerPodCreated", actionCreatePod, "Created scheduler pod %s", pod.Name)

		return computev1.SchedulerStatus{Ready: false}, "scheduler pod was just created", nil
	} else if err != nil {
		return computev1.SchedulerStatus{}, "", err
	}

	if existing.DeletionTimestamp.IsZero() && existing.Labels[templateHashLabel] != hash {
		if err := r.Delete(ctx, &existing); err != nil && !apierrors.IsNotFound(err) {
			return computev1.SchedulerStatus{}, "", err
		}
		r.Recorder.Eventf(cluster, nil, corev1.EventTypeNormal, "SchedulerPodRecreating", actionDeletePod, "Deleted scheduler pod %s for a template change", existing.Name)
		// recreated on the next reconcile once the old pod is gone
		return computev1.SchedulerStatus{Ready: false}, "scheduler pod is being recreated for a template change", nil
	}

	if podIsReady(&existing) {
		return computev1.SchedulerStatus{Ready: true}, "", nil
	}
	return computev1.SchedulerStatus{Ready: false}, podNotReadyMessage(&existing), nil
}

// reconcileWorkerPool creates/deletes pods for cluster.Spec.WorkerPool to
// match its desired replica count, recreating pods whose computed template
// changed, and returns the resulting observed status plus a message
// explaining why the pool isn't fully ready when it isn't.
func (r *PolarsClusterReconciler) reconcileWorkerPool(ctx context.Context, cluster *computev1.PolarsCluster) (computev1.WorkerPoolStatus, string, error) {
	wp := &cluster.Spec.WorkerPool

	podTemplate, err := BuildWorkerPodTemplate(cluster)
	if err != nil {
		return computev1.WorkerPoolStatus{}, "", err
	}
	hash, err := podTemplateHash(&podTemplate.Spec)
	if err != nil {
		return computev1.WorkerPoolStatus{}, "", err
	}
	podTemplate.Labels[templateHashLabel] = hash

	if err := controllerutil.SetControllerReference(cluster, &podTemplate, r.Scheme); err != nil {
		return computev1.WorkerPoolStatus{}, "", err
	}

	// we may temporarily have more pods than allowed due to not waiting for the grace period
	var managedPods corev1.PodList
	if err := r.List(ctx, &managedPods,
		client.MatchingLabels{clusterLabel: cluster.Name, componentLabel: componentWorker},
		client.InNamespace(cluster.Namespace),
	); err != nil {
		return computev1.WorkerPoolStatus{}, "", err
	}

	var activeManagedPods []corev1.Pod
	for _, pod := range managedPods.Items {
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}
		if pod.Labels[templateHashLabel] != hash {
			if err := r.Delete(ctx, &pod); err != nil && !apierrors.IsNotFound(err) {
				return computev1.WorkerPoolStatus{}, "", err
			}
			r.Recorder.Eventf(cluster, nil, corev1.EventTypeNormal, "WorkerPodRecreating", actionDeletePod, "Deleted worker pod %s for a template change", pod.Name)
			continue
		}
		activeManagedPods = append(activeManagedPods, pod)
	}

	var lackingPodCount = int(wp.Replicas) - len(activeManagedPods)
	if lackingPodCount > 0 {
		for range lackingPodCount {
			pod := podTemplate.DeepCopy()

			if err := r.Create(ctx, pod); err != nil {
				return computev1.WorkerPoolStatus{}, "", classifyAPIError("WorkerPodRejected", err)
			}
			r.Recorder.Eventf(cluster, nil, corev1.EventTypeNormal, "WorkerPodCreated", actionCreatePod, "Created worker pod %s", pod.Name)

			activeManagedPods = append(activeManagedPods, *pod)
		}
	} else if lackingPodCount < 0 {
		var numPodsToDelete = -lackingPodCount
		for i := range numPodsToDelete {
			pod := &activeManagedPods[i]
			if err := r.Delete(ctx, pod); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return computev1.WorkerPoolStatus{}, "", err
			}
			r.Recorder.Eventf(cluster, nil, corev1.EventTypeNormal, "WorkerPodDeleted", actionDeletePod, "Deleted worker pod %s", pod.Name)
		}
		activeManagedPods = activeManagedPods[numPodsToDelete:]
	}

	var readyReplicas int32
	var notReadyPod *corev1.Pod
	for i := range activeManagedPods {
		pod := &activeManagedPods[i]
		if podIsReady(pod) {
			readyReplicas++
		} else if notReadyPod == nil {
			notReadyPod = pod
		}
	}

	status := computev1.WorkerPoolStatus{
		Replicas:      int32(len(activeManagedPods)),
		ReadyReplicas: readyReplicas,
		Selector:      fmt.Sprintf("%s=%s,%s=%s", clusterLabel, cluster.Name, componentLabel, componentWorker),
	}

	message := ""
	switch {
	case status.Replicas != wp.Replicas:
		message = fmt.Sprintf("%d/%d desired worker pods exist", status.Replicas, wp.Replicas)
	case notReadyPod != nil:
		message = fmt.Sprintf("worker pod %s: %s", notReadyPod.Name, podNotReadyMessage(notReadyPod))
	}

	return status, message, nil
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
		if apierrors.IsNotFound(err) {
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
				return classifyAPIError("ServiceRejected", err)
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
			return classifyAPIError("ServiceRejected", err)
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

// podNotReadyMessage describes why pod isn't Ready, preferring the kubelet's
// own PodReady condition (e.g. "containers with unready status: [...]"),
// falling back to a not-ready container's waiting/terminated reason.
func podNotReadyMessage(pod *corev1.Pod) string {
	for _, cond := range pod.Status.Conditions {
		if cond.Type != corev1.PodReady || cond.Status == corev1.ConditionTrue {
			continue
		}
		if cond.Message != "" {
			return cond.Message
		}
		if cond.Reason != "" {
			return cond.Reason
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return fmt.Sprintf("container %s is waiting: %s", cs.Name, cs.State.Waiting.Reason)
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
			return fmt.Sprintf("container %s terminated: %s", cs.Name, cs.State.Terminated.Reason)
		}
	}
	return "pod is not Ready"
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
