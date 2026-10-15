package main

import "testing"

func TestOptionsDefaultToLoopbackAndAWSRegionDiscovery(t *testing.T) {
	options, err := parseOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if options.listenAddress != "127.0.0.1:9107" || options.region != "" {
		t.Fatalf("expected loopback listener and SDK/IMDS region discovery, got %+v", options)
	}
}

func TestOptionsAcceptExplicitRegionAndAddress(t *testing.T) {
	options, err := parseOptions([]string{"--region=eu-west-1", "--web.listen-address=127.0.0.1:19017"})
	if err != nil || options.region != "eu-west-1" || options.listenAddress != "127.0.0.1:19017" {
		t.Fatalf("unexpected options: %+v, %v", options, err)
	}
}

func TestOptionsRejectUnknownAndPositionalArguments(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"unexpected.yml"}} {
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("accepted unsupported arguments %q", args)
		}
	}
}
