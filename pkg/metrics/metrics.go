// Package metrics provides Prometheus metrics instrumentation for the canopy orchestration platform.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "canopy"

var (
	// TaskDuration tracks the duration of task executions in seconds.
	TaskDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "task_duration_seconds",
			Help:      "Duration of task executions in seconds",
			Buckets:   []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
		},
		[]string{"status"},
	)

	// MergeQueueDepth tracks the current number of items waiting in the merge queue.
	MergeQueueDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "merge_queue_depth",
			Help:      "Current number of items waiting in the merge queue",
		},
	)

	// ActiveAgents tracks the current number of active agents by status.
	ActiveAgents = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "active_agents",
			Help:      "Current number of agents by status",
		},
		[]string{"status"},
	)

	// OverlayMounts tracks the current number of active overlay filesystem mounts.
	OverlayMounts = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "overlay_mounts",
			Help:      "Current number of active overlay filesystem mounts",
		},
	)

	// MergeConflictsTotal tracks the total number of merge conflicts encountered.
	MergeConflictsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "merge_conflicts_total",
			Help:      "Total number of merge conflicts encountered",
		},
	)

	// ResolverSuccessTotal tracks the number of successfully resolved conflicts.
	ResolverSuccessTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "resolver_success_total",
			Help:      "Total number of conflicts successfully resolved",
		},
	)

	// ResolverFailureTotal tracks the number of failed resolution attempts.
	ResolverFailureTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "resolver_failure_total",
			Help:      "Total number of failed conflict resolutions",
		},
	)

	// ResolverDurationSeconds tracks the duration of resolver executions.
	ResolverDurationSeconds = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "resolver_duration_seconds",
			Help:      "Duration of resolver executions in seconds",
			Buckets:   []float64{10, 30, 60, 120, 300, 600, 900, 1200},
		},
	)

	// IPCMessagesTotal tracks the total number of IPC messages processed by type.
	IPCMessagesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "ipc_messages_total",
			Help:      "Total number of IPC messages processed",
		},
		[]string{"type"},
	)
)

// RecordTaskDuration records a task duration with the given status.
func RecordTaskDuration(durationSeconds float64, status string) {
	TaskDuration.WithLabelValues(status).Observe(durationSeconds)
}

// SetMergeQueueDepth sets the current merge queue depth.
func SetMergeQueueDepth(depth int) {
	MergeQueueDepth.Set(float64(depth))
}

// SetActiveAgents sets the count of active agents for a given status.
func SetActiveAgents(status string, count int) {
	ActiveAgents.WithLabelValues(status).Set(float64(count))
}

// SetOverlayMounts sets the current overlay mount count.
func SetOverlayMounts(count int) {
	OverlayMounts.Set(float64(count))
}

// IncOverlayMounts increments the overlay mount count by 1.
func IncOverlayMounts() {
	OverlayMounts.Inc()
}

// DecOverlayMounts decrements the overlay mount count by 1.
func DecOverlayMounts() {
	OverlayMounts.Dec()
}

// IncMergeConflicts increments the merge conflicts counter.
func IncMergeConflicts() {
	MergeConflictsTotal.Inc()
}

// AddMergeConflicts increments the merge conflicts counter by the given amount.
func AddMergeConflicts(count int) {
	MergeConflictsTotal.Add(float64(count))
}

// IncResolverSuccess increments the resolver success counter.
func IncResolverSuccess() {
	ResolverSuccessTotal.Inc()
}

// IncResolverFailure increments the resolver failure counter.
func IncResolverFailure() {
	ResolverFailureTotal.Inc()
}

// ObserveResolverDuration records a resolver execution duration.
func ObserveResolverDuration(durationSeconds float64) {
	ResolverDurationSeconds.Observe(durationSeconds)
}

// IncIPCMessages increments the IPC messages counter for the given message type.
func IncIPCMessages(msgType string) {
	IPCMessagesTotal.WithLabelValues(msgType).Inc()
}
