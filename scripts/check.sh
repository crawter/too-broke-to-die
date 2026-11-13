#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
go mod tidy
go run ./cmd/tbtdcontent
unformatted=$(find . -name '*.go' -type f -exec gofmt -l {} +)
if [ -n "$unformatted" ]; then
    printf 'Go files require gofmt:\n%s\n' "$unformatted" >&2
    exit 1
fi
go vet ./...
go build ./...
go test ./...
printf '%s\n' 'Display-free Linux source checks passed; final media and manual qualification are not implied.'
