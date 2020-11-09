package main

import (
	"bytes"
	"fmt"
	"net"
	"sync"
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

	var wrReqIn prompb.WriteRequest

	if !bytes.Equal(ctx.Request.Header.Method(), []byte("POST")) {
		ctx.Error("Expecting POST", fh.StatusBadRequest)
		return
	}

	if !bytes.Equal(ctx.Path(), []byte("/push")) {
		ctx.Error("Unknown URL", fh.StatusNotFound)
		return
	}

	buf := bufferPool.Get().(*buffer)
	buf.grow()
	defer bufferPool.Put(buf)

	decoded, err := snappy.Decode(buf.b, ctx.Request.Body())
	if err != nil {
		msg := fmt.Sprintf("Unable to unpack Snappy: %s", err)
		ctx.Error(msg, fh.StatusBadRequest)
		p.Warnf(msg)
		return
	}

	if err = proto.Unmarshal(decoded, &wrReqIn); err != nil {
		msg := fmt.Sprintf("Unable to unmarshal protobuf: %s", err)
		ctx.Error(msg, fh.StatusBadRequest)
		p.Warnf(msg)
		return
	}

	ip := ctx.RemoteAddr()

	// Create per-tenant write requests
	m := map[string]*prompb.WriteRequest{}
	samples := 0

	for _, ts := range wrReqIn.Timeseries {
		samples += len(ts.Samples)
		tenant := p.processTimeseries(ts)

		wrReqOut, ok := m[tenant]
		if !ok {
			wrReqOut = &prompb.WriteRequest{}
			m[tenant] = wrReqOut
		}

		wrReqOut.Timeseries = append(wrReqOut.Timeseries, ts)
		p.Debugf("src=%s tenant=%s labels=%+v", ip, tenant, ts.Labels)
	}

	ok := 0
	var res result
	for _, r := range p.dispatch(m) {
		if r.err != nil {
			err = r.err
			p.Errorf("src=%s %s", ip, err)
		} else if r.code < 200 || r.code > 299 {
			if res.code == 0 {
				res = r
			}

			p.Errorf("src=%s http code not 2xx (%d): %s", ip, r.code, string(r.body))
		} else {
			ok++
		}
	}

	if err != nil {
		ctx.Error(err.Error(), fh.StatusInternalServerError)
	} else if res.code != 0 {
		ctx.SetStatusCode(res.code)
		ctx.SetBody(res.body)
	}

	p.Debugf("src=%s timeseries=%d samples=%d requests_ok=%d/%d", ip, len(wrReqIn.Timeseries), samples, ok, len(m))
	return
}

func (p *processor) dispatch(m map[string]*prompb.WriteRequest) (res []result) {
	var mtx sync.Mutex
	res = make([]result, len(m))

	for tenant, wrReq := range m {
		go func(tenant string, wrReq *prompb.WriteRequest) {
			var r result
			r.code, r.body, r.err = p.send(tenant, wrReq)

			mtx.Lock()
			res = append(res, r)
			mtx.Unlock()
		}(tenant, wrReq)
	}

	return
}

func (p *processor) processTimeseries(ts *prompb.TimeSeries) (tenant string) {
	labelIdx := 0
	for i, l := range ts.Labels {
		if l.Name == p.cfg.Tenant.Label {
			tenant, labelIdx = l.Value, i
			break
		}
	}

	if tenant == "" {
		return p.cfg.Tenant.Default
	}

	if p.cfg.Tenant.LabelRemove {
		l := len(ts.Labels)
		ts.Labels[labelIdx] = ts.Labels[l-1]
		ts.Labels = ts.Labels[:l-1]
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

	// Marshal to Protobuf
	var l int
	buf1.grow()
	if l, err = wr.MarshalTo(buf1.b); err != nil {
		return
	}

	// Compress with Snappy
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
