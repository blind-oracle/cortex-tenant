package main

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	fh "github.com/valyala/fasthttp"
	fhu "github.com/valyala/fasthttp/fasthttputil"
)

// appendOTLPResourceSpans appends a ResourceSpans carrying the given resource
// attributes and a single span to td.
func appendOTLPResourceSpans(td ptrace.Traces, attrs map[string]string, spanName string) {
	rs := td.ResourceSpans().AppendEmpty()
	for k, v := range attrs {
		rs.Resource().Attributes().PutStr(k, v)
	}
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName(spanName)
}

// newTestOTLPTraces builds a ptrace.Traces with a single resource.
func newTestOTLPTraces(attrs map[string]string, spanName string) ptrace.Traces {
	td := ptrace.NewTraces()
	appendOTLPResourceSpans(td, attrs, spanName)
	return td
}

func marshalTraces(t *testing.T, td ptrace.Traces) []byte {
	t.Helper()
	b, err := (&ptrace.ProtoMarshaler{}).MarshalTraces(td)
	require.NoError(t, err)
	return b
}

func unmarshalTraces(t *testing.T, b []byte) ptrace.Traces {
	t.Helper()
	td, err := (&ptrace.ProtoUnmarshaler{}).UnmarshalTraces(b)
	require.NoError(t, err)
	return td
}

func Test_processOTLPResourceSpans(t *testing.T) {
	cfg, err := getConfig(testOTLPConfig)
	require.NoError(t, err)
	cfg.Tenant.LabelRemove = true

	p, err := newProcessor(*cfg)
	require.NoError(t, err)

	// Tenant label present.
	td := newTestOTLPTraces(map[string]string{"tenant": "foobar"}, "s")
	tenant, err := p.processOTLPResourceSpans(td.ResourceSpans().At(0))
	require.NoError(t, err)
	assert.Equal(t, "foobar", tenant)
	// Label removed when label_remove is enabled.
	_, ok := td.ResourceSpans().At(0).Resource().Attributes().Get("tenant")
	assert.False(t, ok, "tenant attribute should have been removed")

	// No tenant label -> default tenant.
	td = newTestOTLPTraces(map[string]string{"foo": "bar"}, "s")
	tenant, err = p.processOTLPResourceSpans(td.ResourceSpans().At(0))
	require.NoError(t, err)
	assert.Equal(t, "default", tenant)

	// other_tenant is preferred over tenant (last in config list wins).
	td = newTestOTLPTraces(map[string]string{"tenant": "a", "other_tenant": "b"}, "s")
	tenant, err = p.processOTLPResourceSpans(td.ResourceSpans().At(0))
	require.NoError(t, err)
	assert.Equal(t, "b", tenant)

	// No tenant label and no default -> error.
	cfg.Tenant.Default = ""
	p, err = newProcessor(*cfg)
	require.NoError(t, err)

	td = newTestOTLPTraces(map[string]string{"foo": "bar"}, "s")
	_, err = p.processOTLPResourceSpans(td.ResourceSpans().At(0))
	assert.Error(t, err)
}

func Test_createOTLPTraceWriteRequests(t *testing.T) {
	cfg, err := getConfig(testOTLPConfig)
	require.NoError(t, err)
	cfg.Tenant.LabelRemove = true

	p, err := newProcessor(*cfg)
	require.NoError(t, err)

	td := ptrace.NewTraces()
	appendOTLPResourceSpans(td, map[string]string{"tenant": "foobar"}, "s1")
	appendOTLPResourceSpans(td, map[string]string{"tenant": "foobaz"}, "s2")
	appendOTLPResourceSpans(td, map[string]string{"no_tenant": "x"}, "s3") // -> default

	m, err := p.createOTLPTraceWriteRequests(td)
	require.NoError(t, err)

	require.Len(t, m, 3)
	for _, tenant := range []string{"foobar", "foobaz", "default"} {
		bodyFunc, ok := m[tenant]
		require.Truef(t, ok, "expected tenant %q", tenant)

		buf, err := bodyFunc()
		require.NoError(t, err)

		out := unmarshalTraces(t, buf)
		require.Equal(t, 1, out.ResourceSpans().Len())

		// tenant label must be stripped from the forwarded payload.
		_, has := out.ResourceSpans().At(0).Resource().Attributes().Get("tenant")
		assert.Falsef(t, has, "tenant attribute should be stripped for %q", tenant)
	}
}

func Test_handle_otlp_traces(t *testing.T) {
	cfg, err := getConfig(testOTLPConfig)
	require.NoError(t, err)

	cfg.pipeIn = fhu.NewInmemoryListener()
	cfg.pipeOut = fhu.NewInmemoryListener()
	cfg.Tenant.LabelRemove = true

	p, err := newProcessor(*cfg)
	require.NoError(t, err)
	require.NoError(t, p.run())

	body := marshalTraces(t, newTestOTLPTraces(map[string]string{"tenant": "foobar"}, "s"))

	gotTenant := ""
	s := &fh.Server{
		Handler: func(ctx *fh.RequestCtx) {
			gotTenant = string(ctx.Request.Header.Peek("X-Scope-OrgID"))
			// Upstream must receive uncompressed OTLP protobuf, not snappy.
			assert.Equal(t, "application/x-protobuf", string(ctx.Request.Header.Peek("Content-Type")))
			assert.Empty(t, string(ctx.Request.Header.Peek("Content-Encoding")))
			if _, err := (&ptrace.ProtoUnmarshaler{}).UnmarshalTraces(ctx.Request.Body()); err != nil {
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
	req.SetRequestURI("http://127.0.0.1/v1/traces")
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
	req.SetRequestURI("http://127.0.0.1/v1/traces")
	req.SetBody([]byte("not-a-protobuf"))

	require.NoError(t, c.Do(req, resp))
	assert.Equal(t, 400, resp.StatusCode())

	// Upstream failure -> 500.
	s.Handler = sinkHandlerError
	req.Reset()
	resp.Reset()
	req.Header.SetMethod("POST")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.SetRequestURI("http://127.0.0.1/v1/traces")
	req.SetBody(body)

	require.NoError(t, c.Do(req, resp))
	assert.Equal(t, 500, resp.StatusCode())

	go p.close()
	time.Sleep(30 * time.Millisecond)
}

func Test_handle_otlp_traces_gzip_egress(t *testing.T) {
	cfg, err := getConfig(testOTLPConfig)
	require.NoError(t, err)

	cfg.pipeIn = fhu.NewInmemoryListener()
	cfg.pipeOut = fhu.NewInmemoryListener()
	cfg.Tenant.LabelRemove = true
	cfg.OTLPEgressGzip = true

	p, err := newProcessor(*cfg)
	require.NoError(t, err)
	require.NoError(t, p.run())

	body := marshalTraces(t, newTestOTLPTraces(map[string]string{"tenant": "foobar"}, "s"))

	s := &fh.Server{
		Handler: func(ctx *fh.RequestCtx) {
			// Upstream must receive gzip-compressed OTLP protobuf.
			assert.Equal(t, "application/x-protobuf", string(ctx.Request.Header.Peek("Content-Type")))
			assert.Equal(t, "gzip", string(ctx.Request.Header.Peek("Content-Encoding")))
			raw, err := ctx.Request.BodyGunzip()
			if err != nil {
				ctx.Error(err.Error(), fh.StatusBadRequest)
				return
			}
			if _, err := (&ptrace.ProtoUnmarshaler{}).UnmarshalTraces(raw); err != nil {
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

	req := fh.AcquireRequest()
	resp := fh.AcquireResponse()
	req.Header.SetMethod("POST")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.SetRequestURI("http://127.0.0.1/v1/traces")
	req.SetBody(body)

	require.NoError(t, c.Do(req, resp))
	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, "Ok", string(resp.Body()))

	go p.close()
	time.Sleep(30 * time.Millisecond)
}
