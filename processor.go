package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blind-oracle/go-common/logger"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/google/uuid"
	me "github.com/hashicorp/go-multierror"
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

	auth struct {
		egressHeader []byte
	}
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

		Concurrency: c.Concurrency,
	}

	p.cli = &fh.Client{
		Name:               "cortex-tenant",
		ReadTimeout:        c.Timeout,
		WriteTimeout:       c.Timeout,
		MaxConnWaitTimeout: 1 * time.Second,
		MaxConnsPerHost:    64,
		DialDualStack:      c.EnableIPv6,
		MaxConnDuration:    c.MaxConnDuration,
	}

	if c.Auth.Egress.Username != "" {
		authString := []byte(fmt.Sprintf("%s:%s", c.Auth.Egress.Username, c.Auth.Egress.Password))
		p.auth.egressHeader = []byte("Basic " + base64.StdEncoding.EncodeToString(authString))
	}

	// For testing
	if c.pipeOut != nil {
		p.cli.Dial = func(a string) (net.Conn, error) {
			return c.pipeOut.Dial()
		}
	}

	return p
}

func (p *processor) run() (err error) {
	var l net.Listener

	// For testing
	if p.cfg.pipeIn == nil {
		if l, err = net.Listen("tcp", p.cfg.Listen); err != nil {
			return
		}
	} else {
		l = p.cfg.pipeIn
	}

	go p.srv.Serve(l)
	return
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
		ctx.SetStatusCode(fh.StatusNotFound)
		return
	}

	wrReqIn, err := p.unmarshal(ctx.Request.Body())
	if err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)
		return
	}

	clientIP := ctx.RemoteAddr()
	reqID, _ := uuid.NewRandom()

	if len(wrReqIn.Timeseries) == 0 {
		// If there's metadata - just accept the request and drop it
		if len(wrReqIn.Metadata) > 0 {
			if p.cfg.Metadata && p.cfg.Tenant.Default != "" {
				code, body, err := p.send(clientIP, reqID, p.cfg.Tenant.Default, wrReqIn)
				if err != nil {
					ctx.Error(err.Error(), fh.StatusInternalServerError)
					p.Errorf("src=%s req_id=%s: unable to proxy metadata: %s", clientIP, reqID, err)
					return
				}

				ctx.SetStatusCode(code)
				ctx.SetBody(body)
			}

			return
		}

		ctx.Error("No timeseries found in the request", fh.StatusBadRequest)
		return
	}

	m, err := p.createWriteRequests(wrReqIn)
	if err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)
		return
	}

	var errs *me.Error
	results := p.dispatch(clientIP, reqID, m)

	code, body := 0, []byte("Ok")

	// Return 204 regardless of errors if AcceptAll is enabled
	if p.cfg.Tenant.AcceptAll {
		code, body = 204, nil
		goto out
	}

	for _, r := range results {
		if r.err != nil {
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

func (p *processor) createWriteRequests(wrReqIn *prompb.WriteRequest) (map[string]*prompb.WriteRequest, error) {
	// Create per-tenant write requests
	m := map[string]*prompb.WriteRequest{}

	for _, ts := range wrReqIn.Timeseries {
		tenant, err := p.processTimeseries(&ts)
		if err != nil {
			return nil, err
		}

		wrReqOut, ok := m[tenant]
		if !ok {
			wrReqOut = &prompb.WriteRequest{}
			m[tenant] = wrReqOut
		}

		wrReqOut.Timeseries = append(wrReqOut.Timeseries, ts)
	}

	return m, nil
}

func (p *processor) unmarshal(b []byte) (*prompb.WriteRequest, error) {
	decoded, err := snappy.Decode(nil, b)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to unpack Snappy")
	}

	req := &prompb.WriteRequest{}
	if err = proto.Unmarshal(decoded, req); err != nil {
		return nil, errors.Wrap(err, "Unable to unmarshal protobuf")
	}

	return req, nil
}

func (p *processor) marshal(wr *prompb.WriteRequest) (bufOut []byte, err error) {
	b := make([]byte, wr.Size())

	// Marshal to Protobuf
	if _, err = wr.MarshalTo(b); err != nil {
		return
	}

	// Compress with Snappy
	return snappy.Encode(nil, b), nil
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

func (p *processor) processTimeseries(ts *prompb.TimeSeries) (tenant string, err error) {
	idx := 0
	for i, l := range ts.Labels {
		if l.Name == p.cfg.Tenant.Label {
			tenant, idx = l.Value, i
			break
		}
	}

	if tenant == "" {
		if p.cfg.Tenant.Default == "" {
			return "", fmt.Errorf("label '%s' not found", p.cfg.Tenant.Label)
		}

		return p.cfg.Tenant.Default, nil
	}

	if p.cfg.Tenant.LabelRemove {
		l := len(ts.Labels)
		ts.Labels[idx] = ts.Labels[l-1]
		ts.Labels = ts.Labels[:l-1]
	}

	return
}

func (p *processor) send(clientIP net.Addr, reqID uuid.UUID, tenant string, wr *prompb.WriteRequest) (code int, body []byte, err error) {
	req := fh.AcquireRequest()
	resp := fh.AcquireResponse()

	defer func() {
		fh.ReleaseRequest(req)
		fh.ReleaseResponse(resp)
	}()

	buf, err := p.marshal(wr)
	if err != nil {
		return
	}

	req.Header.SetMethod("POST")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("X-Cortex-Tenant-Client", clientIP.String())
	req.Header.Set("X-Cortex-Tenant-ReqID", reqID.String())
	if p.cfg.Tenant.LabelPrefix != "" {
		tenant = fmt.Sprintf("%s-%s", p.cfg.Tenant.LabelPrefix, tenant)
	}
	req.Header.Set(p.cfg.Tenant.Header, tenant)

	if p.auth.egressHeader != nil {
		req.Header.SetBytesV("Authorization", p.auth.egressHeader)
	}

	req.SetRequestURI(p.cfg.Target)
	req.SetBody(buf)

	if err = p.cli.DoTimeout(req, resp, p.cfg.Timeout); err != nil {
		return
	}

	code = resp.Header.StatusCode()
	body = make([]byte, len(resp.Body()))
	copy(body, resp.Body())

	return
}

func (p *processor) close() (err error) {
	// Signal that we're shutting down
	atomic.StoreUint32(&p.shuttingDown, 1)
	// Let healthcheck detect that we're offline
	time.Sleep(p.cfg.TimeoutShutdown)
	// Shutdown
	return p.srv.Shutdown()
}
