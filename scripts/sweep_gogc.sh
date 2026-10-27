#!/usr/bin/env bash
# Week 1 sanity check: run gcprobe under several GOGC values and save one CSV
# per setting, plus a log with the toolchain version and run summaries.
#
# Usage: scripts/sweep_gogc.sh [rate_MiBps] [lifetime] [duration]
set -euo pipefail

RATE="${1:-200}"
LIFETIME="${2:-500ms}"
DURATION="${3:-10s}"
OUT="results/week1"
# Fix GOMAXPROCS so the GC CPU fraction is comparable across machines and runs.
export GOMAXPROCS="${GOMAXPROCS:-4}"

cd "$(dirname "$0")/.."
mkdir -p "$OUT"
go build -o bin/gcprobe ./cmd/gcprobe

{
  echo "date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "go: $(go version)"
  echo "host: $(uname -srm)"
  echo "GOMAXPROCS: ${GOMAXPROCS:-default}  GOEXPERIMENT: ${GOEXPERIMENT:-default}"
  echo "rate=${RATE}MiB/s lifetime=${LIFETIME} duration=${DURATION}"
} > "$OUT/run_info.txt"

for g in 50 100 200 400; do
  echo "== GOGC=$g"
  GOGC=$g ./bin/gcprobe -rate "$RATE" -lifetime "$LIFETIME" -duration "$DURATION" \
    -out "$OUT/gogc_${g}.csv" 2>> "$OUT/run_info.txt"
done
echo "Done. CSVs and run_info.txt are in $OUT/"
