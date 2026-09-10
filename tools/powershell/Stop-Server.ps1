[CmdletBinding(SupportsShouldProcess)]
param(
    [string]$PackageRoot = (Join-Path $PSScriptRoot '../../_local/deployment/kairisei-ma-cn602-server'),
    [switch]$DryRun
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$entry = Join-Path ([IO.Path]::GetFullPath($PackageRoot)) 'Stop-Server.ps1'
if (-not (Test-Path -LiteralPath $entry -PathType Leaf)) { throw 'Extract the complete release into PackageRoot first; see docs/BUILD.md.' }
if ($DryRun -or $PSCmdlet.ShouldProcess($entry, 'Request graceful server shutdown')) {
    & $entry -DryRun:$DryRun -Confirm:$false
}
