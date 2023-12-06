package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blind-oracle/go-common/logger"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/google/uuid"
	me "github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/prompb"
	fh "github.com/valyala/fasthttp"
)

var (
	metricTimeseriesBatchesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "timeseries_batches_received",
		Help:      "The total number of batches received.",
	})
	metricTimeseriesBatchesReceivedBytes = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex_tenant",
		Name:      "timeseries_batches_received_bytes",
		Help:      "Size in bytes of timeseries batches received.",
		Buckets:   []float64{0.5, 1, 10, 25, 100, 250, 500, 1000, 5000, 10000, 30000, 300000, 600000, 1800000, 3600000},
	})
	metricTimeseriesReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "timeseries_received",
		Help:      "The total number of timeseries received.",
	}, []string{"tenant"})
	metricTimeseriesRequestDurationMilliseconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex_tenant",
		Name:      "timeseries_request_duration_milliseconds",
		Help:      "HTTP write request duration for tenant-specific timeseries in milliseconds, filtered by response code.",
		Buckets:   []float64{0.5, 1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000, 1800000, 3600000},
	},
		[]string{"code", "tenant"},
	)
	metricTimeseriesRequestErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "timeseries_request_errors",
		Help:      "The total number of tenant-specific timeseries writes that yielded errors.",
	}, []string{"tenant"})
	metricTimeseriesRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex_tenant",
		Name:      "timeseries_requests",
		Help:      "The total number of tenant-specific timeseries writes.",
	}, []string{"tenant"})
)

type result struct {
	code     int
	body     []byte
	duration float64
	tenant   string
	err      error
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
		MaxConnsPerHost:    c.MaxConnsPerHost,
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

	metricTimeseriesBatchesReceivedBytes.Observe(float64(ctx.Request.Header.ContentLength()))
	metricTimeseriesBatchesReceived.Inc()
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
				r := p.send(clientIP, reqID, p.cfg.Tenant.Default, wrReqIn)
				if r.err != nil {
					ctx.Error(err.Error(), fh.StatusInternalServerError)
					p.Errorf("src=%s req_id=%s: unable to proxy metadata: %s", clientIP, reqID, r.err)
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
	results := p.dispatch(clientIP, reqID, m)

	code, body := 0, []byte("Ok")

	// Return 204 regardless of errors if AcceptAll is enabled
	if p.cfg.Tenant.AcceptAll {
		code, body = 204, nil
		goto out
	}

	for _, r := range results {
		if p.cfg.MetricsIncludeTenant {
			metricTenant = r.tenant
		}

		metricTimeseriesRequests.WithLabelValues(metricTenant).Inc()

		if r.err != nil {
			metricTimeseriesRequestErrors.WithLabelValues(metricTenant).Inc()
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

		metricTimeseriesRequestDurationMilliseconds.WithLabelValues(strconv.Itoa(r.code), metricTenant).Observe(r.duration)
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

		if p.cfg.MetricsIncludeTenant {
			metricTimeseriesReceived.WithLabelValues(tenant).Inc()
		} else {
			metricTimeseriesReceived.WithLabelValues("").Inc()
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

			r := p.send(clientIP, reqID, tenant, wrReq)
			res[idx] = r
		}(i, tenant, wrReq)

		i++
	}

	wg.Wait()
	return
}

func removeOrdered(slice []prompb.Label, s int) []prompb.Label {
	return append(slice[:s], slice[s+1:]...)
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
		// Order is important. See:
		// https://github.com/thanos-io/thanos/issues/6452
		// https://github.com/prometheus/prometheus/issues/11505
		ts.Labels = removeOrdered(ts.Labels, idx)
	}

	return
}

func (p *processor) send(clientIP net.Addr, reqID uuid.UUID, tenant string, wr *prompb.WriteRequest) (r result) {
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
	req.SetRequestURI(p.cfg.Target)
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
	clientIP net.Addr, reqID uuid.UUID, tenant string, req *fh.Request) {
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("X-Cortex-Tenant-Client", clientIP.String())
	req.Header.Set("X-Cortex-Tenant-ReqID", reqID.String())
	if p.cfg.Tenant.Prefix != "" {
		tenant = p.cfg.Tenant.Prefix + tenant
	}
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
