#!/bin/sh
set -u

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd) || exit 1
cd "$root" || exit 1

if command -v pwsh >/dev/null 2>&1; then
    exec pwsh -NoProfile -File "$root/scripts/check.ps1" "$@"
fi
if command -v powershell >/dev/null 2>&1; then
    exec powershell -NoProfile -File "$root/scripts/check.ps1" "$@"
fi

echo "BLOCKED: PowerShell is required for intake and its offline fixtures"
if [ "$#" -gt 0 ]; then
    echo "BLOCKED: intake arguments cannot be processed without PowerShell"
fi
if [ -f build/.intake-pending.json ]; then
    echo "BLOCKED: interrupted intake requires recovery"
fi
if ! command -v go >/dev/null 2>&1; then
    echo "BLOCKED: Go source checks require a local Go toolchain"
    echo "UNRUN: native Windows package and target-machine qualification"
    exit 0
fi

files=$(find . \( -name .git -o -name vendor -o -name '.intake-*' \) -prune -o -name '*.go' -type f -print)
if [ -z "$files" ]; then
    echo "UNRUN: Go formatting, vet, tests and race tests; no Go source exists"
    echo "UNRUN: native Windows package and target-machine qualification"
    exit 0
fi

failed=0
if command -v gofmt >/dev/null 2>&1; then
    # Project source paths are passed individually, including paths with spaces.
    format_result=$(find . \( -name .git -o -name vendor -o -name '.intake-*' \) -prune -o -name '*.go' -type f -exec gofmt -l {} \;) || failed=1
    if [ -n "$format_result" ]; then
        echo "FAILED: gofmt"
        printf '%s\n' "$format_result"
        failed=1
    elif [ "$failed" -eq 0 ]; then
        echo "PASSED: gofmt"
    else
        echo "FAILED: gofmt could not inspect source"
    fi
else
    echo "BLOCKED: gofmt is unavailable"
fi

 if [ ! -f vendor/modules.txt ] || [ -f build/.intake-pending.json ]; then
    echo "BLOCKED: vet, tests and race tests require completed dependency intake"
else
    export GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off GOFLAGS=-mod=vendor
    for check in vet test race; do
        case "$check" in
            vet) go vet ./... ;;
            test) go test ./... ;;
            race) go test -race ./... ;;
        esac
        result=$?
        if [ "$result" -eq 0 ]; then
            echo "PASSED: go $check"
        else
            echo "FAILED: go $check"
            failed=1
        fi
    done
fi

echo "UNRUN: native Windows package, final media and target-machine qualification"
exit "$failed"
