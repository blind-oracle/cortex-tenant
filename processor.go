package main

import (
	"bytes"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blind-oracle/go-common/logger"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/google/uuid"
	"github.com/pkg/errors"
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

	shuttingDown uint32

	logger.Logger
}

func newProcessor(c config) *processor {
	p := &processor{
		cfg:    c,
		Logger: logger.NewSimpleLogger("proc"),
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

	if c.pipeOut != nil {
		p.cli.Dial = func(a string) (net.Conn, error) {
			return c.pipeOut.Dial()
		}
	}

	return p
}

func (p *processor) run() (err error) {
	var l net.Listener

	if p.cfg.pipeIn == nil {
		if l, err = net.Listen("tcp", p.cfg.Listen); err != nil {
			return
		}
	} else {
		l = p.cfg.pipeIn
	}

	go p.srv.Serve(l)
	return nil
}

func (p *processor) handle(ctx *fh.RequestCtx) {
	if bytes.Equal(ctx.Path(), []byte("/alive")) {
		if atomic.LoadUint32(&p.shuttingDown) == 1 {
			ctx.SetStatusCode(fh.StatusServiceUnavailable)
		}

		return
	}

	if !bytes.Equal(ctx.Request.Header.Method(), []byte("POST")) {
		ctx.Error("Expecting POST", fh.StatusBadRequest)
		return
	}

	if !bytes.Equal(ctx.Path(), []byte("/push")) {
		ctx.Error("Unknown URL", fh.StatusNotFound)
		return
	}

	wrReqIn, err := p.unmarshal(ctx.Request.Body())
	if err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)
		return
	}

	clientIP := ctx.RemoteAddr()
	reqID, _ := uuid.NewRandom()

	ok := 0
	var res result

	m := p.createWriteRequests(wrReqIn)

	for _, r := range p.dispatch(clientIP, reqID, m) {
		if r.err != nil {
			err = r.err
			p.Errorf("src=%s %s", clientIP, err)
		} else if r.code < 200 || r.code > 299 {
			if res.code == 0 {
				res = r
			}

			p.Errorf("src=%s req_id=%s http code not 2xx (%d): %s", clientIP, reqID, r.code, string(r.body))
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

	return
}

func (p *processor) createWriteRequests(in *prompb.WriteRequest) map[string]*prompb.WriteRequest {
	// Create per-tenant write requests
	m := map[string]*prompb.WriteRequest{}

	for _, ts := range in.Timeseries {
		tenant := p.processTimeseries(ts)

		wrReqOut, ok := m[tenant]
		if !ok {
			wrReqOut = &prompb.WriteRequest{}
			m[tenant] = wrReqOut
		}

		wrReqOut.Timeseries = append(wrReqOut.Timeseries, ts)
	}

	return m
}

func (p *processor) unmarshal(b []byte) (*prompb.WriteRequest, error) {
	buf := bufferPool.Get().(*buffer)
	buf.reset()
	defer bufferPool.Put(buf)

	decoded, err := snappy.Decode(buf.b, b)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to unpack Snappy")
	}

	req := &prompb.WriteRequest{}
	if err = proto.Unmarshal(decoded, req); err != nil {
		return nil, errors.Wrap(err, "Unable to unmarshal protobuf")
	}

	return req, nil
}

func (p *processor) marshal(wr *prompb.WriteRequest, bufDst []byte) (bufOut []byte, err error) {
	buf := bufferPool.Get().(*buffer)
	buf.reset()
	defer bufferPool.Put(buf)

	// Marshal to Protobuf
	l, err := wr.MarshalTo(buf.b)
	if err != nil {
		return
	}

	// Compress with Snappy
	return snappy.Encode(bufDst, buf.b[:l]), nil
}

func (p *processor) dispatch(clientIP net.Addr, reqID uuid.UUID, m map[string]*prompb.WriteRequest) (res []result) {
	var wg sync.WaitGroup
	res = make([]result, len(m))

	i := 0
	for tenant, wrReq := range m {
		wg.Add(1)

		go func(idx int, tenant string, wrReq *prompb.WriteRequest) {
			defer wg.Done()

			var r result
			r.code, r.body, r.err = p.send(clientIP, reqID, tenant, wrReq)
			res[idx] = r
		}(i, tenant, wrReq)

		i++
	}

	wg.Wait()
	return
}

func (p *processor) processTimeseries(ts *prompb.TimeSeries) (tenant string) {
	idx := 0
	for i, l := range ts.Labels {
		if l.Name == p.cfg.Tenant.Label {
			tenant, idx = l.Value, i
			break
		}
	}

	if tenant == "" {
		return p.cfg.Tenant.Default
	}

	if p.cfg.Tenant.LabelRemove {
		l := len(ts.Labels)
		ts.Labels[idx] = ts.Labels[l-1]
		ts.Labels = ts.Labels[:l-1]
	}

	return
}

func (p *processor) send(clientIP net.Addr, reqID uuid.UUID, tenant string, wr *prompb.WriteRequest) (code int, body []byte, err error) {
	buf := bufferPool.Get().(*buffer)
	buf.reset()

	req := fh.AcquireRequest()
	resp := fh.AcquireResponse()

	defer func() {
		fh.ReleaseRequest(req)
		fh.ReleaseResponse(resp)
		bufferPool.Put(buf)
	}()

	if buf.b, err = p.marshal(wr, buf.b); err != nil {
		return
	}

	req.Header.SetMethod("POST")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("X-Cortex-Tenant-Client", clientIP.String())
	req.Header.Set("X-Cortex-Tenant-ReqID", reqID.String())
	req.Header.Set(p.cfg.Tenant.Header, tenant)

	req.SetRequestURI(p.cfg.Target)
	req.SetBody(buf.b)

	if err = p.cli.Do(req, resp); err != nil {
		return
	}

	body = make([]byte, len(resp.Body()))
	copy(body, resp.Body())
	return resp.Header.StatusCode(), body, nil
}

func (p *processor) close() (err error) {
	// Signal that we're shutting down
	atomic.StoreUint32(&p.shuttingDown, 1)
	// Let healthcheck detect that we're offline
	time.Sleep(p.cfg.TimeoutShutdown)
	// Shutdown
	return p.srv.Shutdown()
}
