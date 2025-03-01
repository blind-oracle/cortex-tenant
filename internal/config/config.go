package config

import (
	"os"
	"time"

	"github.com/caarlos0/env/v8"
	"github.com/pkg/errors"
	fhu "github.com/valyala/fasthttp/fasthttputil"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Backend *CortexBackend `yaml:"backend"`

	EnableIPv6 bool `yaml:"ipv6"`

	Timeout         time.Duration `yaml:"timeout"`
	TimeoutShutdown time.Duration `yaml:"timeoutShutdown"`
	Concurrency     int           `yaml:"concurrency"`
	Metadata        bool          `yaml:"metadata"`
	MaxConnDuration time.Duration `yaml:"maxConnectionDuration"`
	MaxConnsPerHost int           `yaml:"maxConnectionsPerHost"`

	Tenant *TenantConfig `yaml:"tenant"`

	PipeIn  *fhu.InmemoryListener
	PipeOut *fhu.InmemoryListener
}

type CortexBackend struct {
	URL  string `yaml:"url"`
	Auth struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"auth"`
}

type TenantConfig struct {
	Labels             []string `yaml:"labels"`
	Prefix             string   `yaml:"prefix"`
	PrefixPreferSource bool     `yaml:"prefixPreferSource"`
	LabelRemove        bool     `yaml:"labelRemove"`
	Header             string   `yaml:"header"`
	Default            string   `yaml:"default"`
	AcceptAll          bool     `yaml:"acceptAll"`
}

func Load(file string) (*Config, error) {
	cfg := &Config{}

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

	if cfg.Concurrency == 0 {
		cfg.Concurrency = 512
	}

	if cfg.Tenant.Header == "" {
		cfg.Tenant.Header = "X-Scope-OrgID"
	}

	// Default to the Label if list is empty
	if len(cfg.Tenant.Labels) == 0 {
		cfg.Tenant.Labels = append(cfg.Tenant.Labels, "__tenant__")
	}

	if cfg.MaxConnsPerHost == 0 {
		cfg.MaxConnsPerHost = 64
	}

	return cfg, nil
}
