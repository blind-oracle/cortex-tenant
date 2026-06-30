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
	"go.opentelemetry.io/collector/pdata/ptrace"
)

var (
	metricOTLPTracesBatchesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_traces_batches_received",
		Help:      "The total number of OTLP traces batches received.",
	})
	metricOTLPTracesBatchesReceivedBytes = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_traces_batches_received_bytes",
		Help:      "Size in bytes of OTLP traces batches received.",
		Buckets:   []float64{0.5, 1, 10, 25, 100, 250, 500, 1000, 5000, 10000, 30000, 300000, 600000, 1800000, 3600000},
	})
	metricOTLPSpansReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_traces_spans_received",
		Help:      "The total number of OTLP spans received.",
	}, []string{"tenant"})
	metricOTLPTracesRequestDurationMilliseconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_traces_request_duration_milliseconds",
		Help:      "HTTP write request duration for tenant-specific OTLP traces in milliseconds, filtered by response code.",
		Buckets:   []float64{0.5, 1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000, 1800000, 3600000},
	},
		[]string{"code", "tenant"},
	)
	metricOTLPTracesRequestErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_traces_request_errors",
		Help:      "The total number of tenant-specific OTLP traces writes that yielded errors.",
	}, []string{"tenant"})
	metricOTLPTracesRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "otlp_traces_requests",
		Help:      "The total number of tenant-specific OTLP traces writes.",
	}, []string{"tenant"})
)

// handleOTLPTraces receives OTLP/HTTP traces on /v1/traces, splits the
// ResourceSpans by tenant (based on a resource attribute) and forwards them
// to an OTLP-capable backend (e.g. Grafana Tempo) with the relevant
// per-tenant header set.
func (p *processor) handleOTLPTraces(ctx *fh.RequestCtx) {
	metricOTLPTracesBatchesReceivedBytes.Observe(float64(ctx.Request.Header.ContentLength()))
	metricOTLPTracesBatchesReceived.Inc()

	td, err := p.unmarshalOTLPTraces(ctx)
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

	if td.ResourceSpans().Len() == 0 {
		ctx.Error("No resource spans found in the request", fh.StatusBadRequest)
		return
	}

	m, err := p.createOTLPTraceWriteRequests(td)
	if err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)
		return
	}

	metricTenant := ""
	var errs *me.Error
	results := p.dispatch(p.cfg.TargetTraces, clientIP, reqID, tenantPrefix, m, p.otlpEgressContentType())

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

		metricOTLPTracesRequests.WithLabelValues(metricTenant).Inc()

		if r.err != nil {
			metricOTLPTracesRequestErrors.WithLabelValues(metricTenant).Inc()
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

		metricOTLPTracesRequestDurationMilliseconds.WithLabelValues(strconv.Itoa(r.code), metricTenant).Observe(r.duration)
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

// createOTLPTraceWriteRequests groups the incoming ResourceSpans by tenant and
// returns one marshal closure per tenant. The resource spans are partitioned
// whole, so all scopes/spans under a resource follow that resource's tenant.
func (p *processor) createOTLPTraceWriteRequests(td ptrace.Traces) (map[string]func() ([]byte, error), error) {
	perTenant := map[string]ptrace.Traces{}

	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)

		tenant, err := p.processOTLPResourceSpans(rs)
		if err != nil {
			return nil, err
		}

		out, ok := perTenant[tenant]
		if !ok {
			out = ptrace.NewTraces()
			perTenant[tenant] = out
		}

		rs.CopyTo(out.ResourceSpans().AppendEmpty())
	}

	// Marshal results
	marshaler := &ptrace.ProtoMarshaler{}
	resM := make(map[string]func() ([]byte, error), len(perTenant))
	for tenant, out := range perTenant {
		if p.cfg.MetricsIncludeTenant {
			metricOTLPSpansReceived.WithLabelValues(tenant).Add(float64(out.SpanCount()))
		} else {
			metricOTLPSpansReceived.WithLabelValues("").Add(float64(out.SpanCount()))
		}

		resM[tenant] = func() ([]byte, error) { // func so err results can be collected in send/dispatch per tenant
			return marshaler.MarshalTraces(out)
		}
	}

	return resM, nil
}

// processOTLPResourceSpans extracts the tenant from a resource attribute and
// optionally removes that attribute. LabelList is reversed at config load time,
// so the first entry present on the resource is the most preferred one.
func (p *processor) processOTLPResourceSpans(rs ptrace.ResourceSpans) (tenant string, err error) {
	attrs := rs.Resource().Attributes()

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

// unmarshalOTLPTraces decodes an OTLP/HTTP traces request body, supporting
// both protobuf (default) and JSON payloads, optionally gzip-compressed.
func (p *processor) unmarshalOTLPTraces(ctx *fh.RequestCtx) (ptrace.Traces, error) {
	body := ctx.Request.Body()

	if enc := string(ctx.Request.Header.Peek("Content-Encoding")); enc != "" {
		switch enc {
		case "gzip":
			b, err := ctx.Request.BodyGunzip()
			if err != nil {
				return ptrace.Traces{}, errors.Wrap(err, "Unable to gunzip body")
			}
			body = b
		case "identity":
			// no-op
		default:
			return ptrace.Traces{}, fmt.Errorf("unsupported Content-Encoding %s, must be gzip or identity", enc)
		}
	}

	contentType := string(ctx.Request.Header.Peek("Content-Type"))
	switch {
	case contentType == "" || strings.HasPrefix(contentType, "application/x-protobuf"):
		um := &ptrace.ProtoUnmarshaler{}
		td, err := um.UnmarshalTraces(body)
		if err != nil {
			return ptrace.Traces{}, errors.Wrap(err, "Unable to unmarshal OTLP protobuf")
		}
		return td, nil
	case strings.HasPrefix(contentType, "application/json"):
		um := &ptrace.JSONUnmarshaler{}
		td, err := um.UnmarshalTraces(body)
		if err != nil {
			return ptrace.Traces{}, errors.Wrap(err, "Unable to unmarshal OTLP JSON")
		}
		return td, nil
	default:
		return ptrace.Traces{}, fmt.Errorf("unsupported Content-Type %s, must be application/x-protobuf or application/json", contentType)
	}
}
