package main

import (
	"bytes"
	"fmt"
	"net"
	"time"

	"github.com/blind-oracle/go-common/logger"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
	fh "github.com/valyala/fasthttp"
)

type result struct {
	code int
	body []byte
	err  error
}

type processor struct {
	cfg config

	srv *fh.Server
	cli *fh.Client

	logger.Logger
}

func newProcessor(c config) (p *processor, err error) {
	p = &processor{
		cfg:    c,
		Logger: logger.NewSimpleLogger("http"),
	}

	p.srv = &fh.Server{
		Name:    "cortex-tenant",
		Handler: p.handle,

		MaxRequestBodySize: 8 * 1024 * 1024,

		ReadTimeout:  c.Timeout,
		WriteTimeout: c.Timeout,
		IdleTimeout:  60 * time.Second,
	}

	p.cli = &fh.Client{
		Name:               "cortex-tenant",
		ReadTimeout:        c.Timeout,
		WriteTimeout:       c.Timeout,
		MaxConnWaitTimeout: 1 * time.Second,
		MaxConnsPerHost:    64,
	}

	l, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return nil, err
	}

	go p.srv.Serve(l)

	p.Warnf("Listening on %s", c.Listen)
	p.Warnf("Sending to %s", c.Target)
	return
}

func (p *processor) handle(ctx *fh.RequestCtx) {
	if bytes.Equal(ctx.Path(), []byte("/alive")) {
		return
	}

	var (
		rq      prompb.WriteRequest
		buf     *buffer
		decoded []byte
		err     error
	)

	if !bytes.Equal(ctx.Request.Header.Method(), []byte("POST")) {
		ctx.Error("Expecting POST", fh.StatusBadRequest)
		return
	}

	if !bytes.Equal(ctx.Path(), []byte("/push")) {
		ctx.Error("Unknown URL", fh.StatusNotFound)
		return
	}

	buf = bufferPool.Get().(*buffer)
	defer bufferPool.Put(buf)

	buf.grow()
	if decoded, err = snappy.Decode(buf.b, ctx.Request.Body()); err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)
		return
	}

	if err = proto.Unmarshal(decoded, &rq); err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)
		return
	}

	// Create per-tenant write requests
	m := map[string]*prompb.WriteRequest{}
	for _, ts := range rq.Timeseries {
		tenant := p.processTimeseries(ts)

		var (
			wr *prompb.WriteRequest
			ok bool
		)

		if wr, ok = m[tenant]; !ok {
			wr = &prompb.WriteRequest{}
			m[tenant] = wr
		}

		wr.Timeseries = append(wr.Timeseries, ts)
	}

	resultCh := make(chan result, len(m))

	for tenant, wr := range m {
		go func(tenant string, wr *prompb.WriteRequest) {
			var r result
			r.code, r.body, r.err = p.send(tenant, wr)
			resultCh <- r
		}(tenant, wr)
	}

	for i := 0; i < len(m); i++ {
		r := <-resultCh

		if r.err != nil {
			msg := fmt.Sprintf("HTTP request failed: %s", r.err)
			ctx.Error(msg, fh.StatusInternalServerError)
			p.Errorf(msg)
			return
		}

		if r.code < 200 || r.code > 299 {
			ctx.SetBody(r.body)
			ctx.SetStatusCode(r.code)
			p.Errorf("HTTP code is not 2xx (%d): %s", r.code, string(r.body))
			return
		}
	}

	return
}

func (p *processor) processTimeseries(ts *prompb.TimeSeries) (tenant string) {
	j := 0
	for i, l := range ts.Labels {
		if l.Name == p.cfg.Tenant.Label {
			tenant, j = l.Value, i
			break
		}
	}

	if tenant == "" {
		tenant = p.cfg.Tenant.Default
	} else if p.cfg.Tenant.LabelRemove {
		cnt := len(ts.Labels)
		ts.Labels[j] = ts.Labels[cnt-1]
		ts.Labels = ts.Labels[:cnt-1]
	}

	return
}

func (p *processor) send(tenant string, wr *prompb.WriteRequest) (code int, body []byte, err error) {
	req := fh.AcquireRequest()
	resp := fh.AcquireResponse()
	buf1 := bufferPool.Get().(*buffer)
	buf2 := bufferPool.Get().(*buffer)

	defer func() {
		fh.ReleaseRequest(req)
		fh.ReleaseResponse(resp)
		bufferPool.Put(buf1)
		bufferPool.Put(buf2)
	}()

	var l int
	buf1.grow()
	if l, err = wr.MarshalTo(buf1.b); err != nil {
		return
	}

	buf2.grow()
	if buf2.b = snappy.Encode(buf2.b, buf1.b[:l]); err != nil {
		return
	}

	req.Header.SetMethod("POST")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set(p.cfg.Tenant.Header, tenant)

	req.SetRequestURI(p.cfg.Target)
	req.SetBody(buf2.b)

	if err = p.cli.Do(req, resp); err != nil {
		return
	}

	body = make([]byte, len(resp.Body()))
	copy(body, resp.Body())
	return resp.Header.StatusCode(), body, nil
}

func (p *processor) close() (err error) {
	return p.srv.Shutdown()
}
