package main

import (
	"flag"
	"io/ioutil"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const (
	bufSize = 1024 * 1024
)

type config struct {
	Listen string
	Target string

	LogLevel string `yaml:"log_level"`

	MaxTenants int `yaml:"max_tenants"`
	BufferSize int `yaml:"buffer_size"`
	BatchSize  int `yaml:"batch_size"`

	FlushInterval time.Duration `yaml:"flush_interval"`
	Timeout       time.Duration

	Tenant struct {
		Label       string
		LabelRemove bool `yaml:"label_remove"`
		Header      string
		Default     string
		RecycleAge  time.Duration `yaml:"recycle_age"`
	}
}

type buffer struct {
	b []byte
}

func (b *buffer) grow() {
	b.b = b.b[:bufSize]
}

var (
	version = "0.0.0"

	bufferPool = sync.Pool{
		New: func() interface{} {
			return &buffer{
				b: make([]byte, 0, bufSize),
			}
		},
	}
)

func main() {
	cfgFile := flag.String("config", "", "Path to a config file")
	flag.Parse()

	if *cfgFile == "" {
		log.Fatalf("Config file required")
	}

	y, err := ioutil.ReadFile(*cfgFile)
	if err != nil {
		log.Fatalf("Unable to read config: %s", err)
	}

	var cfg config
	if err = yaml.UnmarshalStrict(y, &cfg); err != nil {
		log.Fatalf("Unable to parse config: %s", err)
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}

	if cfg.Tenant.Default == "" {
		cfg.Tenant.Default = "default"
	}

	if cfg.Tenant.Header == "" {
		cfg.Tenant.Header = "X-Scope-OrgID"
	}

	if cfg.Tenant.Label == "" {
		cfg.Tenant.Label = "__tenant__"
	}

	if cfg.Tenant.RecycleAge == 0 {
		cfg.Tenant.RecycleAge = 10 * time.Minute
	}

	if cfg.LogLevel != "" {
		lvl, err := log.ParseLevel(cfg.LogLevel)
		if err != nil {
			log.Fatalf("Unable to parse log level: %s", err)
		}
		log.SetLevel(lvl)
	}

	proc, err := newProcessor(cfg)
	if err != nil {
		log.Fatalf("Unable to start: %s", err)
	}

	log.Warnf("Started v%s", version)

	sigchannel := make(chan os.Signal, 1)
	signal.Notify(sigchannel, syscall.SIGTERM, os.Interrupt)

	for sig := range sigchannel {
		switch sig {
		case os.Interrupt, syscall.SIGTERM:
			log.Warn("Got SIGTERM, shutting down")
			if err = proc.close(); err != nil {
				log.Errorf("Error during shutdown: %s", err)
			}

			log.Warnf("Finished")
			return
		}
	}
}
