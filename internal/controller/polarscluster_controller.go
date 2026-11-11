package controller

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	computev1 "github.com/polars-inc/polars-k8s-operator/api/v1"
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

	// Keep stable: the API server won't remove fields applied under an old name.
	fieldOwner = client.FieldOwner("polars-operator")

	// conflictRetryInterval is how often a cluster checks again whether an
	// object name taken by something it doesn't control has freed up: that
	// object going away doesn't trigger a reconcile.
	conflictRetryInterval = time.Minute

	maxConcurrentPodCreations = 4

	schedulerPort       = 5051
	observatoryRESTPort = 3001
)

// PolarsClusterReconciler reconciles a PolarsCluster object
type PolarsClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder

	GatewayAPIEnabled bool
}

// +kubebuilder:rbac:groups=compute.pola.rs,resources=polarsclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.pola.rs,resources=polarsclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=compute.pola.rs,resources=polarsclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=grpcroutes;httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
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

	if err := r.reconcilePolarsClusterServiceAccounts(ctx, cluster); err != nil {
		var se *specError
		if errors.As(err, &se) {
			return r.recordSpecError(ctx, cluster, conditionReady, se)
		}
		return ctrl.Result{}, err
	}

	// Before the scheduler, whose autoscaler reads the PolarsCluster on startup.
	if err := r.reconcileScalingRBAC(ctx, cluster); err != nil {
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
	setReadyCondition(cluster, conditionSchedulerReady, schedulerStatus.Ready, "NotReady", schedulerMessage)

	if err := r.reconcileServices(ctx, cluster); err != nil {
		var se *specError
		if errors.As(err, &se) {
			return r.recordSpecError(ctx, cluster, conditionReady, se)
		}
		return ctrl.Result{}, err
	}

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
	setReadyCondition(cluster, conditionWorkerPoolReady, workerPoolReady, "NotReady", workerMessage)

	routes, err := r.reconcileRoutes(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	if routes.requested {
		setReadyCondition(cluster, conditionRoutesReady, routes.ready, routes.reason, routes.message)
	} else {
		meta.RemoveStatusCondition(&cluster.Status.Conditions, conditionRoutesReady)
	}

	// The scheduler blocking readiness is reported first: a down scheduler
	// affects the whole cluster regardless of the worker pool's state.
	var readyMessage string
	switch {
	case !schedulerStatus.Ready:
		readyMessage = schedulerMessage
	case !workerPoolReady:
		readyMessage = workerMessage
	case !routes.ready:
		readyMessage = routes.message
	}
	setReadyCondition(cluster, conditionReady, schedulerStatus.Ready && workerPoolReady && routes.ready, "NotReady", readyMessage)

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

	if routes.conflict {
		return ctrl.Result{RequeueAfter: conflictRetryInterval}, nil
	}
	return ctrl.Result{}, nil
}

// recordSpecError surfaces a spec-caused error as a False condition of
// condType (mirrored onto conditionReady when condType differs) plus a
// Warning Event, and returns nil so the caller doesn't trigger the default
// requeue-with-backoff: only a spec change (or the underlying rejection
// clearing) can fix it, and that already re-triggers reconciliation. A
// conflict is the exception, retried every conflictRetryInterval.
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
	if errors.Is(se, errConflict) {
		return ctrl.Result{RequeueAfter: conflictRetryInterval}, nil
	}
	return ctrl.Result{}, nil
}

// setReadyCondition upserts a True/False condition of the given type on the
// cluster, with reason "Reconciled" when ready. notReadyReason and
// notReadyMessage are dropped when ready, so a condition never carries a
// stale explanation from a prior NotReady state.
func setReadyCondition(cluster *computev1.PolarsCluster, condType string, ready bool, notReadyReason, notReadyMessage string) {
	status := v1.ConditionFalse
	reason := notReadyReason
	message := notReadyMessage
	if ready {
		status = v1.ConditionTrue
		reason = "Reconciled"
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

	// The kubelet never restarts a stopped pod, and its fixed name means it
	// has to be deleted before a replacement can be created.
	if existing.DeletionTimestamp.IsZero() && podIsStopped(&existing) {
		reason := fmt.Sprintf("%s: %s", existing.Status.Phase, podNotReadyMessage(&existing))
		if err := r.Delete(ctx, &existing); err != nil && !apierrors.IsNotFound(err) {
			return computev1.SchedulerStatus{}, "", err
		}
		r.Recorder.Eventf(cluster, nil, corev1.EventTypeWarning, "SchedulerPodRecreating", actionDeletePod, "Deleted stopped scheduler pod %s (%s)", existing.Name, reason)
		return computev1.SchedulerStatus{Ready: false}, "scheduler pod is being recreated after it stopped (" + reason + ")", nil
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
	log := logf.FromContext(ctx)
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
		// Like a ReplicaSet: a stopped pod no longer counts, so it gets
		// replaced, but it is left for inspection until a template change
		// or the cluster's deletion removes it.
		if podIsStopped(&pod) {
			continue
		}
		activeManagedPods = append(activeManagedPods, pod)
	}

	var lackingPodCount = int(wp.Replicas) - len(activeManagedPods)
	if lackingPodCount > 0 {
		created := make([]*corev1.Pod, lackingPodCount)
		errs := make([]error, lackingPodCount)

		workqueue.ParallelizeUntil(ctx, maxConcurrentPodCreations, lackingPodCount, func(i int) {
			pod := podTemplate.DeepCopy()
			if err := r.Create(ctx, pod); err != nil {
				errs[i] = classifyAPIError("WorkerPodRejected", err)
				return
			}
			created[i] = pod
		})

		for _, pod := range created {
			if pod == nil {
				continue
			}
			r.Recorder.Eventf(cluster, nil, corev1.EventTypeNormal, "WorkerPodCreated", actionCreatePod, "Created worker pod %s", pod.Name)
			activeManagedPods = append(activeManagedPods, *pod)
		}
		if setAside, err := reducePodErrors(errs); err != nil {
			for _, se := range setAside {
				log.Error(se, "Could not create worker Pod", "cluster", cluster.Name)
			}
			return computev1.WorkerPoolStatus{}, "", err
		}
	} else if lackingPodCount < 0 {
		var numPodsToDelete = -lackingPodCount
		// Delete workers outside workersToKeep first: the autoscaler keeps the
		// ones running queries there.
		kept := func(pod corev1.Pod) int {
			if slices.Contains(wp.WorkersToKeep, pod.Name) {
				return 1
			}
			return 0
		}
		slices.SortStableFunc(activeManagedPods, func(a, b corev1.Pod) int { return kept(a) - kept(b) })
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
		} else if notReadyPod == nil || cmp.Or(
			pod.CreationTimestamp.Compare(notReadyPod.CreationTimestamp.Time),
			strings.Compare(pod.Name, notReadyPod.Name),
		) < 0 {
			// Report the pod stuck longest. The name tie-break matters: the
			// cache lists pods in map order, and a message that changes
			// between reconciles rewrites status and re-triggers reconciliation.
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
	services := schedulerServices(cluster)

	desired := []struct {
		name   string
		config *computev1.ServiceConfig
		ports  []*corev1ac.ServicePortApplyConfiguration
	}{
		{
			name:   schedulerServiceName(cluster),
			config: serviceConfig(services.Scheduler),
			ports: []*corev1ac.ServicePortApplyConfiguration{
				servicePort("sched", schedulerPort).WithAppProtocol("kubernetes.io/h2c"),
			},
		},
		{
			name:   schedulerInternalServiceName(cluster),
			config: services.Internal,
			ports: []*corev1ac.ServicePortApplyConfiguration{
				servicePort("worker-sched", 5050),
				servicePort("obser-grpc", 5049),
			},
		},
		{
			name:   observatoryServiceName(cluster),
			config: serviceConfig(services.Observatory),
			ports: []*corev1ac.ServicePortApplyConfiguration{
				servicePort("obser-rest", observatoryRESTPort),
			},
		},
	}

	selector := map[string]string{
		clusterLabel:   cluster.Name,
		componentLabel: componentScheduler,
	}

	errs := make([]error, len(desired))
	workqueue.ParallelizeUntil(ctx, len(desired), len(desired), func(i int) {
		svc := desired[i]
		errs[i] = r.reconcileService(ctx, cluster, svc.name, svc.config, svc.ports, selector)
	})
	setAside, err := reducePodErrors(errs)
	for _, se := range setAside {
		logf.FromContext(ctx).Error(se, "Could not reconcile Service", "cluster", cluster.Name)
	}
	return err
}

func (r *PolarsClusterReconciler) reconcileService(ctx context.Context, cluster *computev1.PolarsCluster, name string, config *computev1.ServiceConfig, ports []*corev1ac.ServicePortApplyConfiguration, selector map[string]string) error {
	cfg := ptr.Deref(config, computev1.ServiceConfig{})
	spec := corev1ac.ServiceSpec().
		WithType(corev1.ServiceTypeClusterIP).
		WithPorts(ports...).
		WithSelector(selector).
		WithPublishNotReadyAddresses(ptr.Deref(cfg.PublishNotReadyAddresses, true))
	if cfg.Type != nil && *cfg.Type != "" {
		spec.WithType(*cfg.Type)
	}
	if cfg.ClusterIP != "" {
		spec.WithClusterIP(cfg.ClusterIP)
	}

	service := corev1ac.Service(name, cluster.Namespace).
		WithLabels(schedulerObjectLabels(cluster)).
		WithAnnotations(cfg.Annotations).
		WithOwnerReferences(controllerRef(cluster)).
		WithSpec(spec)
	return r.applyOwned(ctx, cluster, &corev1.Service{ObjectMeta: v1.ObjectMeta{Name: name, Namespace: cluster.Namespace}}, service, "Service")
}

// applyOwned server-side applies ac, so fields others set on obj survive and
// ones dropped from ac are removed. It reads obj into place first and refuses
// to take over one another controller owns. Errors carry the reasons
// <reasonPrefix>Conflict and <reasonPrefix>Rejected.
func (r *PolarsClusterReconciler) applyOwned(ctx context.Context, cluster *computev1.PolarsCluster, obj client.Object, ac runtime.ApplyConfiguration, reasonPrefix string) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); client.IgnoreNotFound(err) != nil {
		return err
	}
	if owner := v1.GetControllerOf(obj); owner != nil && owner.UID != cluster.UID {
		gvk, err := apiutil.GVKForObject(obj, r.Scheme)
		if err != nil {
			return err
		}
		return specErrorf(reasonPrefix+"Conflict", "%s %s is %w: %s %s", gvk.Kind, obj.GetName(), errConflict, owner.Kind, owner.Name)
	}
	return classifyAPIError(reasonPrefix+"Rejected", r.Apply(ctx, ac, fieldOwner, client.ForceOwnership))
}

func controllerRef(cluster *computev1.PolarsCluster) *metav1ac.OwnerReferenceApplyConfiguration {
	return metav1ac.OwnerReference().
		WithAPIVersion(computev1.GroupVersion.String()).
		WithKind("PolarsCluster").
		WithName(cluster.Name).
		WithUID(cluster.UID).
		WithController(true).
		WithBlockOwnerDeletion(true)
}

func servicePort(name string, port int32) *corev1ac.ServicePortApplyConfiguration {
	return corev1ac.ServicePort().WithName(name).WithPort(port).WithProtocol(corev1.ProtocolTCP)
}

func schedulerServices(cluster *computev1.PolarsCluster) computev1.SchedulerServicesSpec {
	if cluster.Spec.Scheduler != nil && cluster.Spec.Scheduler.Services != nil {
		return *cluster.Spec.Scheduler.Services
	}
	return computev1.SchedulerServicesSpec{}
}

func schedulerObjectLabels(cluster *computev1.PolarsCluster) map[string]string {
	labels := standardLabels(cluster, componentScheduler)
	labels[clusterLabel] = cluster.Name
	return labels
}

func serviceConfig(config *computev1.ExposedServiceConfig) *computev1.ServiceConfig {
	if config == nil {
		return nil
	}
	return &config.ServiceConfig
}

func podIsStopped(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
}

func podIsReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// podNotReadyMessage describes why pod isn't Ready, checking the earliest
// stage of pod startup first. The kubelet's PodReady condition comes last
// because it names the unready containers without saying why.
func podNotReadyMessage(pod *corev1.Pod) string {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
			if msg := reasonMessage(cond.Reason, cond.Message); msg != "" {
				return msg
			}
		}
	}
	if pod.Status.Reason != "" {
		return reasonMessage(pod.Status.Reason, pod.Status.Message)
	}
	for _, cs := range pod.Status.InitContainerStatuses {
		if t := cs.State.Terminated; t != nil && t.ExitCode == 0 {
			continue
		}
		if msg := containerNotReadyMessage("init container", &cs); msg != "" {
			return msg
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if msg := containerNotReadyMessage("container", &cs); msg != "" {
			return msg
		}
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue {
			if msg := reasonMessage(cond.Reason, cond.Message); msg != "" {
				return msg
			}
		}
	}
	return "pod is not Ready"
}

// containerNotReadyMessage returns "" for a running container. A waiting
// container's message includes its last termination, since CrashLoopBackOff
// alone doesn't say why the container crashed.
func containerNotReadyMessage(kind string, cs *corev1.ContainerStatus) string {
	switch {
	case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
		msg := fmt.Sprintf("%s %s is waiting: %s", kind, cs.Name, reasonMessage(cs.State.Waiting.Reason, cs.State.Waiting.Message))
		if last := cs.LastTerminationState.Terminated; last != nil {
			msg += fmt.Sprintf("; last terminated: %s", terminatedMessage(last))
		}
		return msg
	case cs.State.Terminated != nil:
		return fmt.Sprintf("%s %s terminated: %s", kind, cs.Name, terminatedMessage(cs.State.Terminated))
	}
	return ""
}

func terminatedMessage(t *corev1.ContainerStateTerminated) string {
	if t.Reason == "" {
		return fmt.Sprintf("exit code %d", t.ExitCode)
	}
	return fmt.Sprintf("%s (exit code %d)", t.Reason, t.ExitCode)
}

func reasonMessage(reason, message string) string {
	switch {
	case reason == "":
		return message
	case message == "":
		return reason
	}
	return reason + ": " + message
}

// schedulerPodName is stable (no GenerateName): the scheduler is a singleton
// and reconcileScheduler Gets it by name.
func schedulerPodName(cluster *computev1.PolarsCluster) string {
	return fmt.Sprintf("%s-scheduler", cluster.Name)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PolarsClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&computev1.PolarsCluster{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{})
	if r.GatewayAPIEnabled {
		builder = builder.
			Owns(&gatewayv1.GRPCRoute{}).
			Owns(&gatewayv1.HTTPRoute{})
	}
	return builder.
		Named("polarscluster").
		Complete(r)
}
