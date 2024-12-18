package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"net/http"
	_ "net/http/pprof"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

var (
	Version string
)

func main() {
	cfgFile := flag.String("config", "", "Path to a config file")
	flag.Parse()

	cfg, err := configLoad(*cfgFile)
	if err != nil {
		log.Fatal(err)
	}

	if cfg.ListenPprof != "" {
		go func() {
			if err := http.ListenAndServe(cfg.ListenPprof, nil); err != nil {
				log.Fatalf("Unable to listen on %s: %s", cfg.ListenPprof, err)
			}
		}()
	}

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(cfg.ListenMetricsAddress, nil); err != nil {
			log.Fatalf("Unable to listen on %s: %s", cfg.ListenMetricsAddress, err)
		}
	}()

	lvl, err := log.ParseLevel(cfg.LogLevel)
	if err != nil {
		log.Fatalf("Unable to parse log level: %s", err)
	}

	log.SetLevel(lvl)

	cfgJSON, _ := json.Marshal(cfg)
	log.Warnf("Effective config: %+v", string(cfgJSON))

	proc := newProcessor(*cfg)

	if err = proc.run(); err != nil {
		log.Fatalf("Unable to start: %s", err)
	}

	log.Warnf("Listening on %s, sending to %s", cfg.Listen, cfg.Target)
	log.Warnf("Started v%s", Version)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, os.Interrupt)
	<-ch

	log.Warn("Shutting down, draining requests")
	if err = proc.close(); err != nil {
		log.Errorf("Error during shutdown: %s", err)
	}

	log.Warnf("Finished")
}
