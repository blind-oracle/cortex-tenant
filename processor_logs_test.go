package main

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/stretchr/testify/assert"

	fh "github.com/valyala/fasthttp"
	fhu "github.com/valyala/fasthttp/fasthttputil"
)

const (
	testLokiConfig = `listen: 0.0.0.0:8080
listen_pprof: 0.0.0.0:7008

target: http://127.0.0.1:9091/receive
target_loki: http://127.0.0.1:3100/loki/api/v1/push
log_level: debug
timeout: 50ms
timeout_shutdown: 100ms

max_conns_per_host: 64

tenant:
  label_remove: false
  default: default
  label_list:
    - "__tenant__"
    - "__foo__"
    - "__bar__"
`
)

var (
	entry1 = logproto.Entry{
		Timestamp: time.Now().UTC(),
		Line:      "Hello world",
		Parsed: []logproto.LabelAdapter{
			{
				Name:  "__tenant__",
				Value: "foobar",
			},
		},
	}

	entry2 = logproto.Entry{
		Timestamp: time.Now().UTC(),
		Line:      "Hello world",
		StructuredMetadata: []logproto.LabelAdapter{
			{
				Name:  "__tenantXXX",
				Value: "foobaz",
			},
		},
	}

	entry3 = logproto.Entry{
		Timestamp: time.Now().UTC(),
		Line:      "Hello world",
		StructuredMetadata: []logproto.LabelAdapter{
			{
				Name:  "__tenant__",
				Value: "foobaz",
			},
		},
	}

	testStream1 = logproto.Stream{
		Entries: []logproto.Entry{
			entry1,
		},
		Labels: `{app="myapp",__tenant__="foobar"}`,
		Hash:   1234,
	}

	testStream2 = logproto.Stream{
		Entries: []logproto.Entry{
			entry2,
		},
		Labels: `{app="myapp",__tenantXXX="foobaz"}`,
		Hash:   9012,
	}

	testStream3 = logproto.Stream{
		Entries: []logproto.Entry{
			entry3,
		},
		Labels: `{app="myapp",__tenant__="foobaz"}`,
		Hash:   5678,
	}

	testPRQ = &logproto.PushRequest{
		Streams: []logproto.Stream{
			testStream1,
			testStream3,
		},
	}

	testPRQ1 = &logproto.PushRequest{
		Streams: []logproto.Stream{
			testStream1,
		},
	}

	testPRQ2 = &logproto.PushRequest{
		Streams: []logproto.Stream{
			testStream3,
		},
	}

	testPRQ3 = &logproto.PushRequest{}
)

func sinkHandlerLogs(ctx *fh.RequestCtx) {
	reqBuf, err := snappy.Decode(nil, ctx.Request.Body())
	if err != nil {
		ctx.Error(err.Error(), http.StatusBadRequest)
		return
	}

	var req logproto.PushRequest
	if err := proto.Unmarshal(reqBuf, &req); err != nil {
		ctx.Error(err.Error(), http.StatusBadRequest)
		return
	}

	ctx.WriteString("Ok")
}

func Test_handle_logs(t *testing.T) {
	cfg, err := getConfig(testLokiConfig)
	assert.Nil(t, err)

	cfg.pipeIn = fhu.NewInmemoryListener()
	cfg.pipeOut = fhu.NewInmemoryListener()
	cfg.Tenant.LabelRemove = true

	p, _ := newProcessor(*cfg)
	err = p.run()
	assert.Nil(t, err)

	wrq1, err := p.marshalLokiPush(testPRQ)
	assert.Nil(t, err)

	wrq3, err := p.marshalLokiPush(testPRQ3)
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
	req.SetRequestURI("http://127.0.0.1/loki/push")
	req.Header.Add("Content-Type", "application/x-protobuf")
	req.Header.Add("Content-Encoding", "snappy")
	req.SetBody(wrq1)

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 500, resp.StatusCode())

	s.Handler = sinkHandlerLogs
	// Success 1
	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/loki/push")
	req.Header.Add("Content-Type", "application/x-protobuf")
	req.Header.Add("Content-Encoding", "snappy")
	req.SetBody(wrq1)

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, "Ok", string(resp.Body()))

	// Success 2
	req.Reset()
	resp.Reset()

	// Error 0
	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/loki/push")
	req.Header.Add("Content-Type", "application/x-protobuf")
	req.Header.Add("Content-Encoding", "snappy")
	req.SetBody(wrq3)

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 422, resp.StatusCode()) // UnprocessableEntity

	// Error 1
	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/loki/push")
	req.Header.Add("Content-Type", "application/x-protobuf")
	req.Header.Add("Content-Encoding", "snappy")
	req.SetBody([]byte("foobar"))

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 400, resp.StatusCode())

	// Error 2
	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/loki/push")
	req.Header.Add("Content-Type", "application/x-protobuf")
	req.Header.Add("Content-Encoding", "snappy")
	req.SetBody(snappy.Encode(nil, []byte("foobar")))

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 400, resp.StatusCode())

	// Error 3
	s.Handler = sinkHandlerError

	req.Reset()
	resp.Reset()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/loki/push")
	req.Header.Add("Content-Type", "application/x-protobuf")
	req.Header.Add("Content-Encoding", "snappy")
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

func Test_processStream(t *testing.T) {
	cfg, err := getConfig(testLokiConfig)
	assert.Nil(t, err)

	p, _ := newProcessor(*cfg)

	ten, err := p.processStream(&testStream3)
	assert.Nil(t, err)
	assert.Equal(t, "foobaz", ten)

	ten, err = p.processStream(&testStream2)
	assert.Nil(t, err)
	assert.Equal(t, "default", ten)

	cfg.Tenant.Default = ""
	p, _ = newProcessor(*cfg)
	assert.Nil(t, err)

	_, err = p.processStream(&testStream2)
	assert.NotNil(t, err)
}

func Test_marshalLokiPush(t *testing.T) {
	p, err := createProcessor()
	assert.Nil(t, err)

	_, err = p.unmarshalLokiPush([]byte{0xFF})
	assert.NotNil(t, err)

	_, err = p.unmarshalLokiPush(snappy.Encode(nil, []byte{0xFF}))
	assert.NotNil(t, err)

	buf, err := p.marshalLokiPush(testPRQ)
	assert.Nil(t, err)

	buf, err = snappy.Decode(nil, buf)
	assert.Nil(t, err)

	wrq, err := p.unmarshalLokiPush(buf)
	assert.Nil(t, err)

	assert.Equal(t, 2, len(wrq.Streams))
	assert.Equal(t, testStream1, wrq.Streams[0])
	assert.Equal(t, testStream3, wrq.Streams[1])
}

func Test_createPushRequests(t *testing.T) {
	p, err := createProcessor()
	assert.Nil(t, err)

	m, err := p.createPushRequests(testPRQ)
	assert.Nil(t, err)

	mExp := map[string]func() ([]byte, error){
		"foobar": func() ([]byte, error) {
			return p.marshalLokiPush(testPRQ1)
		},
		"foobaz": func() ([]byte, error) {
			return p.marshalLokiPush(testPRQ2)
		},
	}

	for k, v := range mExp {
		v2, ok := m[k]
		assert.True(t, ok)
		if !ok {
			t.Logf("Missing key %s", k)
			continue
		}
		vVal, vErr := v()
		v2Val, v2Err := v2()
		assert.Equal(t, vVal, v2Val)
		assert.Equal(t, vErr, v2Err)
	}
}
