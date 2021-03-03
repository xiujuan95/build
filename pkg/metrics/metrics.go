// Copyright The Shipwright Contributors
//
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shipwright-io/build/pkg/config"
	"github.com/shipwright-io/build/pkg/ctxlog"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"net/http"
	"os"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"time"
)

// Labels used in Prometheus metrics
const (
	BuildStrategyLabel  string = "buildstrategy"
	NamespaceLabel      string = "namespace"
	BuildLabel          string = "build"
	BuildRunLabel       string = "buildrun"
	BuildControllerName string = "shipwright-build-controller"
	// PodNameEnvVar is the constant for env variable POD_NAME
	// which is the name of the current pod.
	PodNameEnvVar = "POD_NAME"
	// ControllerPortName defines the default controller metrics port name used in the metrics Service.
	ControllerPortName = "http-metrics"
	// CRPortName defines the custom resource specific metrics' port name used in the metrics Service.
	CRPortName = "cr-metrics"
)

var (
	buildCount    *prometheus.CounterVec
	buildRunCount *prometheus.CounterVec

	buildRunEstablishDuration  *prometheus.HistogramVec
	buildRunCompletionDuration *prometheus.HistogramVec

	buildRunRampUpDuration   *prometheus.HistogramVec
	taskRunRampUpDuration    *prometheus.HistogramVec
	taskRunPodRampUpDuration *prometheus.HistogramVec

	buildStrategyLabelEnabled = false
	namespaceLabelEnabled     = false
	buildLabelEnabled         = false
	buildRunLabelEnabled      = false

	initialized = false
)

// Optional additional metrics endpoint handlers to be configured
var metricsExtraHandlers = map[string]http.HandlerFunc{}

// InitPrometheus initializes the prometheus stuff
func InitPrometheus(config *config.Config) {
	if initialized {
		return
	}

	initialized = true

	var buildLabels []string
	var buildRunLabels []string
	if contains(config.Prometheus.EnabledLabels, BuildStrategyLabel) {
		buildLabels = append(buildLabels, BuildStrategyLabel)
		buildRunLabels = append(buildRunLabels, BuildStrategyLabel)
		buildStrategyLabelEnabled = true
	}
	if contains(config.Prometheus.EnabledLabels, NamespaceLabel) {
		buildLabels = append(buildLabels, NamespaceLabel)
		buildRunLabels = append(buildRunLabels, NamespaceLabel)
		namespaceLabelEnabled = true
	}
	if contains(config.Prometheus.EnabledLabels, BuildLabel) {
		buildLabels = append(buildLabels, BuildLabel)
		buildRunLabels = append(buildRunLabels, BuildLabel)
		buildLabelEnabled = true
	}
	if contains(config.Prometheus.EnabledLabels, BuildRunLabel) {
		buildRunLabels = append(buildRunLabels, BuildRunLabel)
		buildRunLabelEnabled = true
	}

	buildCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "build_builds_registered_total",
			Help: "Number of total registered Builds.",
		},
		buildLabels)

	buildRunCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "build_buildruns_completed_total",
			Help: "Number of total completed BuildRuns.",
		},
		buildRunLabels)

	buildRunEstablishDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "build_buildrun_establish_duration_seconds",
			Help:    "BuildRun establish duration in seconds.",
			Buckets: config.Prometheus.BuildRunEstablishDurationBuckets,
		},
		buildRunLabels)

	buildRunCompletionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "build_buildrun_completion_duration_seconds",
			Help:    "BuildRun completion duration in seconds.",
			Buckets: config.Prometheus.BuildRunCompletionDurationBuckets,
		},
		buildRunLabels)

	buildRunRampUpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "build_buildrun_rampup_duration_seconds",
			Help:    "BuildRun ramp-up duration in seconds (time between buildrun creation and taskrun creation).",
			Buckets: config.Prometheus.BuildRunRampUpDurationBuckets,
		},
		buildRunLabels)

	taskRunRampUpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "build_buildrun_taskrun_rampup_duration_seconds",
			Help:    "BuildRun taskrun ramp-up duration in seconds (time between taskrun creation and taskrun pod creation).",
			Buckets: config.Prometheus.BuildRunRampUpDurationBuckets,
		},
		buildRunLabels)

	taskRunPodRampUpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "build_buildrun_taskrun_pod_rampup_duration_seconds",
			Help:    "BuildRun taskrun pod ramp-up duration in seconds (time between pod creation and last init container completion).",
			Buckets: config.Prometheus.BuildRunRampUpDurationBuckets,
		},
		buildRunLabels)

	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		buildCount,
		buildRunCount,
		buildRunEstablishDuration,
		buildRunCompletionDuration,
		buildRunRampUpDuration,
		taskRunRampUpDuration,
		taskRunPodRampUpDuration,
	)
}

// ExtraHandlers returns a mapping of paths and their respective
// additional HTTP handlers to be configured at the metrics listener
func ExtraHandlers() map[string]http.HandlerFunc {
	return metricsExtraHandlers
}

func contains(slice []string, element string) bool {
	for _, candidate := range slice {
		if candidate == element {
			return true
		}
	}
	return false
}

func createBuildLabels(buildStrategy string, namespace string, build string) prometheus.Labels {
	labels := prometheus.Labels{}

	if buildStrategyLabelEnabled {
		labels[BuildStrategyLabel] = buildStrategy
	}
	if namespaceLabelEnabled {
		labels[NamespaceLabel] = namespace
	}
	if buildLabelEnabled {
		labels[BuildLabel] = build
	}

	return labels
}

func createBuildRunLabels(buildStrategy string, namespace string, build string, buildRun string) prometheus.Labels {
	labels := prometheus.Labels{}

	if buildStrategyLabelEnabled {
		labels[BuildStrategyLabel] = buildStrategy
	}
	if namespaceLabelEnabled {
		labels[NamespaceLabel] = namespace
	}
	if buildLabelEnabled {
		labels[BuildLabel] = build
	}
	if buildRunLabelEnabled {
		labels[BuildRunLabel] = buildRun
	}

	return labels
}

// BuildCountInc increases a number of the existing build total count
func BuildCountInc(buildStrategy string, namespace string, build string) {
	if buildCount != nil {
		buildCount.With(createBuildLabels(buildStrategy, namespace, build)).Inc()
	}
}

// BuildRunCountInc increases a number of the existing build run total count
func BuildRunCountInc(buildStrategy string, namespace string, build string, buildRun string) {
	if buildRunCount != nil {
		buildRunCount.With(createBuildRunLabels(buildStrategy, namespace, build, buildRun)).Inc()
	}
}

// BuildRunEstablishObserve sets the build run establish time
func BuildRunEstablishObserve(buildStrategy string, namespace string, build string, buildRun string, duration time.Duration) {
	if buildRunEstablishDuration != nil {
		buildRunEstablishDuration.With(createBuildRunLabels(buildStrategy, namespace, build, buildRun)).Observe(duration.Seconds())
	}
}

// BuildRunCompletionObserve sets the build run completion time
func BuildRunCompletionObserve(buildStrategy string, namespace string, build string, buildRun string, duration time.Duration) {
	if buildRunCompletionDuration != nil {
		buildRunCompletionDuration.With(createBuildRunLabels(buildStrategy, namespace, build, buildRun)).Observe(duration.Seconds())
	}
}

// BuildRunRampUpDurationObserve processes the observation of a new buildrun ramp-up duration
func BuildRunRampUpDurationObserve(buildStrategy string, namespace string, build string, buildRun string, duration time.Duration) {
	if buildRunRampUpDuration != nil {
		buildRunRampUpDuration.With(createBuildRunLabels(buildStrategy, namespace, build, buildRun)).Observe(duration.Seconds())
	}
}

// TaskRunRampUpDurationObserve processes the observation of a new taskrun ramp-up duration
func TaskRunRampUpDurationObserve(buildStrategy string, namespace string, build string, buildRun string, duration time.Duration) {
	if taskRunRampUpDuration != nil {
		taskRunRampUpDuration.With(createBuildRunLabels(buildStrategy, namespace, build, buildRun)).Observe(duration.Seconds())
	}
}

// TaskRunPodRampUpDurationObserve processes the observation of a new taskrun pod ramp-up duration
func TaskRunPodRampUpDurationObserve(buildStrategy string, namespace string, build string, buildRun string, duration time.Duration) {
	if taskRunPodRampUpDuration != nil {
		taskRunPodRampUpDuration.With(createBuildRunLabels(buildStrategy, namespace, build, buildRun)).Observe(duration.Seconds())
	}
}

// CreateMetricsService creates a Kubernetes Service to expose the passed metrics
// port(s) with the given name(s).
func CreateMetricsService(ctx context.Context, cfg *rest.Config, buildCfg *config.Config, servicePorts []v1.ServicePort) (*v1.Service, error) {
	if len(servicePorts) < 1 {
		return nil, fmt.Errorf("failed to create metrics Serice; service ports were empty")
	}
	client, err := crclient.New(cfg, crclient.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create new client: %w", err)
	}

	label := map[string]string{"name": BuildControllerName}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-metrics", BuildControllerName),
			Namespace: buildCfg.ManagerOptions.LeaderElectionNamespace,
			Labels:    label,
		},
		Spec: v1.ServiceSpec{
			Ports:    servicePorts,
			Selector: label,
		},
	}

	ownRef, err := getPodOwnerRef(ctx, client, buildCfg.ManagerOptions.LeaderElectionNamespace)
	if err != nil {
		return nil, err
	}
	service.SetOwnerReferences([]metav1.OwnerReference{*ownRef})

	service, err = createOrUpdateService(ctx, client, buildCfg, service)
	if err != nil {
		return nil, fmt.Errorf("failed to create or get service for metrics: %w", err)
	}

	return service, nil
}

func createOrUpdateService(ctx context.Context, client crclient.Client, buildCfg *config.Config, s *v1.Service) (*v1.Service, error) {
	if err := client.Create(ctx, s); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		// Service already exists, we want to update it
		// as we do not know if any fields might have changed.
		existingService := &v1.Service{}
		err := client.Get(ctx, types.NamespacedName{
			Name:      s.Name,
			Namespace: s.Namespace,
		}, existingService)
		if err != nil {
			return nil, err
		}

		s.ResourceVersion = existingService.ResourceVersion
		if existingService.Spec.Type == v1.ServiceTypeClusterIP {
			s.Spec.ClusterIP = existingService.Spec.ClusterIP
		}
		err = client.Update(ctx, s)
		if err != nil {
			return nil, err
		}
		ctxlog.Info(ctx, "Metrics Service object updated", "Service.Name",
			s.Name, "Service.Namespace", s.Namespace)
		return s, nil
	}

	ctxlog.Info(ctx, "Metrics Service object created", "Service.Name",
		s.Name, "Service.Namespace", s.Namespace)
	return s, nil
}

func getPodOwnerRef(ctx context.Context, client crclient.Client, ns string) (*metav1.OwnerReference, error) {
	// Get current Pod the controller is running in
	podName := os.Getenv(PodNameEnvVar)
	if podName == "" {
		return nil, fmt.Errorf("required env %s not set, please configure downward API", PodNameEnvVar)
	}

	pod := &corev1.Pod{}
	key := crclient.ObjectKey{Namespace: ns, Name: podName}
	err := client.Get(ctx, key, pod)
	if err != nil {
		ctxlog.Error(ctx, err, "Failed to get Pod", "Pod.Namespace", ns, "Pod.Name", podName)
		return nil, err
	}

	// .Get() clears the APIVersion and Kind,
	// so we need to set them before returning the object.
	pod.TypeMeta.APIVersion = "v1"
	pod.TypeMeta.Kind = "Pod"

	podOwnerRefs := metav1.NewControllerRef(pod, pod.GroupVersionKind())
	// Get Owner that the Pod belongs to
	ownerRef := metav1.GetControllerOf(pod)
	finalOwnerRef, err := findFinalOwnerRef(ctx, client, ns, ownerRef)
	if err != nil {
		return nil, err
	}
	if finalOwnerRef != nil {
		return finalOwnerRef, nil
	}

	// Default to returning Pod as the Owner
	return podOwnerRefs, nil
}

// findFinalOwnerRef tries to locate the final controller/owner based on the owner reference provided.
func findFinalOwnerRef(ctx context.Context, client crclient.Client, ns string,
	ownerRef *metav1.OwnerReference) (*metav1.OwnerReference, error) {
	if ownerRef == nil {
		return nil, nil
	}

	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(ownerRef.APIVersion)
	obj.SetKind(ownerRef.Kind)
	err := client.Get(ctx, types.NamespacedName{Namespace: ns, Name: ownerRef.Name}, obj)
	if err != nil {
		return nil, err
	}
	newOwnerRef := metav1.GetControllerOf(obj)
	if newOwnerRef != nil {
		return findFinalOwnerRef(ctx, client, ns, newOwnerRef)
	}
	return ownerRef, nil
}
