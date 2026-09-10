[CmdletBinding(SupportsShouldProcess)]
param([string]$IconPath, [switch]$DryRun)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$source = Join-Path $root 'cmd/kairi-launcher-winforms'
$output = Join-Path $root '_local/bin/Kairisei-Launcher.exe'
$csc = Join-Path $env:WINDIR 'Microsoft.NET/Framework64/v4.0.30319/csc.exe'
if (-not (Test-Path -LiteralPath $csc -PathType Leaf)) { throw 'Windows x64 with .NET Framework 4 is required.' }
$arguments = @('/nologo', '/target:winexe', '/platform:x64', '/optimize+', "/out:$output", "/win32manifest:$(Join-Path $source 'Kairisei-Launcher.exe.manifest')",
    '/reference:System.dll', '/reference:System.Core.dll', '/reference:System.Drawing.dll', '/reference:System.Windows.Forms.dll', '/reference:System.Web.Extensions.dll',
    (Join-Path $source 'Program.cs'), (Join-Path $source 'ServerController.cs'))
if ($IconPath) {
    $icon = (Resolve-Path -LiteralPath $IconPath -ErrorAction Stop).Path
    $arguments += "/win32icon:$icon"
}
if ($DryRun -or -not $PSCmdlet.ShouldProcess($output, 'Build Windows launcher; replace previous build output')) {
    [ordered]@{ output = $output; icon = $IconPath } | ConvertTo-Json
    return
}
New-Item -ItemType Directory -Path (Split-Path -Parent $output) -Force | Out-Null
& $csc @arguments
if ($LASTEXITCODE -ne 0) { throw 'Launcher build failed.' }
Write-Output $output
