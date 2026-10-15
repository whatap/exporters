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

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

const (
	defaultListenAddress = "127.0.0.1:9107"
	readHeaderTimeout    = 5 * time.Second
	writeTimeout         = 15 * time.Second
	idleTimeout          = 60 * time.Second
	shutdownTimeout      = 5 * time.Second
)

// Injected by build.sh with -ldflags "-X main.version=... -X main.releaseDate=...".
var (
	version     = "dev"
	releaseDate = "unknown"
)

type options struct {
	listenAddress string
	region        string
	showVersion   bool
}

func versionLine() string {
	return fmt.Sprintf("ec2_state_exporter version %s (released %s)", version, releaseDate)
}

func parseOptions(args []string) (options, error) {
	var options options
	flags := flag.NewFlagSet("ec2_state_exporter", flag.ContinueOnError)
	flags.StringVar(&options.listenAddress, "web.listen-address", defaultListenAddress, "HTTP listen address (loopback by default)")
	flags.StringVar(&options.region, "region", "", "AWS region; default: AWS SDK environment/profile, then EC2 IMDS")
	flags.BoolVar(&options.showVersion, "version", false, "Print version and exit")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, errors.New("positional arguments are not supported")
	}
	return options, nil
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
	loadContext, cancel := context.WithTimeout(ctx, scrapeTimeout)
	loadOptions := []func(*config.LoadOptions) error{config.WithEC2IMDSRegion(), config.WithRetryMaxAttempts(1)}
	if options.region != "" {
		loadOptions = append(loadOptions, config.WithRegion(options.region))
	}
	awsConfig, err := config.LoadDefaultConfig(loadContext, loadOptions...)
	cancel()
	if err != nil {
		log.Fatal("EC2_STATE_EXPORTER: AWS configuration could not be resolved")
	}
	if awsConfig.Region == "" {
		log.Fatal("EC2_STATE_EXPORTER: AWS region is unavailable; configure AWS_REGION or --region")
	}
	server := &http.Server{
		Addr: options.listenAddress, Handler: &exporter{client: ec2.NewFromConfig(awsConfig), region: awsConfig.Region},
		ReadHeaderTimeout: readHeaderTimeout, WriteTimeout: writeTimeout, IdleTimeout: idleTimeout,
	}
	log.Print(fmt.Sprintf("EC2_STATE_EXPORTER: listening on %s, region=%s, version=%s", options.listenAddress, awsConfig.Region, version))
	if err := runHTTPServer(ctx, server, server.ListenAndServe, shutdownTimeout); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("EC2_STATE_EXPORTER: HTTP listener failed")
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
		// Serve returns as soon as Shutdown closes the listener. Keep the
		// caller here until draining (or the forced Close) has finished.
		shutdownContext, cancel := context.WithTimeout(context.Background(), gracePeriod)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			_ = server.Close()
		}
		return <-serveDone
	}
}
