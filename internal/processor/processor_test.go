package processor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/prometheus/prometheus/prompb"
	fh "github.com/valyala/fasthttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/cortex-tenant/internal/config"
	"github.com/projectcapsule/cortex-tenant/internal/metrics"
	"github.com/projectcapsule/cortex-tenant/internal/stores"
)

var _ = Describe("Processor Forwarding", func() {
	var (
		proc           *Processor
		fakeTarget     *httptest.Server
		receivedMu     sync.Mutex
		receivedHeader http.Header
		ctx            context.Context
		cancel         context.CancelFunc
		cfg            config.Config
		store          *stores.TenantStore
		metric         *metrics.Recorder
	)

	metric = metrics.MustMakeRecorder() // or a mock recorder

	BeforeEach(func() {
		// Create a fake target server that records request headers.
		fakeTarget = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedMu.Lock()
			receivedHeader = r.Header.Clone()
			receivedMu.Unlock()
			w.Header().Set("Connection", "close")
			w.WriteHeader(http.StatusOK)
		}))

		// Initialize configuration for the processor.
		// Ensure cfg.Target points to fakeTarget.URL.
		cfg = config.Config{
			Backend: &config.CortexBackend{
				URL: fakeTarget.URL,
			},
			Timeout: 5 * time.Second,
			// Set other fields as needed, for example Tenant config.
			Tenant: &config.TenantConfig{
				Labels: []string{
					"namespace",
					"target_namespace",
				},
				Header:             "X-Scope-OrgID",
				Default:            "default",
				Prefix:             "test-",
				PrefixPreferSource: false,
			},
		}

		// Initialize any required dependencies (store, metrics, logger).
		store = stores.NewTenantStore() // or a suitable mock
		store.Update(&capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "solar",
				Namespace: "solar",
			},
			Status: capsulev1beta2.TenantStatus{
				Namespaces: []string{"solar-one", "solar-two", "solar-three"},
			},
		})
		store.Update(&capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oil",
				Namespace: "oil",
			},
			Status: capsulev1beta2.TenantStatus{
				Namespaces: []string{"oil-one", "oil-two", "oil-three"},
			},
		})

		// Create the processor.
		// Start the processor webserver in a separate goroutine.
		ctx, cancel = context.WithCancel(context.Background())

		log, _ := logr.FromContext(ctx)

		// Create the processor.
		proc = NewProcessor(log, cfg, store, metric)

		go func() {
			if err := proc.Start(ctx); err != nil {
				log.Error(err, "processor failed")
			}
		}()

		// Allow some time for the processor to start.
		time.Sleep(500 * time.Millisecond)
	})

	AfterEach(func() {
		cancel()
		fakeTarget.Close()
	})

	It("should correctly set headers", func() {
		By("settings default tenant", func() {

			// Prepare a minimal prompb.WriteRequest.
			wr := &prompb.WriteRequest{
				Timeseries: []prompb.TimeSeries{
					{
						Labels: []prompb.Label{
							{Name: "job", Value: "test"},
							{Name: "instance", Value: "localhost:9090"},
						},
						Samples: []prompb.Sample{
							{Value: 123, Timestamp: time.Now().UnixMilli()},
						},
					},
				},
			}

			// Marshal and compress using the processor helper.
			buf, err := proc.marshal(wr)
			Expect(err).NotTo(HaveOccurred())

			// Build a POST request to the processor's /push endpoint.
			// Since processor uses fasthttp, use its client for the test.
			var req fh.Request
			var resp fh.Response
			req.SetRequestURI("http://127.0.0.1:8080/push")
			req.Header.SetMethod(fh.MethodPost)
			req.Header.Set("Content-Encoding", "snappy")
			req.Header.Set("Content-Type", "application/x-protobuf")
			req.SetBody(buf)

			// Send the request using fasthttp.
			err = fh.DoTimeout(&req, &resp, cfg.Timeout)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(fh.StatusOK))

			// Wait until the fake target receives the forwarded request.
			Eventually(func() http.Header {
				receivedMu.Lock()
				defer receivedMu.Unlock()
				return receivedHeader
			}, 5*time.Second, 200*time.Millisecond).ShouldNot(BeEmpty())

			// Verify that the forwarded request contains the expected header.
			receivedMu.Lock()
			defer receivedMu.Unlock()
			Expect(receivedHeader).To(HaveKeyWithValue(http.CanonicalHeaderKey("X-Scope-OrgID"), []string{"test-default"}))
			Expect(receivedHeader).To(HaveKeyWithValue(http.CanonicalHeaderKey("X-Prometheus-Remote-Write-Version"), []string{"0.1.0"}))
			Expect(receivedHeader).To(HaveKeyWithValue(http.CanonicalHeaderKey("Content-Encoding"), []string{"snappy"}))
		})

		By("proxy correct tenant (solar)", func() {

			// Prepare a minimal prompb.WriteRequest.
			wr := &prompb.WriteRequest{
				Timeseries: []prompb.TimeSeries{
					{
						Labels: []prompb.Label{
							{Name: "job", Value: "test"},
							{Name: "instance", Value: "localhost:9090"},
							{Name: "namespace", Value: "solar-three"},
						},
						Samples: []prompb.Sample{
							{Value: 123, Timestamp: time.Now().UnixMilli()},
						},
					},
				},
			}

			// Marshal and compress using the processor helper.
			buf, err := proc.marshal(wr)
			Expect(err).NotTo(HaveOccurred())

			// Build a POST request to the processor's /push endpoint.
			// Since processor uses fasthttp, use its client for the test.
			var req fh.Request
			var resp fh.Response
			req.SetRequestURI("http://127.0.0.1:8080/push")
			req.Header.SetMethod(fh.MethodPost)
			req.Header.Set("Content-Encoding", "snappy")
			req.Header.Set("Content-Type", "application/x-protobuf")
			req.SetBody(buf)

			// Send the request using fasthttp.
			err = fh.DoTimeout(&req, &resp, cfg.Timeout)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(fh.StatusOK))

			// Wait until the fake target receives the forwarded request.
			Eventually(func() http.Header {
				receivedMu.Lock()
				defer receivedMu.Unlock()
				return receivedHeader
			}, 5*time.Second, 200*time.Millisecond).ShouldNot(BeEmpty())

			// Verify that the forwarded request contains the expected header.
			receivedMu.Lock()
			defer receivedMu.Unlock()
			Expect(receivedHeader).To(HaveKeyWithValue(http.CanonicalHeaderKey("X-Scope-OrgID"), []string{cfg.Tenant.Prefix + "solar"}))
			Expect(receivedHeader).To(HaveKeyWithValue(http.CanonicalHeaderKey("X-Prometheus-Remote-Write-Version"), []string{"0.1.0"}))
		})

		By("proxy correct tenant (oil)", func() {

			// Prepare a minimal prompb.WriteRequest.
			wr := &prompb.WriteRequest{
				Timeseries: []prompb.TimeSeries{
					{
						Labels: []prompb.Label{
							{Name: "job", Value: "test"},
							{Name: "instance", Value: "localhost:9090"},
							{Name: "target_namespace", Value: "oil-one"},
						},
						Samples: []prompb.Sample{
							{Value: 123, Timestamp: time.Now().UnixMilli()},
						},
					},
				},
			}

			// Marshal and compress using the processor helper.
			buf, err := proc.marshal(wr)
			Expect(err).NotTo(HaveOccurred())

			// Build a POST request to the processor's /push endpoint.
			// Since processor uses fasthttp, use its client for the test.
			var req fh.Request
			var resp fh.Response
			req.SetRequestURI("http://127.0.0.1:8080/push")
			req.Header.SetMethod(fh.MethodPost)
			req.Header.Set("Content-Encoding", "snappy")
			req.Header.Set("Content-Type", "application/x-protobuf")
			req.SetBody(buf)

			// Send the request using fasthttp.
			err = fh.DoTimeout(&req, &resp, cfg.Timeout)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(fh.StatusOK))

			// Wait until the fake target receives the forwarded request.
			Eventually(func() http.Header {
				receivedMu.Lock()
				defer receivedMu.Unlock()
				return receivedHeader
			}, 5*time.Second, 200*time.Millisecond).ShouldNot(BeEmpty())

			// Verify that the forwarded request contains the expected header.
			receivedMu.Lock()
			defer receivedMu.Unlock()
			Expect(receivedHeader).To(HaveKeyWithValue(http.CanonicalHeaderKey("X-Scope-OrgID"), []string{cfg.Tenant.Prefix + "oil"}))
			Expect(receivedHeader).To(HaveKeyWithValue(http.CanonicalHeaderKey("X-Prometheus-Remote-Write-Version"), []string{"0.1.0"}))
		})

		By("default on no match", func() {

			// Prepare a minimal prompb.WriteRequest.
			wr := &prompb.WriteRequest{
				Timeseries: []prompb.TimeSeries{
					{
						Labels: []prompb.Label{
							{Name: "job", Value: "test"},
							{Name: "instance", Value: "localhost:9090"},
							{Name: "target_namespace", Value: "oil-prod"},
						},
						Samples: []prompb.Sample{
							{Value: 123, Timestamp: time.Now().UnixMilli()},
						},
					},
				},
			}

			// Marshal and compress using the processor helper.
			buf, err := proc.marshal(wr)
			Expect(err).NotTo(HaveOccurred())

			// Build a POST request to the processor's /push endpoint.
			// Since processor uses fasthttp, use its client for the test.
			var req fh.Request
			var resp fh.Response
			req.SetRequestURI("http://127.0.0.1:8080/push")
			req.Header.SetMethod(fh.MethodPost)
			req.Header.Set("Content-Encoding", "snappy")
			req.Header.Set("Content-Type", "application/x-protobuf")
			req.SetBody(buf)

			// Send the request using fasthttp.
			err = fh.DoTimeout(&req, &resp, cfg.Timeout)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(fh.StatusOK))

			// Wait until the fake target receives the forwarded request.
			Eventually(func() http.Header {
				receivedMu.Lock()
				defer receivedMu.Unlock()
				return receivedHeader
			}, 5*time.Second, 200*time.Millisecond).ShouldNot(BeEmpty())

			// Verify that the forwarded request contains the expected header.
			receivedMu.Lock()
			defer receivedMu.Unlock()
			Expect(receivedHeader).To(HaveKeyWithValue(http.CanonicalHeaderKey("X-Scope-OrgID"), []string{cfg.Tenant.Prefix + cfg.Tenant.Default}))
			Expect(receivedHeader).To(HaveKeyWithValue(http.CanonicalHeaderKey("X-Prometheus-Remote-Write-Version"), []string{"0.1.0"}))
		})

	})
})
