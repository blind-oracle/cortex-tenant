package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blind-oracle/go-common/logger"
	"github.com/dyson/certman"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	fh "github.com/valyala/fasthttp"
)

type result struct {
	code     int
	body     []byte
	duration float64
	tenant   string
	err      error
}

// egressContentType describes the protocol-specific HTTP headers to set on
// outgoing requests to the upstream backend.
type egressContentType struct {
	contentType     string
	contentEncoding string
	promRemoteWrite bool
}

var (
	// contentTypePromRW is used for Prometheus remote write and Loki push,
	// both of which send snappy-compressed protobuf.
	contentTypePromRW = egressContentType{
		contentType:     "application/x-protobuf",
		contentEncoding: "snappy",
		promRemoteWrite: true,
	}

	// contentTypeOTLP is used for OTLP/HTTP, sending uncompressed protobuf.
	contentTypeOTLP = egressContentType{
		contentType:     "application/x-protobuf",
		contentEncoding: "",
		promRemoteWrite: false,
	}

	// contentTypeOTLPGzip is used for OTLP/HTTP when egress gzip compression is
	// enabled, sending gzip-compressed protobuf.
	contentTypeOTLPGzip = egressContentType{
		contentType:     "application/x-protobuf",
		contentEncoding: "gzip",
		promRemoteWrite: false,
	}
)

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

func newProcessor(c config) (*processor, error) {
	p := &processor{
		cfg:    c,
		Logger: logger.NewSimpleLogger("proc"),
	}

	p.srv = &fh.Server{
		Name:    "cortex-tenant",
		Handler: p.handle,

		MaxRequestBodySize: c.MaxRequestBodySize,

		ReadTimeout:  c.Timeout,
		WriteTimeout: c.Timeout,
		IdleTimeout:  c.IdleTimeout,

		Concurrency: c.Concurrency,

		TLSConfig: &tls.Config{},
	}

	p.cli = &fh.Client{
		Name:               "cortex-tenant",
		ReadTimeout:        c.Timeout,
		WriteTimeout:       c.Timeout,
		MaxConnWaitTimeout: 1 * time.Second,
		MaxConnsPerHost:    c.MaxConnsPerHost,
		DialDualStack:      c.EnableIPv6,
		MaxConnDuration:    c.MaxConnDuration,
		TLSConfig:          &tls.Config{},
	}

	if caFile := c.Auth.Egress.TlsConfig.CaBundleFile; caFile != "" {
		caCert, err := ioutil.ReadFile(caFile)
		if err != nil {
			return nil, errors.Wrap(err, "Unable to load CA Bundle")
		}

		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		p.cli.TLSConfig.RootCAs = caCertPool
	}

	if c.Auth.Egress.Username != "" {
		authString := []byte(fmt.Sprintf("%s:%s", c.Auth.Egress.Username, c.Auth.Egress.Password))
		p.auth.egressHeader = []byte("Basic " + base64.StdEncoding.EncodeToString(authString))
	}

	if c.Auth.Ingress.TlsConfig.CertFile != "" && c.Auth.Ingress.TlsConfig.KeyFile != "" {
		cm, err := certman.New(
			c.Auth.Ingress.TlsConfig.CertFile,
			c.Auth.Ingress.TlsConfig.KeyFile,
		)
		if err != nil {
			return nil, errors.Wrap(err, "Unable to configure server TLS")
		}
		err = cm.Watch()
		if err != nil {
			return nil, errors.Wrap(err, "Unable to configure server TLS")
		}

		p.srv.TLSConfig.GetCertificate = cm.GetCertificate
	}

	// For testing
	if c.pipeOut != nil {
		p.cli.Dial = func(a string) (net.Conn, error) {
			return c.pipeOut.Dial()
		}
	}

	return p, nil
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

	if p.srv.TLSConfig.GetCertificate == nil {
		go p.srv.Serve(l)
	} else {
		// Just pass empty certFile and keyFile to serveTLS because we have
		// overriden the static behaviour with CertMan in the processor setup.
		go p.srv.ServeTLS(l, "", "")
	}
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

	if bytes.Equal(ctx.Path(), []byte("/push")) {
		p.handleMetrics(ctx)
		return
	}

	if bytes.Equal(ctx.Path(), []byte("/loki/push")) {
		p.handleLogs(ctx)
		return
	}

	if bytes.Equal(ctx.Path(), []byte("/v1/metrics")) {
		p.handleOTLPMetrics(ctx)
		return
	}

	if bytes.Equal(ctx.Path(), []byte("/v1/traces")) {
		p.handleOTLPTraces(ctx)
		return
	}

	ctx.SetStatusCode(fh.StatusNotFound)
}

// otlpEgressContentType returns the egress content type used for OTLP forwarding,
// selecting the gzip-compressed variant when OTLPEgressGzip is enabled.
func (p *processor) otlpEgressContentType() egressContentType {
	if p.cfg.OTLPEgressGzip {
		return contentTypeOTLPGzip
	}
	return contentTypeOTLP
}

func (p *processor) dispatch(target string, clientIP net.Addr, reqID uuid.UUID, tenantPrefix string, m map[string]func() ([]byte, error), ct egressContentType) (res []result) {
	var wg sync.WaitGroup
	res = make([]result, len(m))

	i := 0
	for tenant, bodyFunc := range m {
		wg.Add(1)

		go func(idx int, tenant string, bodyFunc func() ([]byte, error)) {
			defer wg.Done()

			r := p.send(target, clientIP, reqID, tenant, bodyFunc, ct)
			res[idx] = r
		}(i, tenantPrefix+tenant, bodyFunc)

		i++
	}

	wg.Wait()
	return
}

func (p *processor) send(target string, clientIP net.Addr, reqID uuid.UUID, tenant string, bodyFunc func() ([]byte, error), ct egressContentType) (r result) {
	start := time.Now()
	r.tenant = tenant

	req := fh.AcquireRequest()
	resp := fh.AcquireResponse()

	defer func() {
		fh.ReleaseRequest(req)
		fh.ReleaseResponse(resp)
	}()

	buf, err := bodyFunc()
	if err != nil {
		r.err = err
		return
	}

	// Compress the payload if the egress protocol requests gzip. Snappy-encoded
	// payloads (Prometheus remote write / Loki) are already compressed by their
	// handlers, so only gzip is applied here.
	if ct.contentEncoding == "gzip" {
		buf = fh.AppendGzipBytes(nil, buf)
	}

	p.fillRequestHeaders(clientIP, reqID, tenant, ct, req)

	if p.auth.egressHeader != nil {
		req.Header.SetBytesV("Authorization", p.auth.egressHeader)
	}

	req.Header.SetMethod(fh.MethodPost)
	req.SetRequestURI(target)
	req.SetBody(buf)

	if err = p.cli.DoTimeout(req, resp, p.cfg.Timeout); err != nil {
		r.err = err
		return
	}

	r.code = resp.Header.StatusCode()
	r.body = make([]byte, len(resp.Body()))
	copy(r.body, resp.Body())
	r.duration = time.Since(start).Seconds() / 1000

	return
}

func (p *processor) fillRequestHeaders(
	clientIP net.Addr, reqID uuid.UUID, tenant string, ct egressContentType, req *fh.Request) {
	if ct.contentEncoding != "" {
		req.Header.Set("Content-Encoding", ct.contentEncoding)
	}
	req.Header.Set("Content-Type", ct.contentType)
	if ct.promRemoteWrite {
		req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	}
	req.Header.Set("X-Cortex-Tenant-Client", clientIP.String())
	req.Header.Set("X-Cortex-Tenant-ReqID", reqID.String())
	req.Header.Set(p.cfg.Tenant.Header, tenant)
}

func (p *processor) close() (err error) {
	// Signal that we're shutting down
	atomic.StoreUint32(&p.shuttingDown, 1)
	// Let healthcheck detect that we're offline
	time.Sleep(p.cfg.TimeoutShutdown)
	// Shutdown
	return p.srv.Shutdown()
}
