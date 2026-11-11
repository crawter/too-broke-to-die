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

    $malformed = Join-Path $temp 'malformed.json'
    Set-Content -LiteralPath $malformed -Value '{'
    Assert-Throws { Read-JSONFile $malformed } ([System.IO.InvalidDataException]) 'malformed JSON'
    Write-Host 'PASSED: offline dependency-intake fixtures'
} finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction Stop
}
