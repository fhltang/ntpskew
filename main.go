// Command ntpskew runs a small background poller that queries one or more
// NTP servers on a fixed schedule, computes the local clock's offset (skew)
// relative to each server, and exposes the results as Prometheus metrics.
//
// Design notes:
//   - Polling runs on its own ticker, independent of Prometheus scrapes.
//     Scrapes just read the last-known gauge values. This avoids doing a
//     network round-trip inside the scrape path (which could time out or
//     hammer the NTP server if scraped frequently).
//   - Each configured server is polled concurrently and independently, so
//     one slow/unreachable server doesn't block metrics for the others.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/beevik/ntp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	clockOffsetSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ntp_clock_offset_seconds",
			Help: "Offset between the local clock and the NTP server's clock, in seconds. Positive means the local clock is ahead.",
		},
		[]string{"server"},
	)

	roundTripSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ntp_round_trip_seconds",
			Help: "Round-trip time of the most recent NTP query, in seconds.",
		},
		[]string{"server"},
	)

	stratum = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ntp_stratum",
			Help: "Stratum of the NTP server as reported in the most recent successful query.",
		},
		[]string{"server"},
	)

	rootDispersionSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ntp_root_dispersion_seconds",
			Help: "Root dispersion reported by the NTP server in the most recent successful query, in seconds.",
		},
		[]string{"server"},
	)

	lastQueryTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ntp_last_query_timestamp_seconds",
			Help: "Unix timestamp of the most recent query attempt (successful or not).",
		},
		[]string{"server"},
	)

	lastSuccessTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ntp_last_success_timestamp_seconds",
			Help: "Unix timestamp of the most recent successful query.",
		},
		[]string{"server"},
	)

	queriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ntp_queries_total",
			Help: "Total number of NTP queries attempted, labeled by outcome.",
		},
		[]string{"server", "outcome"}, // outcome: success|error
	)
)

func init() {
	prometheus.MustRegister(
		clockOffsetSeconds,
		roundTripSeconds,
		stratum,
		rootDispersionSeconds,
		lastQueryTimestamp,
		lastSuccessTimestamp,
		queriesTotal,
	)
}

// config holds runtime configuration, populated from flags/env vars.
type config struct {
	servers      []string
	pollInterval time.Duration
	queryTimeout time.Duration
	listenAddr   string
	metricsPath  string
}

func loadConfig() config {
	var (
		serversFlag  string
		pollInterval time.Duration
		queryTimeout time.Duration
		listenAddr   string
		metricsPath  string
	)

	flag.StringVar(&serversFlag, "ntp-servers", envOr("NTP_SERVERS", "pool.ntp.org"),
		"Comma-separated list of NTP servers to query (env: NTP_SERVERS)")
	flag.DurationVar(&pollInterval, "poll-interval", envDurationOr("NTP_POLL_INTERVAL", 30*time.Second),
		"How often to poll each NTP server (env: NTP_POLL_INTERVAL)")
	flag.DurationVar(&queryTimeout, "query-timeout", envDurationOr("NTP_QUERY_TIMEOUT", 5*time.Second),
		"Timeout for a single NTP query (env: NTP_QUERY_TIMEOUT)")
	flag.StringVar(&listenAddr, "listen-addr", envOr("LISTEN_ADDR", ":9917"),
		"Address for the metrics HTTP server to listen on (env: LISTEN_ADDR)")
	flag.StringVar(&metricsPath, "metrics-path", envOr("METRICS_PATH", "/metrics"),
		"HTTP path to serve Prometheus metrics on (env: METRICS_PATH)")
	flag.Parse()

	var servers []string
	for _, s := range strings.Split(serversFlag, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			servers = append(servers, s)
		}
	}

	return config{
		servers:      servers,
		pollInterval: pollInterval,
		queryTimeout: queryTimeout,
		listenAddr:   listenAddr,
		metricsPath:  metricsPath,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Printf("warning: could not parse %s=%q as a duration, using default %s", key, v, fallback)
	}
	return fallback
}

// pollServer queries a single NTP server once and updates its metrics.
func pollServer(ctx context.Context, server string, timeout time.Duration) {
	lastQueryTimestamp.WithLabelValues(server).Set(float64(time.Now().Unix()))

	opts := ntp.QueryOptions{Timeout: timeout}
	resp, err := ntp.QueryWithOptions(server, opts)
	if err != nil {
		queriesTotal.WithLabelValues(server, "error").Inc()
		log.Printf("ntp query failed for %s: %v", server, err)
		return
	}
	if err := resp.Validate(); err != nil {
		queriesTotal.WithLabelValues(server, "error").Inc()
		log.Printf("ntp response from %s failed validation: %v", server, err)
		return
	}

	clockOffsetSeconds.WithLabelValues(server).Set(resp.ClockOffset.Seconds())
	roundTripSeconds.WithLabelValues(server).Set(resp.RTT.Seconds())
	stratum.WithLabelValues(server).Set(float64(resp.Stratum))
	rootDispersionSeconds.WithLabelValues(server).Set(resp.RootDispersion.Seconds())
	lastSuccessTimestamp.WithLabelValues(server).Set(float64(time.Now().Unix()))
	queriesTotal.WithLabelValues(server, "success").Inc()

	log.Printf("server=%s offset=%s rtt=%s stratum=%d", server, resp.ClockOffset, resp.RTT, resp.Stratum)
}

// pollLoop runs an independent polling schedule for a single server until
// ctx is cancelled. Polls are jittered slightly on startup so that multiple
// servers configured together don't all fire in lockstep.
func pollLoop(ctx context.Context, server string, interval, timeout time.Duration) {
	// Do an immediate first query so metrics are populated right away.
	pollServer(ctx, server, timeout)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pollServer(ctx, server, timeout)
		}
	}
}

func main() {
	cfg := loadConfig()

	if len(cfg.servers) == 0 {
		log.Fatal("no NTP servers configured")
	}

	log.Printf("starting ntpskew: servers=%v poll_interval=%s query_timeout=%s listen_addr=%s metrics_path=%s",
		cfg.servers, cfg.pollInterval, cfg.queryTimeout, cfg.listenAddr, cfg.metricsPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, server := range cfg.servers {
		go pollLoop(ctx, server, cfg.pollInterval, cfg.queryTimeout)
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.metricsPath, promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())
}
