package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/omics"
)

const (
	defaultListenAddress = "127.0.0.1:9109"
	readHeaderTimeout    = 5 * time.Second
	writeTimeout         = 15 * time.Second
	idleTimeout          = 60 * time.Second
	shutdownTimeout      = 5 * time.Second
	awsConfigTimeout     = 10 * time.Second
)

// Injected by build.sh with -ldflags "-X main.version=... -X main.releaseDate=...".
var (
	version     = "dev"
	releaseDate = "unknown"
)

type options struct {
	cfg             config
	listenAddress   string
	analyzerEnabled bool
	analyzerCommand string
	analyzerTimeout time.Duration
	analyzerWorkDir string
	showVersion     bool
}

func versionLine() string {
	return fmt.Sprintf("healthomics_exporter version %s (released %s)", version, releaseDate)
}

func parseOptions(args []string) (options, error) {
	options := options{cfg: defaultConfig()}
	flags := flag.NewFlagSet("healthomics_exporter", flag.ContinueOnError)
	flags.StringVar(&options.listenAddress, "web.listen-address", defaultListenAddress, "HTTP listen address (loopback by default)")
	flags.StringVar(&options.cfg.region, "region", "", "AWS region; default: AWS SDK environment/profile, then EC2 IMDS")
	flags.DurationVar(&options.cfg.interval, "interval", options.cfg.interval, "HealthOmics API polling interval")
	flags.DurationVar(&options.cfg.retention, "retention", options.cfg.retention, "How long finished runs stay exported after they stop")
	flags.BoolVar(&options.analyzerEnabled, "analyzer.enabled", true, "Run AWS Run Analyzer on finished runs")
	flags.StringVar(&options.analyzerCommand, "analyzer.command", "aws-healthomics-tools", "aws-healthomics-tools executable (pip package aws-healthomics-tools)")
	flags.DurationVar(&options.analyzerTimeout, "analyzer.timeout", 10*time.Minute, "Timeout of one Run Analyzer execution")
	flags.StringVar(&options.analyzerWorkDir, "analyzer.work-dir", os.TempDir(), "Directory for temporary Run Analyzer CSV files")
	flags.BoolVar(&options.showVersion, "version", false, "Print version and exit")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	switch {
	case flags.NArg() != 0:
		return options, errors.New("positional arguments are not supported")
	case options.cfg.interval <= 0:
		return options, errors.New("--interval must be positive")
	case options.cfg.retention <= 0:
		return options, errors.New("--retention must be positive")
	case options.analyzerTimeout <= 0:
		return options, errors.New("--analyzer.timeout must be positive")
	}
	options.cfg.maxStaleness = 3 * options.cfg.interval
	return options, nil
}

// run polls immediately and then every interval. Run Analyzer works in its own goroutine so
// a slow analysis never delays status polling.
func (e *exporter) run(ctx context.Context) {
	analyze := make(chan struct{}, 1)
	analyzerDone := make(chan struct{})
	go func() {
		defer close(analyzerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-analyze:
				e.analyzePending(ctx)
			}
		}
	}()
	ticker := time.NewTicker(e.cfg.interval)
	defer ticker.Stop()
	for {
		e.pollOnce(ctx)
		select {
		case analyze <- struct{}{}:
		default: // an analysis pass is already queued
		}
		select {
		case <-ctx.Done():
			<-analyzerDone
			return
		case <-ticker.C:
		}
	}
}

func main() {
	options, err := parseOptions(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	if options.showVersion {
		fmt.Println(versionLine())
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	loadContext, cancel := context.WithTimeout(ctx, awsConfigTimeout)
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithEC2IMDSRegion()}
	if options.cfg.region != "" {
		loadOptions = append(loadOptions, awsconfig.WithRegion(options.cfg.region))
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(loadContext, loadOptions...)
	cancel()
	if err != nil {
		log.Fatal("HEALTHOMICS_EXPORTER: AWS configuration could not be resolved")
	}
	if awsConfig.Region == "" {
		log.Fatal("HEALTHOMICS_EXPORTER: AWS region is unavailable; configure AWS_REGION or --region")
	}
	options.cfg.region = awsConfig.Region
	var analyzer runAnalyzer
	if options.analyzerEnabled {
		command, err := exec.LookPath(options.analyzerCommand)
		if err != nil {
			log.Fatalf("HEALTHOMICS_EXPORTER: %s not found; install it with `pip install aws-healthomics-tools` or pass --analyzer.enabled=false", options.analyzerCommand)
		}
		analyzer = &cliAnalyzer{command: command, region: awsConfig.Region, timeout: options.analyzerTimeout, workDir: options.analyzerWorkDir}
	}
	e := newExporter(options.cfg, omics.NewFromConfig(awsConfig), analyzer, time.Now)
	go e.run(ctx)
	server := &http.Server{
		Addr: options.listenAddress, Handler: e,
		ReadHeaderTimeout: readHeaderTimeout, WriteTimeout: writeTimeout, IdleTimeout: idleTimeout,
	}
	log.Printf("HEALTHOMICS_EXPORTER: listening on %s, region=%s, interval=%s, retention=%s, analyzer=%t, version=%s",
		options.listenAddress, awsConfig.Region, options.cfg.interval, options.cfg.retention, analyzer != nil, version)
	if err := runHTTPServer(ctx, server, server.ListenAndServe, shutdownTimeout); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("HEALTHOMICS_EXPORTER: HTTP listener failed")
	}
}

func runHTTPServer(ctx context.Context, server *http.Server, serve func() error, gracePeriod time.Duration) error {
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serve()
	}()
	select {
	case err := <-serveDone:
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), gracePeriod)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			_ = server.Close()
		}
		return <-serveDone
	}
}
