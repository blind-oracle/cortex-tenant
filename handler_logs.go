package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/google/uuid"
	"github.com/grafana/loki/v3/pkg/logproto"
	lokiunmarshal "github.com/grafana/loki/v3/pkg/util/unmarshal"
	me "github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/promql/parser"
	fh "github.com/valyala/fasthttp"
)

var (
	metricStreamsBatchesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "streams_batches_received",
		Help:      "The total number of streams batches received.",
	})
	metricStreamsBatchesReceivedBytes = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex_tenant",
		Name:      "streams_batches_received_bytes",
		Help:      "Size in bytes of streams batches received.",
		Buckets:   []float64{0.5, 1, 10, 25, 100, 250, 500, 1000, 5000, 10000, 30000, 300000, 600000, 1800000, 3600000},
	})
	metricStreamsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "streams_received",
		Help:      "The total number of streams received.",
	}, []string{"tenant"})
	metricStreamsRequestDurationMilliseconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex_tenant",
		Name:      "streams_request_duration_milliseconds",
		Help:      "HTTP write request duration for tenant-specific streams in milliseconds, filtered by response code.",
		Buckets:   []float64{0.5, 1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000, 1800000, 3600000},
	},
		[]string{"code", "tenant"},
	)
	metricStreamsRequestErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "streams_request_errors",
		Help:      "The total number of tenant-specific streams writes that yielded errors.",
	}, []string{"tenant"})
	metricStreamsRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "streams_requests",
		Help:      "The total number of tenant-specific streams writes.",
	}, []string{"tenant"})
)

func (p *processor) handleLogs(ctx *fh.RequestCtx) {
	metricStreamsBatchesReceivedBytes.Observe(float64(ctx.Request.Header.ContentLength()))
	metricStreamsBatchesReceived.Inc()

	var wrReqIn *logproto.PushRequest
	var err error
	var body []byte
	var contentTypeStr string
	body, contentTypeStr, err = decodeRequestBodyAndContentType(ctx)
	if err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)
		return
	}
	switch {
	case strings.HasPrefix(contentTypeStr, "application/x-protobuf"):
		wrReqIn, err = p.unmarshalLokiPush(body)
	case strings.HasPrefix(contentTypeStr, "application/json"):
		wrReqIn, err = p.unmarshalLokiPushJson(body)
	default:
		err = fmt.Errorf("unsupported Content-Type %s, must be application/x-protobuf or application/json", contentTypeStr)
	}
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

	if len(wrReqIn.Streams) == 0 || len(wrReqIn.Streams[0].Entries) == 0 {
		ctx.Error("error at least one valid stream is required for ingestion", fh.StatusUnprocessableEntity)
		return
	}

	m, err := p.createPushRequests(wrReqIn)
	if err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)
		return
	}

	metricTenant := ""
	var errs *me.Error
	results := p.dispatch(p.cfg.TargetLoki, clientIP, reqID, tenantPrefix, m, contentTypePromRW)

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

		metricStreamsRequests.WithLabelValues(metricTenant).Inc()

		if r.err != nil {
			metricStreamsRequestErrors.WithLabelValues(metricTenant).Inc()
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

		metricStreamsRequestDurationMilliseconds.WithLabelValues(strconv.Itoa(r.code), metricTenant).Observe(r.duration)
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

func (p *processor) createPushRequests(wrReqIn *logproto.PushRequest) (map[string]func() ([]byte, error), error) {
	// Create per-tenant push requests
	m := map[string]*logproto.PushRequest{}

	for _, s := range wrReqIn.Streams {
		tenant, err := p.processStream(&s)
		if err != nil {
			return nil, err
		}

		if p.cfg.MetricsIncludeTenant {
			metricStreamsReceived.WithLabelValues(tenant).Inc()
		} else {
			metricStreamsReceived.WithLabelValues("").Inc()
		}

		wrReqOut, ok := m[tenant]
		if !ok {
			wrReqOut = &logproto.PushRequest{}
			m[tenant] = wrReqOut
		}

		wrReqOut.Streams = append(wrReqOut.Streams, s)
	}

	// Marshal results
	resM := make(map[string]func() ([]byte, error), len(m))
	for tenant, wrReqOut := range m {
		resM[tenant] = func() ([]byte, error) {
			return p.marshalLokiPush(wrReqOut)
		}
	}

	return resM, nil
}

func (p *processor) marshalLokiPush(wrReq *logproto.PushRequest) ([]byte, error) {
	encoded, err := proto.Marshal(wrReq)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to marshal protobuf")
	}

	return snappy.Encode(nil, encoded), nil
}

func (p *processor) unmarshalLokiPush(b []byte) (*logproto.PushRequest, error) {
	req := &logproto.PushRequest{}
	if err := proto.Unmarshal(b, req); err != nil {
		return nil, fmt.Errorf("unable to unmarshal protobuf: %w", err)
	}
	return req, nil
}

func (p *processor) unmarshalLokiPushJson(b []byte) (*logproto.PushRequest, error) {
	reader := bytes.NewReader(b)
	req := &logproto.PushRequest{}
	if err := lokiunmarshal.DecodePushRequest(reader, req); err != nil {
		return nil, fmt.Errorf("unable to unmarshal json: %w", err)
	}
	return req, nil
}

func (p *processor) processStream(s *logproto.Stream) (tenant string, err error) {
	labels, err := streamLabels(s)
	if err != nil {
		return "", err
	}
	tenant = findMatchingLabelValue(labels, p.cfg.Tenant.LabelList)
	if tenant != "" {
		return
	}

	// Fallback to default tenant if configured
	if p.cfg.Tenant.Default == "" {
		return "", fmt.Errorf("label(s): {'%s'} not found, actual values: %v", strings.Join(p.cfg.Tenant.LabelList, "','"), labels)
	}

	return p.cfg.Tenant.Default, nil
}

func findMatchingLabelValue(labels []logproto.LabelAdapter, configuredLabels []string) string {
	for _, l := range labels {
		for _, configuredLabel := range configuredLabels {
			if l.Name == configuredLabel {
				return l.Value
			}
		}
	}
	return ""
}

func streamLabels(s *logproto.Stream) ([]logproto.LabelAdapter, error) {
	// Parse labels in prometheus format {foo="bar", baz="bax"}
	lbls, err := parser.ParseMetric(s.Labels)
	if err != nil {
		return nil, fmt.Errorf("unable to parse stream labels: %w", err)
	}
	result := make([]logproto.LabelAdapter, 0, len(lbls))
	for _, l := range lbls {
		result = append(result, logproto.LabelAdapter{
			Name:  l.Name,
			Value: l.Value,
		})
	}
	return result, nil
}

func decodeRequestBodyAndContentType(ctx *fh.RequestCtx) (body []byte, contentTypeStr string, err error) {
	contentType := ctx.Request.Header.Peek("Content-Type")
	if contentType == nil {
		err = errors.New("missing Content-Type header")
		return nil, "", err
	}
	contentTypeStr = string(contentType)
	encoding := ctx.Request.Header.Peek("Content-Encoding")
	if encoding == nil {
		// Protobuf must always be snappy encoded and json not encoded unless specified
		if strings.HasPrefix(contentTypeStr, "application/x-protobuf") {
			body, err = snappy.Decode(nil, ctx.Request.Body())
			if err != nil {
				return nil, "", err
			}
		} else {
			body = ctx.Request.Body()
		}
	} else {
		encodingStr := string(encoding)
		switch encodingStr {
		case "snappy":
			body, err = snappy.Decode(nil, ctx.Request.Body())
			if err != nil {
				return nil, "", err
			}
		case "gzip":
			body, err = ctx.Request.BodyGunzip()
			if err != nil {
				return nil, "", err
			}
		default:
			err = fmt.Errorf("unsupported Content-Encoding %s, must be snappy or gzip", encodingStr)
			return nil, "", err
		}
	}
	return body, contentTypeStr, nil
}
