package main

import (
	"net"
	"net/http"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/assert"

	fh "github.com/valyala/fasthttp"
	fhu "github.com/valyala/fasthttp/fasthttputil"
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

	testTS1 = &prompb.TimeSeries{
		Labels: []*prompb.Label{
			&prompb.Label{
				Name:  "__tenant__",
				Value: "foobar",
			},
		},

		Samples: []prompb.Sample{
			smpl1,
		},
	}

	testTS2 = &prompb.TimeSeries{
		Labels: []*prompb.Label{
			&prompb.Label{
				Name:  "__tenant__",
				Value: "foobaz",
			},
		},

		Samples: []prompb.Sample{
			smpl2,
		},
	}

	testTS3 = &prompb.TimeSeries{
		Labels: []*prompb.Label{
			&prompb.Label{
				Name:  "__tenantXXX",
				Value: "foobaz",
			},
		},
	}

	testTS4 = &prompb.TimeSeries{
		Labels: []*prompb.Label{
			&prompb.Label{
				Name:  "__tenant__",
				Value: "foobaz",
			},
		},

		Samples: []prompb.Sample{
			smpl2,
		},
	}

	testWRQ = &prompb.WriteRequest{
		Timeseries: []*prompb.TimeSeries{
			testTS1,
			testTS2,
		},
	}

	testWRQ1 = &prompb.WriteRequest{
		Timeseries: []*prompb.TimeSeries{
			testTS1,
		},
	}

	testWRQ2 = &prompb.WriteRequest{
		Timeseries: []*prompb.TimeSeries{
			testTS2,
		},
	}
)

func createProcessor() (*processor, error) {
	cfg, err := configLoad("config.yml")
	if err != nil {
		return nil, err
	}

	return newProcessor(*cfg), nil
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
}

func Test_handle(t *testing.T) {
	cfg, err := configLoad("config.yml")
	assert.Nil(t, err)

	cfg.pipeIn = fhu.NewInmemoryListener()
	cfg.pipeOut = fhu.NewInmemoryListener()
	cfg.Tenant.LabelRemove = true

	p := newProcessor(*cfg)
	err = p.run()
	assert.Nil(t, err)

	s := &fh.Server{
		Handler: sinkHandler,
	}

	go s.Serve(cfg.pipeOut)

	c := &fh.Client{
		Dial: func(a string) (net.Conn, error) {
			return cfg.pipeIn.Dial()
		},
	}

	buf, err := p.marshal(testWRQ, nil)
	assert.Nil(t, err)

	// Success
	req := fh.AcquireRequest()
	resp := fh.AcquireResponse()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/push")
	req.SetBody(buf)

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 200, resp.StatusCode())

	// Error
	req = fh.AcquireRequest()
	resp = fh.AcquireResponse()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/push")
	req.SetBody([]byte("foobar"))

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 400, resp.StatusCode())

	// Error 2
	req = fh.AcquireRequest()
	resp = fh.AcquireResponse()

	req.Header.SetMethod("POST")
	req.SetRequestURI("http://127.0.0.1/push")
	req.SetBody(snappy.Encode(nil, []byte("foobar")))

	err = c.Do(req, resp)
	assert.Nil(t, err)

	assert.Equal(t, 400, resp.StatusCode())
}

func Test_processTimeseries(t *testing.T) {
	cfg, err := configLoad("config.yml")
	assert.Nil(t, err)
	cfg.Tenant.LabelRemove = true

	p := newProcessor(*cfg)
	assert.Nil(t, err)

	ten := p.processTimeseries(testTS4)
	assert.Equal(t, "foobaz", ten)

	ten = p.processTimeseries(testTS3)
	assert.Equal(t, "default", ten)
}

func Test_marshal(t *testing.T) {
	p, err := createProcessor()
	assert.Nil(t, err)

	_, err = p.unmarshal([]byte{0xFF})
	assert.NotNil(t, err)

	_, err = p.unmarshal(snappy.Encode(nil, []byte{0xFF}))
	assert.NotNil(t, err)

	buf := make([]byte, 1024)
	buf, err = p.marshal(testWRQ, buf)
	assert.Nil(t, err)

	wrq, err := p.unmarshal(buf)
	assert.Nil(t, err)

	assert.Equal(t, testTS1, wrq.Timeseries[0])
	assert.Equal(t, testTS2, wrq.Timeseries[1])

	p.close()
}

func Test_createWriteRequests(t *testing.T) {
	p, err := createProcessor()
	assert.Nil(t, err)

	m := p.createWriteRequests(testWRQ)

	mExp := map[string]*prompb.WriteRequest{
		"foobar": testWRQ1,
		"foobaz": testWRQ2,
	}

	assert.Equal(t, mExp, m)
}

func Benchmark_marshal(b *testing.B) {
	p, _ := createProcessor()
	buf := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, _ = p.marshal(testWRQ, buf)
		_, _ = p.unmarshal(buf)
	}
}
