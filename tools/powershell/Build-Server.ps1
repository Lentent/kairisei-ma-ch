[CmdletBinding(SupportsShouldProcess)]
param(
    [ValidateSet('windows-amd64', 'linux-amd64', 'linux-arm64')]
    [string[]]$Targets = @('windows-amd64', 'linux-amd64', 'linux-arm64'),
    [switch]$SkipTests,
    [switch]$DryRun
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$outputRoot = Join-Path $projectRoot '_local/bin'
$go = (Get-Command go -CommandType Application -ErrorAction Stop).Source
if ($DryRun -or -not $PSCmdlet.ShouldProcess($outputRoot, 'Build selected server binaries; replace previous build outputs')) {
    [ordered]@{ source = $projectRoot; output = $outputRoot; targets = $Targets; tests = -not $SkipTests } | ConvertTo-Json
    return
}
$previous = @{}
foreach ($name in @('GOOS', 'GOARCH', 'CGO_ENABLED')) {
    $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
try {
    $env:CGO_ENABLED = '0'
    # Tests run on this computer, before setting the cross-compilation target.
    $env:GOOS = $null
    $env:GOARCH = $null
    New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
    Push-Location (Join-Path $projectRoot 'server')
    try {
        if (-not $SkipTests) {
            & $go test ./...
            if ($LASTEXITCODE -ne 0) { throw 'go test failed.' }
            & $go vet ./...
            if ($LASTEXITCODE -ne 0) { throw 'go vet failed.' }
        }
        foreach ($target in ($Targets | Select-Object -Unique)) {
            $env:GOOS, $env:GOARCH = $target.Split('-')
            $name = if ($env:GOOS -eq 'windows') { 'kairi-server.exe' } else { "kairi-server-$target" }
            $output = Join-Path $outputRoot $name
            & $go build -buildvcs=false -trimpath '-ldflags=-s -w' -o $output ./cmd/kairi-server
            if ($LASTEXITCODE -ne 0) { throw "Build failed: $target" }
            [ordered]@{ target = $target; output = $output; sha256 = (Get-FileHash -LiteralPath $output -Algorithm SHA256).Hash.ToLowerInvariant() } | ConvertTo-Json -Compress
        }
    } finally { Pop-Location }
} finally {
    foreach ($name in $previous.Keys) { [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process') }
}
