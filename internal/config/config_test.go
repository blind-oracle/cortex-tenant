package config_test

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/projectcapsule/cortex-tenant/internal/config"
)

var _ = Describe("Config Loading", func() {
	BeforeEach(func() {
		// Ensure no interfering env vars.
		os.Unsetenv("CT_LISTEN")
		os.Unsetenv("CT_TENANT_HEADER")
		os.Unsetenv("CT_CONCURRENCY")
		// Unset other env variables as needed...
	})

	It("should load and override values from a valid YAML file", func() {
		// Create a temporary YAML config file.
		yamlContent := `
backend:
  url: "http://backend.example.com"
tenant:
  labels: ["mytenant"]
  prefix: "pfx-"
  prefixPreferSource: true
  header: "X-Tenant-Header"
  default: "defaulttenant"
  acceptAll: true
timeout: 7s
timeoutShutdown: 2s
maxConnectionDuration: 10s
maxConnectionsPerHost: 128
`
		tmpFile, err := os.CreateTemp("", "config-test-*.yaml")
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(yamlContent))
		Expect(err).NotTo(HaveOccurred())
		tmpFile.Close()

		cfg, err := config.Load(tmpFile.Name())
		Expect(err).NotTo(HaveOccurred())

		// Verify values from YAML.
		Expect(cfg.Backend).NotTo(BeNil())
		Expect(cfg.Backend.URL).To(Equal("http://backend.example.com"))
		Expect(cfg.Tenant).NotTo(BeNil())
		Expect(cfg.Tenant.Labels).To(Equal([]string{"mytenant"}))
		Expect(cfg.Tenant.Prefix).To(Equal("pfx-"))
		Expect(cfg.Tenant.PrefixPreferSource).To(BeTrue())
		Expect(cfg.Tenant.Header).To(Equal("X-Tenant-Header"))
		Expect(cfg.Tenant.Default).To(Equal("defaulttenant"))
		Expect(cfg.Tenant.AcceptAll).To(BeTrue())

		// Verify duration values.
		Expect(cfg.Timeout).To(Equal(7 * time.Second))
		Expect(cfg.TimeoutShutdown).To(Equal(2 * time.Second))
		Expect(cfg.MaxConnsPerHost).To(Equal(128))
	})

	It("should return an error for invalid YAML", func() {
		tmpFile, err := os.CreateTemp("", "config-invalid-*.yaml")
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(tmpFile.Name())

		// Write invalid YAML.
		_, err = tmpFile.Write([]byte("invalid: : yaml:::"))
		Expect(err).NotTo(HaveOccurred())
		tmpFile.Close()

		_, err = config.Load(tmpFile.Name())
		Expect(err).To(HaveOccurred())
	})
})
