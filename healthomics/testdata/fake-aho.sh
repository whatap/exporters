#!/bin/sh
# Stand-in for `aws-healthomics-tools` in tests. It enforces the argument contract the exporter
# relies on: run_analyzer <runId> --region <region> -o <path>.
[ "$FAKE_AHO_MODE" = "fail" ] && { echo "No run manifest found for run $2" >&2; exit 1; }
[ "$FAKE_AHO_MODE" = "hang" ] && { sleep 30; exit 0; }
if [ "$1" != "run_analyzer" ] || [ "$3" != "--region" ] || [ "$5" != "-o" ] || [ "$#" -ne 6 ]; then
  echo "unexpected arguments: $*" >&2
  exit 3
fi
case "$2" in *[!0-9]*|"") echo "bad run id: $2" >&2; exit 3;; esac
cp "$(dirname "$0")/run-analyzer-0.14.0-run-5930278.csv" "$6"
