package processor

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/google/uuid"
	me "github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/prompb"
	fh "github.com/valyala/fasthttp"

	"github.com/projectcapsule/cortex-tenant/internal/config"
	"github.com/projectcapsule/cortex-tenant/internal/metrics"
	"github.com/projectcapsule/cortex-tenant/internal/stores"
)

type result struct {
	code     int
	body     []byte
	duration float64
	tenant   string
	err      error
}

type Processor struct {
	cfg config.Config

	srv *fh.Server
	cli *fh.Client

	shuttingDown uint32

	logr.Logger

	auth struct {
		egressHeader []byte
	}

	// Tenant store
	store *stores.TenantStore
	// Metrics Recorder
	metrics *metrics.Recorder
}

func NewProcessor(
	log logr.Logger,
	c config.Config,
	store *stores.TenantStore,
	metrics *metrics.Recorder,
) *Processor {
	p := &Processor{
		cfg:     c,
		Logger:  log,
		store:   store,
		metrics: metrics,
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
		MaxConnsPerHost:    c.MaxConnsPerHost,
		DialDualStack:      c.EnableIPv6,
		MaxConnDuration:    c.MaxConnDuration,
	}

	if c.Backend.Auth.Username != "" {
		authString := []byte(fmt.Sprintf("%s:%s", c.Backend.Auth.Username, c.Backend.Auth.Password))
		p.auth.egressHeader = []byte("Basic " + base64.StdEncoding.EncodeToString(authString))
	}

	// For testing
	if c.PipeOut != nil {
		p.cli.Dial = func(_ string) (net.Conn, error) {
			return c.PipeOut.Dial()
		}
	}

	return p
}

// Start implements the Runnable interface
// It should block until the context is done (i.e. shutdown is triggered).
func (p *Processor) Start(ctx context.Context) error {
	// Run your processor (blocking call)
	if err := p.run(); err != nil {
		return fmt.Errorf("failed to run processor: %w", err)
	}

	// Wait for shutdown signal via the context
	<-ctx.Done()

	// Perform any graceful shutdown/cleanup
	if err := p.close(); err != nil {
		return fmt.Errorf("failed to shutdown processor: %w", err)
	}

	return nil
}

//nolint:gosec
func (p *Processor) run() (err error) {
	var l net.Listener

	// For testing
	if p.cfg.PipeIn == nil {
		if l, err = net.Listen("tcp", "0.0.0.0:8080"); err != nil {
			return
		}
	} else {
		l = p.cfg.PipeIn
	}

	//nolint:errcheck
	go p.srv.Serve(l)

	return
}

func (p *Processor) handle(ctx *fh.RequestCtx) {
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

	p.metrics.MetricTimeseriesBatchesReceivedBytes.Observe(float64(ctx.Request.Header.ContentLength()))
	p.metrics.MetricTimeseriesBatchesReceived.Inc()

	wrReqIn, err := p.unmarshal(ctx.Request.Body())
	if err != nil {
		ctx.Error(err.Error(), fh.StatusBadRequest)

		return
	}

	tenantPrefix := p.cfg.Tenant.Prefix

	if p.cfg.Tenant.PrefixPreferSource {
		sourceTenantPrefix := string(ctx.Request.Header.Peek(p.cfg.Tenant.Header))
		if sourceTenantPrefix != "" {
			tenantPrefix = sourceTenantPrefix + "-"
		}
	}

	clientIP := ctx.RemoteAddr()
	reqID, _ := uuid.NewRandom()

	//nolint:nestif
	if len(wrReqIn.Timeseries) == 0 {
		// If there's metadata - just accept the request and drop it
		if len(wrReqIn.Metadata) > 0 {
			if p.cfg.Metadata && p.cfg.Tenant.Default != "" {
				r := p.send(*p.cfg.Backend, clientIP, reqID, tenantPrefix+p.cfg.Tenant.Default, wrReqIn)
				if r.err != nil {
					ctx.Error(r.err.Error(), fh.StatusInternalServerError)
					p.Error(r.err, "src=%s req_id=%s: unable to proxy metadata: %s", clientIP, reqID)

					return
				}

				ctx.SetStatusCode(r.code)
				ctx.SetBody(r.body)
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

	metricTenant := ""

	var errs *me.Error

	results := p.dispatch(clientIP, reqID, tenantPrefix, m)

	code, body := 0, []byte("Ok")

	// Return 204 regardless of errors if AcceptAll is enabled
	if p.cfg.Tenant.AcceptAll {
		code, body = 204, nil

		goto out
	}

	for _, r := range results {
		p.metrics.MetricTimeseriesRequests.WithLabelValues(r.tenant).Inc()

		if r.err != nil {
			p.metrics.MetricTimeseriesRequestErrors.WithLabelValues(r.tenant).Inc()
			errs = me.Append(errs, r.err)
			p.Error(r.err, "request failed", "source", clientIP)

			continue
		}

		if r.code < 200 || r.code >= 300 {
			p.Info("src=%s req_id=%s HTTP code %d (%s)", clientIP, reqID, r.code, string(r.body))
		}

		if r.code > code {
			code, body = r.code, r.body
		}

		p.metrics.MetricTimeseriesRequestDurationSeconds.WithLabelValues(strconv.Itoa(r.code), metricTenant).Observe(r.duration)
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

func (p *Processor) createWriteRequests(wrReqIn *prompb.WriteRequest) (map[string]*prompb.WriteRequest, error) {
	// Create per-tenant write requests
	m := map[string]*prompb.WriteRequest{}

	for _, ts := range wrReqIn.Timeseries {
		tenant, err := p.processTimeseries(&ts)
		if err != nil {
			return nil, err
		}

		// Tenant & Total
		p.metrics.MetricTimeseriesReceived.WithLabelValues(tenant).Inc()
		p.metrics.MetricTimeseriesReceived.WithLabelValues("").Inc()

		wrReqOut, ok := m[tenant]
		if !ok {
			wrReqOut = &prompb.WriteRequest{}
			m[tenant] = wrReqOut
		}

		wrReqOut.Timeseries = append(wrReqOut.Timeseries, ts)
	}

	return m, nil
}

func (p *Processor) unmarshal(b []byte) (*prompb.WriteRequest, error) {
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

func (p *Processor) marshal(wr *prompb.WriteRequest) (bufOut []byte, err error) {
	b := make([]byte, wr.Size())

	// Marshal to Protobuf
	if _, err = wr.MarshalTo(b); err != nil {
		return
	}

	// Compress with Snappy
	return snappy.Encode(nil, b), nil
}

func (p *Processor) dispatch(clientIP net.Addr, reqID uuid.UUID, tenantPrefix string, m map[string]*prompb.WriteRequest) (res []result) {
	var wg sync.WaitGroup

	res = make([]result, len(m))

	i := 0

	for tenant, wrReq := range m {
		wg.Add(1)

		go func(idx int, tenant string, wrReq *prompb.WriteRequest) {
			defer wg.Done()

			r := p.send(*p.cfg.Backend, clientIP, reqID, tenant, wrReq)
			res[idx] = r
		}(i, tenantPrefix+tenant, wrReq)

		i++
	}

	wg.Wait()

	return
}

func removeOrdered(slice []prompb.Label, s int) []prompb.Label {
	return append(slice[:s], slice[s+1:]...)
}

func (p *Processor) processTimeseries(ts *prompb.TimeSeries) (tenant string, err error) {
	idx := 0

	var namespace string

	for i, l := range ts.Labels {
		for _, configuredLabel := range p.cfg.Tenant.Labels {
			if l.Name == configuredLabel {
				p.Logger.Info("found", "label", configuredLabel, "value", l.Value)

				namespace = l.Value
				idx = i

				break
			}
		}
	}

	tenant = p.store.GetTenant(namespace)

	if tenant == "" {
		if p.cfg.Tenant.Default == "" {
			return "", fmt.Errorf("label(s): {'%s'} not found", strings.Join(p.cfg.Tenant.Labels, "','"))
		}

		return p.cfg.Tenant.Default, nil
	}

	if p.cfg.Tenant.LabelRemove {
		// Order is important. See:
		// https://github.com/thanos-io/thanos/issues/6452
		// https://github.com/prometheus/prometheus/issues/11505
		ts.Labels = removeOrdered(ts.Labels, idx)
	}

	return
}

func (p *Processor) send(backend config.CortexBackend, clientIP net.Addr, reqID uuid.UUID, tenant string, wr *prompb.WriteRequest) (r result) {
	start := time.Now()
	r.tenant = tenant

	req := fh.AcquireRequest()
	resp := fh.AcquireResponse()

	defer func() {
		fh.ReleaseRequest(req)
		fh.ReleaseResponse(resp)
	}()

	buf, err := p.marshal(wr)
	if err != nil {
		r.err = err

		return
	}

	p.fillRequestHeaders(clientIP, reqID, tenant, req)

	if p.auth.egressHeader != nil {
		req.Header.SetBytesV("Authorization", p.auth.egressHeader)
	}

	req.Header.SetMethod(fh.MethodPost)
	req.SetRequestURI(backend.URL)
	req.SetBody(buf)

	if err = p.cli.DoTimeout(req, resp, p.cfg.Timeout); err != nil {
		r.err = err

		return
	}

	r.code = resp.Header.StatusCode()
	r.body = make([]byte, len(resp.Body()))
	copy(r.body, resp.Body())
	r.duration = time.Since(start).Seconds()

	return
}

func (p *Processor) fillRequestHeaders(
	clientIP net.Addr,
	reqID uuid.UUID,
	tenant string,
	req *fh.Request,
) {
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("X-Cortex-Tenant-Client", clientIP.String())
	req.Header.Set("X-Cortex-Tenant-ReqID", reqID.String())
	req.Header.Set(p.cfg.Tenant.Header, tenant)
}

func (p *Processor) close() (err error) {
	// Signal that we're shutting down
	atomic.StoreUint32(&p.shuttingDown, 1)
	// Let healthcheck detect that we're offline
	time.Sleep(p.cfg.TimeoutShutdown)
	// Shutdown
	return p.srv.Shutdown()
}
