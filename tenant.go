package main

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/blind-oracle/go-common/batcher"
	"github.com/blind-oracle/go-common/logger"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
	fh "github.com/valyala/fasthttp"
)

type tenant struct {
	name string
	cfg  config

	cli     *fh.Client
	batcher *batcher.Batcher

	lastFlush atomic.Value
}

func newTenant(name string, c config) *tenant {
	t := &tenant{
		name: name,
		cfg:  c,
	}

	t.cli = &fh.Client{
		Name:               "cortex-tenant",
		ReadTimeout:        c.Timeout,
		WriteTimeout:       c.Timeout,
		MaxConnWaitTimeout: 1 * time.Second,
		MaxConnsPerHost:    64,
	}

	bc := batcher.Config{
		Flush:         t.flush,
		BufferSize:    c.BufferSize,
		BatchSize:     c.BatchSize,
		FlushInterval: c.FlushInterval,
		Logger:        logger.NewSimpleLogger(name),
	}

	t.batcher, _ = batcher.New(bc)
	return t
}

func (t *tenant) push(ts *prompb.TimeSeries) {
	t.batcher.Queue(ts)
}

func (t *tenant) flush(batch []interface{}) (err error) {
	t.lastFlush.Store(time.Now())

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

	var rq prompb.WriteRequest
	for _, o := range batch {
		ts := o.(*prompb.TimeSeries)
		rq.Timeseries = append(rq.Timeseries, ts)
	}

	var l int
	buf1.grow()
	if l, err = rq.MarshalTo(buf1.b); err != nil {
		return
	}

	buf2.grow()
	if buf2.b = snappy.Encode(buf2.b, buf1.b[:l]); err != nil {
		return
	}

	req.Header.SetMethod("POST")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set(t.cfg.Tenant.Header, t.name)

	req.SetRequestURI(t.cfg.Target)
	req.SetBody(buf2.b)

	if err = t.cli.Do(req, resp); err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}

	if resp.Header.StatusCode() != 200 {
		return fmt.Errorf("HTTP code is not 200: %d (%s)", resp.Header.StatusCode(), string(resp.Body()))
	}

	return
}

func (t *tenant) close() error {
	return t.batcher.Close()
}
