package main

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pmetric"

	fh "github.com/valyala/fasthttp"
	fhu "github.com/valyala/fasthttp/fasthttputil"
)

const testOTLPConfig = `listen: 0.0.0.0:8080
listen_pprof: 0.0.0.0:7008

target: http://127.0.0.1:9091/receive
target_loki: http://127.0.0.1:3100/loki/api/v1/push
target_otlp: http://127.0.0.1:9009/otlp/v1/metrics
log_level: debug
timeout: 50ms
timeout_shutdown: 100ms

max_conns_per_host: 64

tenant:
  label_remove: false
  default: default
  label_list:
    - "tenant"
    - "other_tenant"
`

// appendOTLPResource appends a ResourceMetrics carrying the given resource
// attributes and a single gauge data point to md.
func appendOTLPResource(md pmetric.Metrics, attrs map[string]string, metricName string, value float64) {
	rm := md.ResourceMetrics().AppendEmpty()
	for k, v := range attrs {
		rm.Resource().Attributes().PutStr(k, v)
	}
	m := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName(metricName)
	m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(value)
}

// newTestOTLPMetrics builds a pmetric.Metrics with a single resource.
func newTestOTLPMetrics(attrs map[string]string, metricName string, value float64) pmetric.Metrics {
	md := pmetric.NewMetrics()
	appendOTLPResource(md, attrs, metricName, value)
	return md
}

func marshalOTLP(t *testing.T, md pmetric.Metrics) []byte {
	t.Helper()
	b, err := (&pmetric.ProtoMarshaler{}).MarshalMetrics(md)
	require.NoError(t, err)
	return b
}

func unmarshalOTLP(t *testing.T, b []byte) pmetric.Metrics {
	t.Helper()
	md, err := (&pmetric.ProtoUnmarshaler{}).UnmarshalMetrics(b)
	require.NoError(t, err)
	return md
}

func Test_processOTLPResourceMetrics(t *testing.T) {
	cfg, err := getConfig(testOTLPConfig)
	require.NoError(t, err)
	cfg.Tenant.LabelRemove = true

	p, err := newProcessor(*cfg)
	require.NoError(t, err)

	// Tenant label present.
	md := newTestOTLPMetrics(map[string]string{"tenant": "foobar"}, "m", 1)
	tenant, err := p.processOTLPResourceMetrics(md.ResourceMetrics().At(0))
	require.NoError(t, err)
	assert.Equal(t, "foobar", tenant)
	// Label removed when label_remove is enabled.
	_, ok := md.ResourceMetrics().At(0).Resource().Attributes().Get("tenant")
	assert.False(t, ok, "tenant attribute should have been removed")

	// No tenant label -> default tenant.
	md = newTestOTLPMetrics(map[string]string{"foo": "bar"}, "m", 1)
	tenant, err = p.processOTLPResourceMetrics(md.ResourceMetrics().At(0))
	require.NoError(t, err)
	assert.Equal(t, "default", tenant)

	// other_tenant is preferred over tenant (last in config list wins).
	md = newTestOTLPMetrics(map[string]string{"tenant": "a", "other_tenant": "b"}, "m", 1)
	tenant, err = p.processOTLPResourceMetrics(md.ResourceMetrics().At(0))
	require.NoError(t, err)
	assert.Equal(t, "b", tenant)

	// No tenant label and no default -> error.
	cfg.Tenant.Default = ""
	p, err = newProcessor(*cfg)
	require.NoError(t, err)

	md = newTestOTLPMetrics(map[string]string{"foo": "bar"}, "m", 1)
	_, err = p.processOTLPResourceMetrics(md.ResourceMetrics().At(0))
	assert.Error(t, err)
}

func Test_createOTLPWriteRequests(t *testing.T) {
	cfg, err := getConfig(testOTLPConfig)
	require.NoError(t, err)
	cfg.Tenant.LabelRemove = true

	p, err := newProcessor(*cfg)
	require.NoError(t, err)

	md := pmetric.NewMetrics()
	appendOTLPResource(md, map[string]string{"tenant": "foobar"}, "m1", 1)
	appendOTLPResource(md, map[string]string{"tenant": "foobaz"}, "m2", 2)
	appendOTLPResource(md, map[string]string{"no_tenant": "x"}, "m3", 3) // -> default

	m, err := p.createOTLPWriteRequests(md)
	require.NoError(t, err)

	require.Len(t, m, 3)
	for _, tenant := range []string{"foobar", "foobaz", "default"} {
		bodyFunc, ok := m[tenant]
		require.Truef(t, ok, "expected tenant %q", tenant)

		buf, err := bodyFunc()
		require.NoError(t, err)

		out := unmarshalOTLP(t, buf)
		require.Equal(t, 1, out.ResourceMetrics().Len())

		// tenant label must be stripped from the forwarded payload.
		_, has := out.ResourceMetrics().At(0).Resource().Attributes().Get("tenant")
		assert.Falsef(t, has, "tenant attribute should be stripped for %q", tenant)
	}
}

func Test_handle_otlp(t *testing.T) {
	cfg, err := getConfig(testOTLPConfig)
	require.NoError(t, err)

	cfg.pipeIn = fhu.NewInmemoryListener()
	cfg.pipeOut = fhu.NewInmemoryListener()
	cfg.Tenant.LabelRemove = true

	p, err := newProcessor(*cfg)
	require.NoError(t, err)
	require.NoError(t, p.run())

	body := marshalOTLP(t, newTestOTLPMetrics(map[string]string{"tenant": "foobar"}, "m", 1))

	gotTenant := ""
	s := &fh.Server{
		Handler: func(ctx *fh.RequestCtx) {
			gotTenant = string(ctx.Request.Header.Peek("X-Scope-OrgID"))
			// Upstream must receive uncompressed OTLP protobuf, not snappy.
			assert.Equal(t, "application/x-protobuf", string(ctx.Request.Header.Peek("Content-Type")))
			assert.Empty(t, string(ctx.Request.Header.Peek("Content-Encoding")))
			if _, err := (&pmetric.ProtoUnmarshaler{}).UnmarshalMetrics(ctx.Request.Body()); err != nil {
				ctx.Error(err.Error(), fh.StatusBadRequest)
				return
			}
			ctx.WriteString("Ok")
		},
	}
	go s.Serve(cfg.pipeOut)

	c := &fh.Client{
		Dial: func(a string) (net.Conn, error) {
			return cfg.pipeIn.Dial()
		},
	}

	// Success: valid OTLP protobuf is routed to the tenant from the resource label.
	req := fh.AcquireRequest()
	resp := fh.AcquireResponse()

	req.Header.SetMethod("POST")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.SetRequestURI("http://127.0.0.1/v1/metrics")
	req.SetBody(body)

	require.NoError(t, c.Do(req, resp))
	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, "Ok", string(resp.Body()))
	assert.Equal(t, "foobar", gotTenant)

	// Invalid protobuf body -> 400.
	req.Reset()
	resp.Reset()
	req.Header.SetMethod("POST")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.SetRequestURI("http://127.0.0.1/v1/metrics")
	req.SetBody([]byte("not-a-protobuf"))

	require.NoError(t, c.Do(req, resp))
	assert.Equal(t, 400, resp.StatusCode())

	// Upstream failure -> 500.
	s.Handler = sinkHandlerError
	req.Reset()
	resp.Reset()
	req.Header.SetMethod("POST")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.SetRequestURI("http://127.0.0.1/v1/metrics")
	req.SetBody(body)

	require.NoError(t, c.Do(req, resp))
	assert.Equal(t, 500, resp.StatusCode())

	go p.close()
	time.Sleep(30 * time.Millisecond)
}
