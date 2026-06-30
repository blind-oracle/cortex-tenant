package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	me "github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	fh "github.com/valyala/fasthttp"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

var (
	metricOTLPMetricsBatchesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_metrics_batches_received",
		Help:      "The total number of OTLP metrics batches received.",
	})
	metricOTLPMetricsBatchesReceivedBytes = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_metrics_batches_received_bytes",
		Help:      "Size in bytes of OTLP metrics batches received.",
		Buckets:   []float64{0.5, 1, 10, 25, 100, 250, 500, 1000, 5000, 10000, 30000, 300000, 600000, 1800000, 3600000},
	})
	metricOTLPDatapointsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_metrics_datapoints_received",
		Help:      "The total number of OTLP metric data points received.",
	}, []string{"tenant"})
	metricOTLPMetricsRequestDurationMilliseconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_metrics_request_duration_milliseconds",
		Help:      "HTTP write request duration for tenant-specific OTLP metrics in milliseconds, filtered by response code.",
		Buckets:   []float64{0.5, 1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000, 1800000, 3600000},
	},
		[]string{"code", "tenant"},
	)
	metricOTLPMetricsRequestErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_metrics_request_errors",
		Help:      "The total number of tenant-specific OTLP metrics writes that yielded errors.",
	}, []string{"tenant"})
	metricOTLPMetricsRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_metrics_requests",
		Help:      "The total number of tenant-specific OTLP metrics writes.",
	}, []string{"tenant"})
)

// handleOTLPMetrics receives OTLP/HTTP metrics on /v1/metrics, splits the
// ResourceMetrics by tenant (based on a resource attribute) and forwards them
// to an OTLP-capable backend (e.g. Grafana Mimir / Cortex) with the relevant
// per-tenant header set.
func (p *processor) handleOTLPMetrics(ctx *fh.RequestCtx) {
	metricOTLPMetricsBatchesReceivedBytes.Observe(float64(ctx.Request.Header.ContentLength()))
	metricOTLPMetricsBatchesReceived.Inc()

	md, err := p.unmarshalOTLPMetrics(ctx)
	if err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)
		return
	}

	tenantPrefix := p.cfg.Tenant.Prefix

	if p.cfg.Tenant.PrefixPreferSource {
		sourceTenantPrefix := string(ctx.Request.Header.Peek(p.cfg.Tenant.Header))
		if sourceTenantPrefix != "" {
			tenantPrefix = sourceTenantPrefix + "-"
		}
	}

	clientIP := ctx.RemoteAddr()
	reqID, _ := uuid.NewRandom()

	if md.ResourceMetrics().Len() == 0 {
		ctx.Error("No resource metrics found in the request", fh.StatusBadRequest)
		return
	}

	m, err := p.createOTLPWriteRequests(md)
	if err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)
		return
	}

	metricTenant := ""
	var errs *me.Error
	results := p.dispatch(p.cfg.TargetOTLP, clientIP, reqID, tenantPrefix, m, p.otlpEgressContentType())

	code, body := 0, []byte("Ok")

	// Return 204 regardless of errors if AcceptAll is enabled
	if p.cfg.Tenant.AcceptAll {
		code, body = 204, nil
		goto out
	}

	for _, r := range results {
		if p.cfg.MetricsIncludeTenant {
			metricTenant = r.tenant
		}

		metricOTLPMetricsRequests.WithLabelValues(metricTenant).Inc()

		if r.err != nil {
			metricOTLPMetricsRequestErrors.WithLabelValues(metricTenant).Inc()
			errs = me.Append(errs, r.err)
			p.Errorf("src=%s %s", clientIP, r.err)
			continue
		}

		if r.code < 200 || r.code >= 300 {
			if p.cfg.LogResponseErrors {
				p.Errorf("src=%s req_id=%s HTTP code %d (%s)", clientIP, reqID, r.code, string(r.body))
			}
		}

		if r.code > code {
			code, body = r.code, r.body
		}

		metricOTLPMetricsRequestDurationMilliseconds.WithLabelValues(strconv.Itoa(r.code), metricTenant).Observe(r.duration)
	}

	if errs.ErrorOrNil() != nil {
		ctx.Error(errs.Error(), fh.StatusInternalServerError)
		return
	}

out:
	// Pass back max status code from upstream response
	ctx.SetBody(body)
	ctx.SetStatusCode(code)
}

// createOTLPWriteRequests groups the incoming ResourceMetrics by tenant and
// returns one marshal closure per tenant. The resource metrics are partitioned
// whole, so all scopes/metrics/datapoints under a resource follow that
// resource's tenant.
func (p *processor) createOTLPWriteRequests(md pmetric.Metrics) (map[string]func() ([]byte, error), error) {
	perTenant := map[string]pmetric.Metrics{}

	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)

		tenant, err := p.processOTLPResourceMetrics(rm)
		if err != nil {
			return nil, err
		}

		out, ok := perTenant[tenant]
		if !ok {
			out = pmetric.NewMetrics()
			perTenant[tenant] = out
		}

		rm.CopyTo(out.ResourceMetrics().AppendEmpty())
	}

	// Marshal results
	marshaler := &pmetric.ProtoMarshaler{}
	resM := make(map[string]func() ([]byte, error), len(perTenant))
	for tenant, out := range perTenant {
		if p.cfg.MetricsIncludeTenant {
			metricOTLPDatapointsReceived.WithLabelValues(tenant).Add(float64(out.DataPointCount()))
		} else {
			metricOTLPDatapointsReceived.WithLabelValues("").Add(float64(out.DataPointCount()))
		}

		resM[tenant] = func() ([]byte, error) { // func so err results can be collected in send/dispatch per tenant
			return marshaler.MarshalMetrics(out)
		}
	}

	return resM, nil
}

// processOTLPResourceMetrics extracts the tenant from a resource attribute and
// optionally removes that attribute. LabelList is reversed at config load time,
// so the first entry present on the resource is the most preferred one.
func (p *processor) processOTLPResourceMetrics(rm pmetric.ResourceMetrics) (tenant string, err error) {
	attrs := rm.Resource().Attributes()

	matchedLabel := ""
	for _, configuredLabel := range p.cfg.Tenant.LabelList {
		if v, ok := attrs.Get(configuredLabel); ok {
			tenant, matchedLabel = v.AsString(), configuredLabel
			break
		}
	}

	if tenant == "" {
		if p.cfg.Tenant.Default == "" {
			return "", fmt.Errorf("label(s): {'%s'} not found", strings.Join(p.cfg.Tenant.LabelList, "','"))
		}

		return p.cfg.Tenant.Default, nil
	}

	if p.cfg.Tenant.LabelRemove {
		attrs.Remove(matchedLabel)
	}

	return tenant, nil
}

// unmarshalOTLPMetrics decodes an OTLP/HTTP metrics request body, supporting
// both protobuf (default) and JSON payloads, optionally gzip-compressed.
func (p *processor) unmarshalOTLPMetrics(ctx *fh.RequestCtx) (pmetric.Metrics, error) {
	body := ctx.Request.Body()

	if enc := string(ctx.Request.Header.Peek("Content-Encoding")); enc != "" {
		switch enc {
		case "gzip":
			b, err := ctx.Request.BodyGunzip()
			if err != nil {
				return pmetric.Metrics{}, errors.Wrap(err, "Unable to gunzip body")
			}
			body = b
		case "identity":
			// no-op
		default:
			return pmetric.Metrics{}, fmt.Errorf("unsupported Content-Encoding %s, must be gzip or identity", enc)
		}
	}

	contentType := string(ctx.Request.Header.Peek("Content-Type"))
	switch {
	case contentType == "" || strings.HasPrefix(contentType, "application/x-protobuf"):
		um := &pmetric.ProtoUnmarshaler{}
		md, err := um.UnmarshalMetrics(body)
		if err != nil {
			return pmetric.Metrics{}, errors.Wrap(err, "Unable to unmarshal OTLP protobuf")
		}
		return md, nil
	case strings.HasPrefix(contentType, "application/json"):
		um := &pmetric.JSONUnmarshaler{}
		md, err := um.UnmarshalMetrics(body)
		if err != nil {
			return pmetric.Metrics{}, errors.Wrap(err, "Unable to unmarshal OTLP JSON")
		}
		return md, nil
	default:
		return pmetric.Metrics{}, fmt.Errorf("unsupported Content-Type %s, must be application/x-protobuf or application/json", contentType)
	}
}
