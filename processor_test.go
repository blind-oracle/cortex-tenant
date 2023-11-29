package main

import (
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/google/uuid"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	fh "github.com/valyala/fasthttp"
	fhu "github.com/valyala/fasthttp/fasthttputil"
)

const (
	testConfig = `listen: 0.0.0.0:8080
listen_pprof: 0.0.0.0:7008

target: http://127.0.0.1:9091/receive
log_level: debug
timeout: 50ms
timeout_shutdown: 100ms

max_conns_per_host: 64

tenant:
  label_remove: false
  default: default
`
	testConfigWithValues = `listen: 0.0.0.0:8080
listen_pprof: 0.0.0.0:7008

target: http://127.0.0.1:9091/receive
log_level: debug
timeout: 50ms
timeout_shutdown: 100ms

tenant:
  prefix: foobar-
  label_remove: false
  default: default
`
)

var (
	smpl1 = prompb.Sample{
		Value:     123,
		Timestamp: 456,
	}

	smpl2 = prompb.Sample{
		Value:     789,
		Timestamp: 101112,
	}

	testTS1 = prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  "__tenant__",
				Value: "foobar",
			},
		},

		Samples: []prompb.Sample{
			smpl1,
		},
	}

	testTS2 = prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  "__tenant__",
				Value: "foobaz",
			},
		},

		Samples: []prompb.Sample{
			smpl2,
		},
	}

	testTS3 = prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  "__tenantXXX",
				Value: "foobaz",
			},
		},
	}

	testTS4 = prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  "__tenant__",
				Value: "foobaz",
			},
		},

		Samples: []prompb.Sample{
			smpl2,
		},
	}

	testWRQ = &prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			testTS1,
			testTS2,
		},
	}

	testWRQ1 = &prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			testTS1,
		},
	}

	testWRQ2 = &prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			testTS2,
		},
	}

	testWRQ3 = &prompb.WriteRequest{}
	testWRQ4 = &prompb.WriteRequest{
		Metadata: []prompb.MetricMetadata{
			{
				MetricFamilyName: "foobar",
			},
		},
	}
)

func getConfig(contents string) (*config, error) {
	err := os.WriteFile("config_test.yml", []byte(contents), 0o666)
	if err != nil {
		return nil, err
	}

	cfg, err := configLoad("config_test.yml")
	if err != nil {
		return nil, err
	}

	if err = os.Remove("config_test.yml"); err != nil {
		return nil, err
	}

	return cfg, nil
}

func createProcessor() (*processor, error) {
	cfg, err := getConfig(testConfig)
	if err != nil {
		return nil, err
	}

	return newProcessor(*cfg), nil
}

func sinkHandlerError(ctx *fh.RequestCtx) {
	ctx.Error("Some error", fh.StatusInternalServerError)
}

func sinkHandler(ctx *fh.RequestCtx) {
	reqBuf, err := snappy.Decode(nil, ctx.Request.Body())
	if err != nil {
		ctx.Error(err.Error(), http.StatusBadRequest)
		return
	}

	var req prompb.WriteRequest
	if err := proto.Unmarshal(reqBuf, &req); err != nil {
		ctx.Error(err.Error(), http.StatusBadRequest)
		return
	}

	ctx.WriteString("Ok")
}

func Test_config(t *testing.T) {
	cfg, err := configLoad("config.yml")
	assert.Nil(t, err)
	assert.Equal(t, 10, cfg.Concurrency)
}

// Check if Prefix empty by default
func Test_config_is_prefix_empty_by_default(t *testing.T) {
	cfg, err := configLoad("config.yml")
	assert.Nil(t, err)
	assert.Equal(t, "", cfg.Tenant.Prefix)
}

// Check if Prefix empty by default
func Test_config_is_prefix_empty_if_not_set(t *testing.T) {
	cfg, err := getConfig(testConfig)
	assert.Nil(t, err)
	assert.Equal(t, "", cfg.Tenant.Prefix)
}

// Check if Prefix filled with value
func Test_config_is_prefix_filled(t *testing.T) {
	cfg, err := getConfig(testConfigWithValues)
	assert.Nil(t, err)
	assert.Equal(t, "foobar-", cfg.Tenant.Prefix)
}

func Test_request_headers(t *testing.T) {
	cfg, err := getConfig(testConfig)
	assert.Nil(t, err)

	p := newProcessor(*cfg)

	req := fh.AcquireRequest()
	clientIP, _ := net.ResolveIPAddr("ip", "1.1.1.1")
	reqID, _ := uuid.NewRandom()
	p.fillRequestHeaders(clientIP, reqID, "my-tenant", req)

	assert.Equal(t, "snappy", string(req.Header.Peek("Content-Encoding")))
	assert.Equal(t, "my-tenant", string(req.Header.Peek("X-Scope-OrgID")))
}

func Test_request_headers_with_prefix(t *testing.T) {
	cfg, err := getConfig(testConfigWithValues)
	assert.Nil(t, err)

	p := newProcessor(*cfg)

	req := fh.AcquireRequest()
	clientIP, _ := net.ResolveIPAddr("ip", "1.1.1.1")
	reqID, _ := uuid.NewRandom()
	p.fillRequestHeaders(clientIP, reqID, "my-tenant", req)

	assert.Equal(t, "foobar-my-tenant", string(req.Header.Peek("X-Scope-OrgID")))
}

func Test_handle(t *testing.T) {
	cfg, err := getConfig(testConfig)
	assert.Nil(t, err)

	cfg.pipeIn = fhu.NewInmemoryListener()
	cfg.pipeOut = fhu.NewInmemoryListener()
	cfg.Tenant.LabelRemove = true

	p := newProcessor(*cfg)
	err = p.run()
	assert.Nil(t, err)

	wrq1, err := p.marshal(testWRQ)
	assert.Nil(t, err)

	wrq3, err := p.marshal(testWRQ3)
	assert.Nil(t, err)

	wrq4, err := p.marshal(testWRQ4)
	assert.Nil(t, err)

	s := &fh.Server{
		Handler: sinkHandlerError,
	}
	// client.Do behaviour changed in https://github.com/valyala/fasthttp/pull/1346
	// Don't run requests in a separate Goroutine anymore.
	go s.Serve(cfg.pipeOut)

	c := &fh.Client{
		Dial: func(a string) (net.Conn, error) {
			return cfg.pipeIn.Dial()
		},
	}

	// Connection failed
	req := fh.AcquireRequest()
	resp := fh.AcquireResponse()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/push")
	req.SetBody(wrq1)

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 500, resp.StatusCode())

	s.Handler = sinkHandler
	// Success 1
	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/push")
	req.SetBody(wrq1)

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, "Ok", string(resp.Body()))

	// Success 2
	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/push")
	req.SetBody(wrq4)

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 200, resp.StatusCode())

	// Error 0
	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/push")
	req.SetBody(wrq3)

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 400, resp.StatusCode())

	// Error 1
	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/push")
	req.SetBody([]byte("foobar"))

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 400, resp.StatusCode())

	// Error 2
	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/push")
	req.SetBody(snappy.Encode(nil, []byte("foobar")))

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 400, resp.StatusCode())

	// Error 3
	s.Handler = sinkHandlerError

	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/push")
	req.SetBody(wrq1)

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 500, resp.StatusCode())

	// Close
	go p.close()
	time.Sleep(30 * time.Millisecond)

	req.Reset()
	resp.Reset()

	req.Header.SetMethod("GET")
	req.SetRequestURI("http://127.0.0.1/alive")

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 503, resp.StatusCode())
}

func Test_processTimeseries(t *testing.T) {
	cfg, err := getConfig(testConfig)
	assert.Nil(t, err)
	cfg.Tenant.LabelRemove = true

	p := newProcessor(*cfg)
	assert.Nil(t, err)

	ten, err := p.processTimeseries(&testTS4)
	assert.Nil(t, err)
	assert.Equal(t, "foobaz", ten)

	ten, err = p.processTimeseries(&testTS3)
	assert.Nil(t, err)
	assert.Equal(t, "default", ten)

	cfg.Tenant.Default = ""
	p = newProcessor(*cfg)
	assert.Nil(t, err)

	_, err = p.processTimeseries(&testTS3)
	assert.NotNil(t, err)
}

func Test_marshal(t *testing.T) {
	p, err := createProcessor()
	assert.Nil(t, err)

	_, err = p.unmarshal([]byte{0xFF})
	assert.NotNil(t, err)

	_, err = p.unmarshal(snappy.Encode(nil, []byte{0xFF}))
	assert.NotNil(t, err)

	//buf := make([]byte, 1024)
	buf, err := p.marshal(testWRQ)
	assert.Nil(t, err)

	wrq, err := p.unmarshal(buf)
	assert.Nil(t, err)

	assert.Equal(t, testTS1, wrq.Timeseries[0])
	assert.Equal(t, testTS2, wrq.Timeseries[1])
}

func Test_createWriteRequests(t *testing.T) {
	p, err := createProcessor()
	assert.Nil(t, err)

	m, err := p.createWriteRequests(testWRQ)
	assert.Nil(t, err)

	mExp := map[string]*prompb.WriteRequest{
		"foobar": testWRQ1,
		"foobaz": testWRQ2,
	}

	assert.Equal(t, mExp, m)
}

func Benchmark_marshal(b *testing.B) {
	p, _ := createProcessor()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, _ := p.marshal(testWRQ)
		_, _ = p.unmarshal(buf)
	}
}

func TestRemoveOrdered(t *testing.T) {
	l := []prompb.Label{
		{
			Name:  "aaa",
			Value: "bbb",
		},
	}

	l = removeOrdered(l, 0)
	require.Equal(t, []prompb.Label{}, l)

	l = []prompb.Label{
		{
			Name:  "aaa",
			Value: "bbb",
		},
		{
			Name:  "ccc",
			Value: "ddd",
		},
	}
	l = removeOrdered(l, 0)
	require.Equal(t, []prompb.Label{
		{
			Name:  "ccc",
			Value: "ddd",
		},
	}, l)

}
