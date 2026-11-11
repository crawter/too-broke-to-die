[CmdletBinding()]
param(
    [string]$Evidence
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$failed = $false

Push-Location $root
try {
    if (Test-Path -LiteralPath 'build/.intake-pending.json') {
        Write-Host 'BLOCKED: interrupted intake requires recovery before dependency checks'
    }

    try {
        & (Join-Path $PSScriptRoot 'intake-dependencies_test.ps1')
    } catch {
        Write-Host "FAILED: intake fixtures: $($_.Exception.Message)"
        $failed = $true
    }

    if ($Evidence) {
        & (Join-Path $PSScriptRoot 'intake-dependencies.ps1') -Root $root -Evidence $Evidence
        if ($LASTEXITCODE -ne 0) {
            $failed = $true
        }
    } else {
        Write-Host 'BLOCKED: dependency verification needs reviewed evidence; pass -Evidence'
    }

    $go = Get-Command go -ErrorAction SilentlyContinue
    $files = @(Get-ChildItem -Path $root -Filter '*.go' -File -Recurse |
        Where-Object { $_.FullName -notmatch '[\\/](vendor|\.git|\.intake-[^\\/]+)[\\/]' })
    if (-not $go) {
        Write-Host 'BLOCKED: Go source checks require a local Go toolchain'
    } elseif ($files.Count -eq 0) {
        Write-Host 'UNRUN: Go source architecture, formatting, vet, tests and race tests; no Go source exists'
    } else {
        $gofmt = Get-Command gofmt -ErrorAction SilentlyContinue
        if (-not $gofmt) {
            Write-Host 'BLOCKED: gofmt is unavailable'
        } else {
            $unformatted = @(& $gofmt.Source -l @($files.FullName))
            if ($LASTEXITCODE -ne 0 -or $unformatted.Count -gt 0) {
                Write-Host "FAILED: gofmt $($unformatted -join ', ')"
                $failed = $true
            } else {
                Write-Host 'PASSED: gofmt'
            }
        }
        if (-not (Test-Path -LiteralPath 'vendor/modules.txt') -or
            -not (Test-Path -LiteralPath 'go.sum') -or
            -not (Test-Path -LiteralPath 'build/toolchain-lock.json') -or
            (Test-Path -LiteralPath 'build/.intake-pending.json')) {
            Write-Host 'BLOCKED: source architecture check, vet, tests and race tests require completed dependency intake'
        } else {
            $saved = @{}
            $settings = @{ GOTOOLCHAIN = 'local'; GOWORK = 'off'; GOENV = 'off'; GOPROXY = 'off'; GOSUMDB = 'off'; GOFLAGS = '-mod=vendor' }
            foreach ($key in $settings.Keys) {
                $saved[$key] = [System.Environment]::GetEnvironmentVariable($key, 'Process')
                [System.Environment]::SetEnvironmentVariable($key, $settings[$key], 'Process')
            }
            try {
                & $go.Source run ./cmd/sourcecheck
                if ($LASTEXITCODE -ne 0) {
                    Write-Host 'FAILED: source architecture check'
                    $failed = $true
                } else {
                    Write-Host 'PASSED: selected source architecture checks'
                    Write-Host 'UNRUN: complete architecture and instrumentation review'
                }
                foreach ($command in @(@('vet', './...'), @('test', './...'), @('test', '-race', './...'))) {
                    & $go.Source @command
                    if ($LASTEXITCODE -ne 0) {
                        Write-Host "FAILED: go $($command -join ' ')"
                        $failed = $true
                    } else {
                        Write-Host "PASSED: go $($command -join ' ')"
                    }
                }
            } finally {
                foreach ($key in $settings.Keys) {
                    [System.Environment]::SetEnvironmentVariable($key, $saved[$key], 'Process')
                }
            }
        }
    }
    Write-Host 'UNRUN: native Windows package, reproducibility, lifecycle, final media, specialist review and complete-game qualification'
} finally {
    Pop-Location
}

if ($failed) {
    exit 1
}
exit 0
