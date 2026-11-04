package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

const (
	defaultListenAddress = "127.0.0.1:9108"
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
	configFile    string
	listenAddress string
	region        string
	showVersion   bool
}

func versionLine() string {
	return fmt.Sprintf("cloudwatch_promql_exporter version %s (released %s)", version, releaseDate)
}

func parseOptions(args []string) (options, error) {
	var options options
	flags := flag.NewFlagSet("cloudwatch_promql_exporter", flag.ContinueOnError)
	flags.StringVar(&options.configFile, "config", "", "Path to the YAML query configuration (required)")
	flags.StringVar(&options.listenAddress, "web.listen-address", defaultListenAddress, "HTTP listen address (loopback by default)")
	flags.StringVar(&options.region, "region", "", "AWS region; default: AWS SDK environment/profile, then EC2 IMDS")
	flags.BoolVar(&options.showVersion, "version", false, "Print version and exit")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, errors.New("positional arguments are not supported")
	}
	if options.configFile == "" && !options.showVersion {
		return options, errors.New("--config is required")
	}
	return options, nil
}

func defaultEndpoint(region string) string {
	return "https://monitoring." + region + ".amazonaws.com"
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
	raw, err := os.ReadFile(options.configFile)
	if err != nil {
		log.Fatalf("CLOUDWATCH_PROMQL_EXPORTER: cannot read config: %v", err)
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		log.Fatalf("CLOUDWATCH_PROMQL_EXPORTER: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	loadContext, cancel := context.WithTimeout(ctx, awsConfigTimeout)
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithEC2IMDSRegion()}
	if options.region != "" {
		loadOptions = append(loadOptions, awsconfig.WithRegion(options.region))
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(loadContext, loadOptions...)
	cancel()
	if err != nil {
		log.Fatal("CLOUDWATCH_PROMQL_EXPORTER: AWS configuration could not be resolved")
	}
	if awsConfig.Region == "" {
		log.Fatal("CLOUDWATCH_PROMQL_EXPORTER: AWS region is unavailable; configure AWS_REGION or --region")
	}
	e := newExporter(cfg, &promQLClient{
		endpoint: defaultEndpoint(awsConfig.Region), region: awsConfig.Region, credentials: awsConfig.Credentials,
		httpClient: &http.Client{Timeout: cfg.Timeout}, now: time.Now,
	})
	go e.run(ctx)
	server := &http.Server{
		Addr: options.listenAddress, Handler: e,
		ReadHeaderTimeout: readHeaderTimeout, WriteTimeout: writeTimeout, IdleTimeout: idleTimeout,
	}
	log.Printf("CLOUDWATCH_PROMQL_EXPORTER: listening on %s, region=%s, queries=%d, interval=%s, version=%s",
		options.listenAddress, awsConfig.Region, len(cfg.Queries), cfg.Interval, version)
	if err := runHTTPServer(ctx, server, server.ListenAndServe, shutdownTimeout); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("CLOUDWATCH_PROMQL_EXPORTER: HTTP listener failed")
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
