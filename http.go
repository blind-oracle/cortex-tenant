package main

import (
	"bytes"
	"net"
	"sync"
	"time"

	"github.com/blind-oracle/go-common/logger"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	me "github.com/hashicorp/go-multierror"
	"github.com/prometheus/prometheus/prompb"
	fh "github.com/valyala/fasthttp"
)

type httpServer struct {
	cfg config
	srv *fh.Server

	tenants map[string]*tenant
	chClose chan struct{}

	wg sync.WaitGroup
	sync.RWMutex
	logger.Logger
}

func newHTTPServer(c config) (s *httpServer, err error) {
	s = &httpServer{
		cfg:     c,
		tenants: make(map[string]*tenant, c.MaxTenants),
		chClose: make(chan struct{}),
		Logger:  logger.NewSimpleLogger("http"),
	}

	s.srv = &fh.Server{
		Name:    "cortex-tenant",
		Handler: s.handle,

		MaxRequestBodySize: 8 * 1024 * 1024,

		ReadTimeout:  c.Timeout,
		WriteTimeout: c.Timeout,
		IdleTimeout:  60 * time.Second,
	}

	l, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return nil, err
	}

	go s.srv.Serve(l)
	s.wg.Add(1)
	go s.recycler()
	return
}

func (s *httpServer) handle(ctx *fh.RequestCtx) {
	var (
		rq      prompb.WriteRequest
		buf     *buffer
		decoded []byte
		err     error
	)

	if !bytes.Equal(ctx.Request.Header.Method(), []byte("POST")) {
		goto bad
	}

	if !bytes.Equal(ctx.Path(), []byte("/push")) {
		goto bad
	}

	buf = bufferPool.Get().(*buffer)
	defer bufferPool.Put(buf)

	buf.grow()
	if decoded, err = snappy.Decode(buf.b, ctx.Request.Body()); err != nil {
		goto bad
	}

	if err = proto.Unmarshal(decoded, &rq); err != nil {
		goto bad
	}

	for _, ts := range rq.Timeseries {
		s.processTimeseries(ts)
	}

	return

bad:
	msg := "Bad request"
	if err != nil {
		msg += ": " + err.Error()
	}

	ctx.Error(msg, fh.StatusBadRequest)
}

func (s *httpServer) processTimeseries(ts *prompb.TimeSeries) {
	var (
		tenant string
		idx    int
	)

	for i, l := range ts.Labels {
		if l.Name == s.cfg.Tenant.Label {
			tenant = l.Value
			idx = i
			break
		}
	}

	if tenant == "" {
		tenant = s.cfg.Tenant.Default
	} else if s.cfg.Tenant.LabelRemove {
		cnt := len(ts.Labels)
		ts.Labels[idx] = ts.Labels[cnt-1]
		ts.Labels = ts.Labels[:cnt-1]
	}

	s.RLock()
	t := s.tenants[tenant]
	s.RUnlock()

	if t == nil {
		s.Lock()
		if s.cfg.MaxTenants > 0 && len(s.tenants) >= s.cfg.MaxTenants {
			s.Unlock()
			s.Errorf("MaxTenants (%s) reached, new tenant (%s) dropped", s.cfg.MaxTenants, tenant)
			return
		}

		s.Warnf("Creating tenant '%s'", tenant)
		t = newTenant(tenant, s.cfg)

		s.tenants[tenant] = t
		s.Unlock()
	}

	t.push(ts)
}

func (s *httpServer) close() (err error) {
	close(s.chClose)
	s.wg.Wait()

	var errs *me.Error
	if err = s.srv.Shutdown(); err != nil {
		me.Append(errs, err)
	}

	s.RLock()
	for _, t := range s.tenants {
		if err = t.close(); err != nil {
			me.Append(errs, err)
		}
	}
	s.RUnlock()

	return errs.ErrorOrNil()
}

func (s *httpServer) recycler() {
	ticker := time.NewTicker(10 * time.Second)

	defer func() {
		ticker.Stop()
		s.wg.Done()
	}()

	for {
		select {
		case <-ticker.C:
			now := time.Now()

			toClose := []*tenant{}
			s.Lock()
			for tn, t := range s.tenants {
				if now.Sub(t.lastFlush.Load().(time.Time)) >= s.cfg.Tenant.RecycleAge {
					toClose = append(toClose, t)
					delete(s.tenants, tn)
				}
			}
			s.Unlock()

			for _, t := range toClose {
				s.Warnf("Recycling tenant '%s'", t.name)

				if err := t.close(); err != nil {
					s.Errorf("Errors while closing tenant '%s': %s", t.name, err)
				}
			}

		case <-s.chClose:
			return
		}
	}
}
