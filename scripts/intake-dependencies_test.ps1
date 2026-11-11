$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'intake-dependencies.ps1')

function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) {
        throw $Message
    }
}

function Assert-Throws {
    param([scriptblock]$Action, [type]$Expected, [string]$Message)
    try {
        & $Action
    } catch {
        if ($Expected.IsInstanceOfType($_.Exception)) {
            return
        }
        throw "Unexpected exception for ${Message}: $($_.Exception)"
    }
    throw "Expected exception: $Message"
}

function New-TestZip {
    param([string]$Path, [string]$Entry)
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $stream = [System.IO.File]::Open($Path, [System.IO.FileMode]::CreateNew)
    try {
        $archive = [System.IO.Compression.ZipArchive]::new($stream, [System.IO.Compression.ZipArchiveMode]::Create)
        try {
            $item = $archive.CreateEntry($Entry)
            $writer = [System.IO.StreamWriter]::new($item.Open())
            try {
                $writer.Write('fixture')
            } finally {
                $writer.Dispose()
            }
        } finally {
            $archive.Dispose()
        }
    } finally {
        $stream.Dispose()
    }
}

function Test-LaneBoundary {
    param([string]$Directory, [string]$Shell)

    $realGo = Get-Command go -CommandType Application -ErrorAction SilentlyContinue
    if (-not $realGo) {
        Write-Host 'BLOCKED: lane-boundary fixtures require a local Go compiler'
        return
    }

    $bin = Join-Path $Directory 'fixture-bin'
    New-Item -ItemType Directory -Path $bin -Force | Out-Null
    $suffix = ''
    if ([System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT) {
        $suffix = '.exe'
    }
    $fakeGo = Join-Path $bin "go$suffix"
    $fakeFormat = Join-Path $bin "gofmt$suffix"
    $source = Join-Path $Directory 'fake-go.go'
    @'
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if strings.HasPrefix(filepath.Base(os.Args[0]), "gofmt") {
		return
	}
	log, err := os.OpenFile(os.Getenv("TBTD_FIXTURE_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	_, err = fmt.Fprintln(log, strings.Join(os.Args[1:], " "), os.Getenv("GOPROXY"), os.Getenv("GOFLAGS"))
	if err != nil {
		_ = log.Close()
		os.Exit(2)
	}
	if err = log.Close(); err != nil {
		os.Exit(2)
	}
	if len(os.Args) > 1 && os.Args[1] == "run" && os.Getenv("TBTD_FIXTURE_FAIL") == "1" {
		os.Exit(1)
	}
}
'@ | Set-Content -LiteralPath $source -Encoding ASCII

    $keys = @('PATH', 'GO111MODULE', 'GOTOOLCHAIN', 'GOWORK', 'GOENV', 'GOPROXY', 'GOSUMDB', 'GOFLAGS', 'CGO_ENABLED', 'TBTD_FIXTURE_LOG', 'TBTD_FIXTURE_FAIL')
    $saved = @{}
    foreach ($key in $keys) {
        $saved[$key] = [System.Environment]::GetEnvironmentVariable($key, 'Process')
    }
    try {
        $env:GO111MODULE = 'off'
        $env:GOTOOLCHAIN = 'local'
        $env:GOWORK = 'off'
        $env:GOENV = 'off'
        $env:GOPROXY = 'off'
        $env:GOSUMDB = 'off'
        $env:GOFLAGS = ''
        $env:CGO_ENABLED = '0'
        & $realGo.Source build -o $fakeGo $source
        if ($LASTEXITCODE -ne 0) {
            throw 'cannot build isolated lane fixture'
        }
        Copy-Item -LiteralPath $fakeGo -Destination $fakeFormat

        $checkRoot = Join-Path $Directory 'lane-project'
        $checkScripts = Join-Path $checkRoot 'scripts'
        New-Item -ItemType Directory -Path $checkScripts -Force | Out-Null
        New-Item -ItemType Directory -Path (Join-Path $checkRoot 'build') -Force | Out-Null
        New-Item -ItemType Directory -Path (Join-Path $checkRoot 'vendor') -Force | Out-Null
        Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'check.ps1') -Destination $checkScripts
        Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'check.sh') -Destination $checkScripts
        Set-Content -LiteralPath (Join-Path $checkScripts 'intake-dependencies_test.ps1') -Value "Write-Host 'PASSED: isolated fixture'"
        Set-Content -LiteralPath (Join-Path $checkRoot 'fixture.go') -Value "package fixture`n"
        Set-Content -LiteralPath (Join-Path $checkRoot 'vendor/modules.txt') -Value 'fixture'
        $log = Join-Path $Directory 'lane-calls.txt'
        $env:TBTD_FIXTURE_LOG = $log
        $env:TBTD_FIXTURE_FAIL = '0'

        $paths = @(@{ Name = 'PowerShell'; Program = $Shell; Arguments = @('-NoProfile', '-File', (Join-Path $checkScripts 'check.ps1')); Path = $bin + [System.IO.Path]::PathSeparator + $saved.PATH })
        if ([System.IO.Path]::DirectorySeparatorChar -eq '/') {
            $sh = Get-Command sh -CommandType Application -ErrorAction SilentlyContinue
            $find = Get-Command find -CommandType Application -ErrorAction SilentlyContinue
            $dirname = Get-Command dirname -CommandType Application -ErrorAction SilentlyContinue
            if ($sh -and $find -and $dirname) {
                New-Item -ItemType SymbolicLink -Path (Join-Path $bin 'find') -Target $find.Source | Out-Null
                New-Item -ItemType SymbolicLink -Path (Join-Path $bin 'dirname') -Target $dirname.Source | Out-Null
                $paths += @{ Name = 'portable shell'; Program = $sh.Source; Arguments = @((Join-Path $checkScripts 'check.sh')); Path = $bin }
            } else {
                Write-Host 'BLOCKED: portable lane fixture requires sh, find and dirname'
            }
        } else {
            Write-Host 'UNRUN: portable shell fallback fixture requires a POSIX host'
        }

        foreach ($lane in $paths) {
            $env:PATH = $lane.Path
            $sum = Join-Path $checkRoot 'go.sum'
            $lock = Join-Path $checkRoot 'build/toolchain-lock.json'
            if (Test-Path -LiteralPath $sum) { Remove-Item -LiteralPath $sum }
            Set-Content -LiteralPath $lock -Value 'fixture'
            if (Test-Path -LiteralPath $log) { Remove-Item -LiteralPath $log }
            $output = @(& $lane.Program @($lane.Arguments) 2>&1) -join "`n"
            Assert-True ($LASTEXITCODE -eq 0) "$($lane.Name): missing sum should block"
            Assert-True ($output -match 'BLOCKED: source architecture check') "$($lane.Name): missing sum was not reported"
            Assert-True (-not (Test-Path -LiteralPath $log)) "$($lane.Name): Go ran without a sum"

            Set-Content -LiteralPath $sum -Value 'fixture'
            Remove-Item -LiteralPath $lock
            $output = @(& $lane.Program @($lane.Arguments) 2>&1) -join "`n"
            Assert-True ($LASTEXITCODE -eq 0) "$($lane.Name): missing lock should block"
            Assert-True ($output -match 'BLOCKED: source architecture check') "$($lane.Name): missing lock was not reported"
            Assert-True (-not (Test-Path -LiteralPath $log)) "$($lane.Name): Go ran without a lock"

            Set-Content -LiteralPath $lock -Value 'fixture'
            $output = @(& $lane.Program @($lane.Arguments) 2>&1) -join "`n"
            Assert-True ($LASTEXITCODE -eq 0) "$($lane.Name): successful checker failed"
            Assert-True ($output -match 'PASSED: selected source architecture checks') "$($lane.Name): checker success was not reported"
            $calls = Get-Content -LiteralPath $log -Raw
            Assert-True ($calls -match 'run ./cmd/sourcecheck off -mod=vendor') "$($lane.Name): offline checker invocation missing"
            Assert-True ($calls -match 'vet ./...') "$($lane.Name): vet was not retained"
            Assert-True ($calls -match 'test -race ./...') "$($lane.Name): race test was not retained"

            Remove-Item -LiteralPath $log
            $env:TBTD_FIXTURE_FAIL = '1'
            $output = @(& $lane.Program @($lane.Arguments) 2>&1) -join "`n"
            Assert-True ($LASTEXITCODE -eq 1) "$($lane.Name): checker failure did not fail the lane"
            Assert-True ($output -match 'FAILED: source architecture check') "$($lane.Name): checker failure was not reported"
            Assert-True ($output -notmatch 'PASSED: selected source architecture checks') "$($lane.Name): failed checker was reported as passed"
            $calls = Get-Content -LiteralPath $log -Raw
            Assert-True ($calls -match 'test -race ./...') "$($lane.Name): checker failure suppressed later checks"
            $env:TBTD_FIXTURE_FAIL = '0'
        }
    } finally {
        foreach ($key in $keys) {
            [System.Environment]::SetEnvironmentVariable($key, $saved[$key], 'Process')
        }
    }
}

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ('intake-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temp | Out-Null
try {
    $review = [pscustomobject]@{
        version = '1.0.0'
        reviewer = 'reviewer'
        availability = 'availability evidence'
        provenance = 'provenance evidence'
        source = 'source evidence'
        build = 'build evidence'
        security = 'security evidence'
        license = 'license evidence'
        replacementReviewer = ''
        replacementRationale = ''
    }
    Test-Review $review 'fixture' '1.0.0' '1.0.0'
    Assert-Throws { Test-Review $review 'fixture' '1.0.0' '2.0.0' } ([IntakeBlockedException]) 'unreviewed replacement'
    $review.replacementReviewer = 'reviewer'
    $review.replacementRationale = 'proposed version unavailable'
    Test-Review $review 'fixture' '1.0.0' '2.0.0'
    $review.security = ''
    Assert-Throws { Test-Review $review 'fixture' '1.0.0' '1.0.0' } ([IntakeBlockedException]) 'missing security evidence'
    Assert-Throws { Get-UniqueReview @() 'missing' } ([IntakeBlockedException]) 'unavailable pin'
    Assert-Throws { Get-UniqueReview @($review, $review) '' } ([IntakeBlockedException]) 'missing named review'

    $zip = Join-Path $temp 'valid.zip'
    New-TestZip $zip 'fixture/LICENSE'
    $entries = Get-ArchiveEntries $zip
    Assert-True ('fixture/LICENSE' -cin $entries) 'valid archive entry was not found'
    $badZip = Join-Path $temp 'unsafe.zip'
    New-TestZip $badZip '../escape'
    Assert-Throws { Get-ArchiveEntries $badZip } ([System.IO.InvalidDataException]) 'archive traversal'
    Assert-Throws { Test-SafeArchivePath 'C:/escape' } ([System.IO.InvalidDataException]) 'drive path'
    Assert-Throws { Test-SafeArchivePath 'dir\escape' } ([System.IO.InvalidDataException]) 'backslash path'

    $hash = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash
    $artifact = [pscustomobject]@{ path = $zip; sha256 = $hash; reviewer = 'reviewer'; url = '' }
    $observed = Resolve-Artifact $artifact $temp (Join-Path $temp 'copy.zip') $false
    Assert-True ($observed -ceq $hash.ToLowerInvariant()) 'observed digest differs'
    $artifact.sha256 = '0' * 64
    Assert-Throws {
        Resolve-Artifact $artifact $temp (Join-Path $temp 'corrupt.zip') $false
    } ([System.IO.InvalidDataException]) 'corrupt digest'
    $artifact.path = Join-Path $temp 'missing.zip'
    Assert-Throws {
        Resolve-Artifact $artifact $temp (Join-Path $temp 'missing-copy.zip') $false
    } ([IntakeBlockedException]) 'unavailable artifact'
    $artifact.sha256 = 'not-a-digest'
    Assert-Throws {
        Resolve-Artifact $artifact $temp (Join-Path $temp 'invalid.zip') $false
    } ([System.IO.InvalidDataException]) 'malformed artifact record'

    $root = Join-Path $temp 'project'
    $stage = Join-Path $temp 'stage'
    foreach ($directory in @($root, $stage)) {
        New-Item -ItemType Directory -Path (Join-Path $directory 'build') -Force | Out-Null
        New-Item -ItemType Directory -Path (Join-Path $directory 'vendor') -Force | Out-Null
        Set-Content -LiteralPath (Join-Path $directory 'go.mod') -Value 'old'
        Set-Content -LiteralPath (Join-Path $directory 'go.sum') -Value 'old'
        Set-Content -LiteralPath (Join-Path $directory 'vendor/modules.txt') -Value 'old'
        Set-Content -LiteralPath (Join-Path $directory 'build/toolchain-lock.json') -Value 'old'
        Set-Content -LiteralPath (Join-Path $directory 'build/dependency-intake.json') -Value 'old'
    }
    Set-Content -LiteralPath (Join-Path $stage 'go.mod') -Value 'new'
    Set-Content -LiteralPath (Join-Path $stage 'go.sum') -Value 'new'
    Set-Content -LiteralPath (Join-Path $stage 'vendor/modules.txt') -Value 'new'
    Assert-Throws { Publish-Intake $root $stage 2 } ([System.IO.IOException]) 'interrupted generation'
    Assert-True ((Get-Content -LiteralPath (Join-Path $root 'go.mod') -Raw).Trim() -ceq 'old') 'rollback did not restore go.mod'
    Assert-True ((Get-Content -LiteralPath (Join-Path $root 'go.sum') -Raw).Trim() -ceq 'old') 'rollback did not restore go.sum'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $root 'build/.intake-pending.json'))) 'rollback left a journal'
    Publish-Intake $root $stage
    Publish-Intake $root $stage
    Assert-True ((Get-Content -LiteralPath (Join-Path $root 'go.mod') -Raw).Trim() -ceq 'new') 'regeneration changed output'
    Assert-True ((Get-Content -LiteralPath (Join-Path $root 'vendor/modules.txt') -Raw).Trim() -ceq 'new') 'vendor regeneration changed output'

    $recoveryRoot = Join-Path $temp 'recovery'
    $recoveryBuild = Join-Path $recoveryRoot 'build'
    $backupName = '.intake-backup-' + [guid]::NewGuid().ToString('N')
    $backup = Join-Path $recoveryBuild $backupName
    New-Item -ItemType Directory -Path $backup -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $backup '0') -Value 'original module'
    Set-Content -LiteralPath (Join-Path $recoveryRoot 'go.mod') -Value 'partial publication'
    Set-Content -LiteralPath (Join-Path $recoveryRoot 'go.sum') -Value 'partial publication'
    @{ backup = $backupName; existing = @($true, $false, $false, $false, $false) } |
        ConvertTo-Json | Set-Content -LiteralPath (Join-Path $recoveryBuild '.intake-pending.json')
    Assert-Throws { Invoke-Intake $recoveryRoot '' $false $false } ([IntakeBlockedException]) 'missing evidence after interrupted publication'
    Assert-True ((Get-Content -LiteralPath (Join-Path $recoveryRoot 'go.mod') -Raw).Trim() -ceq 'original module') 'recovery did not restore original module'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $recoveryRoot 'go.sum'))) 'recovery retained a newly published sum'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $recoveryBuild '.intake-pending.json'))) 'recovery retained the journal'

    $checkRoot = Join-Path $temp 'check-project'
    $checkScripts = Join-Path $checkRoot 'scripts'
    New-Item -ItemType Directory -Path $checkScripts -Force | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $checkRoot 'build') -Force | Out-Null
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'check.ps1') -Destination $checkScripts
    Set-Content -LiteralPath (Join-Path $checkScripts 'intake-dependencies_test.ps1') -Value "Write-Host 'PASSED: isolated fixture'"
    Set-Content -LiteralPath (Join-Path $checkRoot 'fixture.go') -Value "package fixture`n"
    Set-Content -LiteralPath (Join-Path $checkRoot 'build/.intake-pending.json') -Value '{}'
    $shell = (Get-Process -Id $PID).Path
    $output = @(& $shell -NoProfile -File (Join-Path $checkScripts 'check.ps1') 2>&1) -join "`n"
    Assert-True ($LASTEXITCODE -eq 0) 'blocked check unexpectedly failed'
    Assert-True ($output -match 'BLOCKED: interrupted intake') 'check did not report interrupted intake'
    Assert-True ($output -match 'BLOCKED: dependency verification needs reviewed evidence') 'check did not report missing evidence'
    Assert-True ($output -notmatch 'PASSED: selected source architecture checks') 'blocked source check was reported as passed'
    Assert-True ($output -match 'UNRUN: native Windows package') 'check did not retain native qualification status'

    Test-LaneBoundary $temp $shell

    $malformed = Join-Path $temp 'malformed.json'
    Set-Content -LiteralPath $malformed -Value '{'
    Assert-Throws { Read-JSONFile $malformed } ([System.IO.InvalidDataException]) 'malformed JSON'
    Write-Host 'PASSED: offline dependency-intake fixtures'
} finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction Stop
}
