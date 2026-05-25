package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"mcstatus-exporter/internal/config"
	"mcstatus-exporter/internal/ping"
	"mcstatus-exporter/internal/queue"
)

func main() {
	log.Info("mcstatus-exporter")

	cfgFile := config.GetEnv("CONFIG_FILE", "servers.yaml")
	store := config.NewStore()
	if err := store.Load(cfgFile); err != nil {
		log.Fatalf("error loading config: %v", err)
	}
	store.Watch(cfgFile)

	prometheus.MustRegister(ping.Gauge)

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		results := ping.QueryAll(store.Snapshot())
		ping.ApplyToGauge(results)
		promhttp.Handler().ServeHTTP(w, r)
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if config.GetEnv("QUEUE_ENABLED", "false") == "true" {
		producer, err := buildProducer(store)
		if err != nil {
			log.Fatalf("queue producer disabled: %v", err)
		}
		go producer.Run(ctx)
		log.Info("queue producer started", "interval", producer.Interval, "source", producer.Source)
	}

	httpBindAddr := config.GetEnv("BIND", ":8080")
	log.Infof("listening on %s", httpBindAddr)

	server := &http.Server{Addr: httpBindAddr}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(err, "error starting HTTP server")
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown error: " + err.Error())
	}
}

func buildProducer(store *config.Store) (*queue.Producer, error) {
	source := os.Getenv("QUEUE_SOURCE")
	token := os.Getenv("CLOUDFLARE_API_TOKEN")
	accountID := os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	queueID := os.Getenv("CLOUDFLARE_QUEUE_ID")

	missing := []string{}
	if source == "" {
		missing = append(missing, "QUEUE_SOURCE")
	}
	if token == "" {
		missing = append(missing, "CLOUDFLARE_API_TOKEN")
	}
	if accountID == "" {
		missing = append(missing, "CLOUDFLARE_ACCOUNT_ID")
	}
	if queueID == "" {
		missing = append(missing, "CLOUDFLARE_QUEUE_ID")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %v", missing)
	}

	interval, err := time.ParseDuration(config.GetEnv("PING_INTERVAL", "30s"))
	if err != nil {
		return nil, fmt.Errorf("PING_INTERVAL: %w", err)
	}

	drainPerTick := 5
	if v := os.Getenv("SPOOL_DRAIN_PER_TICK"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("SPOOL_DRAIN_PER_TICK: must be positive integer")
		}
		drainPerTick = n
	}

	spool, err := queue.NewSpool(config.GetEnv("SPOOL_DIR", "./spool"))
	if err != nil {
		return nil, err
	}

	return &queue.Producer{
		Source:       source,
		Interval:     interval,
		DrainPerTick: drainPerTick,
		Store:        store,
		Client:       queue.NewClient(accountID, queueID, token),
		Spool:        spool,
	}, nil
}
