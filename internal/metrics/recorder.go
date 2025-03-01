package metrics

import (
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

type Recorder struct {
	MetricTimeseriesBatchesReceived        prometheus.Counter
	MetricTimeseriesBatchesReceivedBytes   prometheus.Histogram
	MetricTimeseriesReceived               *prometheus.CounterVec
	MetricTimeseriesRequestDurationSeconds *prometheus.HistogramVec
	MetricTimeseriesRequestErrors          *prometheus.CounterVec
	MetricTimeseriesRequests               *prometheus.CounterVec
}

func MustMakeRecorder() *Recorder {
	metricsRecorder := NewRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewRecorder() *Recorder {
	return &Recorder{
		MetricTimeseriesBatchesReceived: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "cortex_tenant",
				Name:      "timeseries_batches_received_total",
				Help:      "The total number of batches received.",
			},
		),
		MetricTimeseriesBatchesReceivedBytes: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "cortex_tenant",
				Name:      "timeseries_batches_received_bytes",
				Help:      "Size in bytes of timeseries batches received.",
				Buckets:   []float64{0.5, 1, 10, 25, 100, 250, 500, 1000, 5000, 10000, 30000, 300000, 600000, 1800000, 3600000},
			},
		),
		MetricTimeseriesReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "cortex_tenant",
				Name:      "timeseries_received_total",
				Help:      "The total number of timeseries received.",
			},
			[]string{"tenant"},
		),
		MetricTimeseriesRequestDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "cortex_tenant",
				Name:      "timeseries_request_duration_seconds",
				Help:      "HTTP write request duration for tenant-specific timeseries in seconds, filtered by response code.",
				Buckets:   []float64{0.5, 1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000, 1800000, 3600000},
			},
			[]string{"code", "tenant"},
		),
		MetricTimeseriesRequestErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "cortex_tenant",
				Name:      "timeseries_request_errors_total",
				Help:      "The total number of tenant-specific timeseries writes that yielded errors.",
			},
			[]string{"tenant"},
		),
		MetricTimeseriesRequests: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "cortex_tenant",
				Name:      "timeseries_requests_total",
				Help:      "The total number of tenant-specific timeseries writes.",
			},
			[]string{"tenant"},
		),
	}
}

func (r *Recorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.MetricTimeseriesBatchesReceived,
		r.MetricTimeseriesBatchesReceivedBytes,
		r.MetricTimeseriesReceived,
		r.MetricTimeseriesRequestDurationSeconds,
		r.MetricTimeseriesRequestErrors,
		r.MetricTimeseriesRequests,
	}
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *Recorder) DeleteMetricsForTenant(tenant *capsulev1beta2.Tenant) {
	r.MetricTimeseriesRequests.DeleteLabelValues(tenant.Name)
	r.MetricTimeseriesRequestDurationSeconds.DeleteLabelValues(tenant.Name)
	r.MetricTimeseriesRequestErrors.DeleteLabelValues(tenant.Name)
	r.MetricTimeseriesRequests.DeleteLabelValues(tenant.Name)
}
