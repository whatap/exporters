package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/whatap/ncloud_exporter/collector"
	"github.com/whatap/ncloud_exporter/config"
	"github.com/whatap/ncloud_exporter/ncloud"
)

// Injected at build time via -ldflags. See build.sh.
var (
	version     = "dev"
	releaseDate = "unknown"
)

func main() {
	configPath := flag.String("config", "config.yml", "Path to config file")
	listenAddress := flag.String("web.listen-address", ":9850", "Address to listen on for web interface and telemetry")
	metricsPath := flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics")
	logLevel := flag.String("log.level", "info", "Log level: debug, info, warn, error")
	logFormat := flag.String("log.format", "text", "Log format: text, json")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("ncloud_exporter version %s (released %s)\n", version, releaseDate)
		os.Exit(0)
	}

	if err := setupLogger(*logLevel, *logFormat); err != nil {
		fmt.Fprintf(os.Stderr, "invalid log config: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	client := ncloud.NewClient(cfg.NCloud.AccessKey, cfg.NCloud.SecretKey)
	ciClient := ncloud.NewCloudInsightClient(client)
	rcClient := ncloud.NewResourceClient(client)

	coll := collector.NewNCloudCollector(ciClient, rcClient, cfg)

	if err := coll.InitCWKeys(); err != nil {
		slog.Error("failed to initialize cw_keys", "error", err)
		os.Exit(1)
	}

	prometheus.MustRegister(coll)

	// SIGHUP config reload
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
			slog.Info("received SIGHUP, reloading config")
			newCfg, err := config.Load(*configPath)
			if err != nil {
				slog.Error("config reload failed", "error", err)
				continue
			}

			ciClient := ncloud.NewCloudInsightClient(
				ncloud.NewClient(newCfg.NCloud.AccessKey, newCfg.NCloud.SecretKey),
			)
			rcClient := ncloud.NewResourceClient(
				ncloud.NewClient(newCfg.NCloud.AccessKey, newCfg.NCloud.SecretKey),
			)

			coll.ReloadConfig(newCfg, ciClient, rcClient)
			slog.Info("config reloaded successfully")
		}
	}()

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
<head><title>NCloud Exporter</title></head>
<body>
<h1>NCloud Prometheus Exporter</h1>
<p><a href="` + *metricsPath + `">Metrics</a></p>
</body>
</html>`))
	})

	slog.Info("starting ncloud_exporter",
		"version", version, "releaseDate", releaseDate,
		"address", *listenAddress, "path", *metricsPath)
	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		slog.Error("http server error", "error", err)
		os.Exit(1)
	}
}

func setupLogger(level, format string) error {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "info":
		lv = slog.LevelInfo
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		return fmt.Errorf("unknown log level: %q (use debug, info, warn, error)", level)
	}

	opts := &slog.HandlerOptions{Level: lv}

	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	default:
		return fmt.Errorf("unknown log format: %q (use text, json)", format)
	}

	slog.SetDefault(slog.New(handler))
	return nil
}
