package main

import (
	"flag"
	"io/ioutil"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"net/http"
	_ "net/http/pprof"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const (
	bufSize = 1024 * 1024
)

type config struct {
	Listen      string
	ListenPprof string `yaml:"listen_pprof"`

	Target string

	LogLevel        string `yaml:"log_level"`
	Timeout         time.Duration
	TimeoutShutdown time.Duration `yaml:"timeout_shutdown"`

	Tenant struct {
		Label       string
		LabelRemove bool `yaml:"label_remove"`
		Header      string
		Default     string
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

	if cfg.ListenPprof != "" {
		go func() {
			if err := http.ListenAndServe(cfg.ListenPprof, nil); err != nil {
				log.Fatalf("Unable to listen on %s: %s", cfg.ListenPprof, err)
			}
		}()
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}

	if cfg.TimeoutShutdown == 0 {
		cfg.TimeoutShutdown = 10 * time.Second
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

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, os.Interrupt)
	<-ch

	log.Warn("Shutting down, draining requests")
	if err = proc.close(); err != nil {
		log.Errorf("Error during shutdown: %s", err)
	}

	log.Warnf("Finished")
}
