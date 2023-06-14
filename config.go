package main

import (
	"fmt"
	"os"
	"time"

	"github.com/caarlos0/env/v8"
	"github.com/pkg/errors"
	fhu "github.com/valyala/fasthttp/fasthttputil"
	"gopkg.in/yaml.v2"
)

type config struct {
	Listen                     string `env:"CT_LISTEN"`
	ListenPprof                string `yaml:"listen_pprof" env:"CT_LISTEN_PPROF"`
	ListenMetricsAddress       string `yaml:"listen_metrics_address" env: "CT_LISTEN_METRICS_ADDRESS"`
	ListenMetricsIncludeTenant bool   `yaml:"listen_metrics_include_tenant" env: "CT_LISTEN_METRICS_INCLUDE_TENANT"`

	Target     string `env:"CT_TARGET"`
	EnableIPv6 bool   `yaml:"enable_ipv6" env:"CT_ENABLE_IPV6"`

	LogLevel          string        `yaml:"log_level" env:"CT_LOG_LEVEL"`
	Timeout           time.Duration `env:"CT_TIMEOUT"`
	TimeoutShutdown   time.Duration `yaml:"timeout_shutdown" env:"CT_TIMEOUT_SHUTDOWN"`
	Concurrency       int           `env:"CT_CONCURRENCY"`
	Metadata          bool          `env:"CT_METADATA"`
	LogResponseErrors bool          `yaml:"log_response_errors" env:"CT_LOG_RESPONSE_ERRORS"`
	MaxConnDuration   time.Duration `yaml:"max_connection_duration" env:"CT_MAX_CONN_DURATION"`

	Auth struct {
		Egress struct {
			Username string `env:"CT_AUTH_EGRESS_USERNAME"`
			Password string `env:"CT_AUTH_EGRESS_PASSWORD"`
		}
	}

	Tenant struct {
		Label       string `env:"CT_TENANT_LABEL"`
		Prefix      string `yaml:"prefix" env:"CT_TENANT_PREFIX"`
		LabelRemove bool   `yaml:"label_remove" env:"CT_TENANT_LABEL_REMOVE"`
		Header      string `env:"CT_TENANT_HEADER"`
		Default     string `env:"CT_TENANT_DEFAULT"`
		AcceptAll   bool   `yaml:"accept_all" env:"CT_TENANT_ACCEPT_ALL"`
	}

	pipeIn  *fhu.InmemoryListener
	pipeOut *fhu.InmemoryListener
}

func configLoad(file string) (*config, error) {
	cfg := &config{}

	if file != "" {
		y, err := os.ReadFile(file)
		if err != nil {
			return nil, errors.Wrap(err, "Unable to read config")
		}

		if err := yaml.UnmarshalStrict(y, cfg); err != nil {
			return nil, errors.Wrap(err, "Unable to parse config")
		}
	}

	if err := env.Parse(cfg); err != nil {
		return nil, errors.Wrap(err, "Unable to parse env vars")
	}

	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8081"
	}

	if cfg.ListenMetricsAddress == "" {
		cfg.ListenMetricsAddress = "0.0.0.0:9090"
	}

	if cfg.LogLevel == "" {
		cfg.LogLevel = "warn"
	}

	if cfg.Target == "" {
		cfg.Target = "127.0.0.1:9090"
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}

	if cfg.Concurrency == 0 {
		cfg.Concurrency = 512
	}

	if cfg.Tenant.Header == "" {
		cfg.Tenant.Header = "X-Scope-OrgID"
	}

	if cfg.Tenant.Label == "" {
		cfg.Tenant.Label = "__tenant__"
	}

	if cfg.Auth.Egress.Username != "" {
		if cfg.Auth.Egress.Password == "" {
			return nil, fmt.Errorf("egress auth user specified, but the password is not")
		}
	}

	return cfg, nil
}
