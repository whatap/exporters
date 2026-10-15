package main

import "testing"

func TestVersionFlagIsAcceptedWithoutStartingTheExporter(t *testing.T) {
	options, err := parseOptions([]string{"--version"})
	if err != nil || !options.showVersion {
		t.Fatalf("expected --version to be parsed, got %+v, %v", options, err)
	}
}

func TestVersionLineDefaultsWhenBuildMetadataIsNotInjected(t *testing.T) {
	if got, want := versionLine(), "ec2_state_exporter version dev (released unknown)"; got != want {
		t.Fatalf("versionLine() = %q, want %q", got, want)
	}
}
