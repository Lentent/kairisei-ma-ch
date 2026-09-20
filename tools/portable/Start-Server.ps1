[CmdletBinding(SupportsShouldProcess)]
param(
    [ValidateNotNullOrEmpty()]
    [string]$AdvertiseHost = '10.0.2.2',

    [ValidateNotNullOrEmpty()]
    [string]$ListenHost = '0.0.0.0',

    [ValidateRange(1, 65533)]
    [int]$Port = 26020,

    [ValidateRange(5, 120)]
    [int]$ReadinessTimeoutSeconds = 60,

    [switch]$DryRun,

    [switch]$ValidationOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-AdvertiseIPv4 {
    param([string]$Address)

    $parsed = $null
    if (-not [Net.IPAddress]::TryParse($Address, [ref]$parsed) -or
        $parsed.AddressFamily -ne [Net.Sockets.AddressFamily]::InterNetwork) {
        throw "AdvertiseHost must be an IPv4 address: $Address"
    }
    $bytes = $parsed.GetAddressBytes()
    if ($bytes[0] -eq 0 -or ($bytes[0] -ge 224 -and $bytes[0] -le 239)) {
        throw "AdvertiseHost must be a unicast IPv4 address: $Address"
    }
    return $parsed.ToString()
}

function Assert-RequiredPath {
    param([string]$LiteralPath, [string]$PathType = 'Leaf')
    if (-not (Test-Path -LiteralPath $LiteralPath -PathType $PathType)) {
        throw "Required portable-server path is missing: $LiteralPath"
    }
}

function Quote-NativeArgument {
    param([string]$Value)
    if ($Value.Contains('"')) {
        throw 'Portable-server paths and arguments must not contain a double quote.'
    }
    return '"' + $Value + '"'
}

function Add-NativeArgument {
    param(
        [Collections.Generic.List[string]]$List,
        [string]$Name,
        [string]$Value
    )
    $List.Add($Name)
    $List.Add((Quote-NativeArgument -Value $Value))
}

function Test-PortAvailable {
    param([int]$CandidatePort)
    $listener = $null
    try {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $CandidatePort)
        $listener.Start()
        return $true
    }
    catch {
        return $false
    }
    finally {
        if ($null -ne $listener) { $listener.Stop() }
    }
}

$packageRoot = [IO.Path]::GetFullPath($PSScriptRoot)
$serverPath = Join-Path $packageRoot 'kairi-server.exe'
$configRoot = Join-Path $packageRoot 'server\config'
$controlRoot = Join-Path $packageRoot '_local\control\server'
$resourceRoot = Join-Path $packageRoot '_local\resources'
$dataRoot = Join-Path $packageRoot '_local\data'
$deploymentPath = Join-Path $packageRoot 'deployment.json'
$runRoot = Join-Path $packageRoot ('_local\runtime\server-runs\' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ'))
$statePath = Join-Path $dataRoot 'server-state.json'
$saveSeedPath = Join-Path $configRoot 'cn602-save-template.json'
$savePath = Join-Path $dataRoot 'cn602-save.json'
$saveDatabasePath = Join-Path $dataRoot 'cn602-save-state.sqlite3'
$requestLogPath = Join-Path $runRoot 'requests.jsonl'
$stdoutPath = Join-Path $runRoot 'stdout.log'
$stderrPath = Join-Path $runRoot 'stderr.log'
$assetMapPath = Join-Path $controlRoot 'cn602-private-derived-avatar-icons-asset-map.json'
$cardMasterPath = Join-Path $controlRoot 'cn602-card-runtime-master.json'
$exploreMasterPath = Join-Path $controlRoot 'cn602-explore-runtime-master.json'
$storyMasterPath = Join-Path $controlRoot 'cn602-story-runtime-master.json'
$battleMasterPath = Join-Path $controlRoot 'cn602-battle-runtime-master.json'
$combatCardPath = Join-Path $controlRoot 'cn602-card-master\card.csv'
$combatMasterRoot = Join-Path $controlRoot 'cn602-battle-master'
$naviMasterPath = Join-Path $controlRoot 'cn602-navi-runtime-master.json'
$itemMasterPath = Join-Path $controlRoot 'cn602-item-runtime-master.json'
$avatarMasterPath = Join-Path $controlRoot 'cn602-avatar-runtime-master.json'
$gachaBannerPath = Join-Path $resourceRoot 'banners\gacha.png'
$fiveStarBannerPath = Join-Path $controlRoot 'cn602-gacha-five-star-banner.png'
$homeBannerPath = Join-Path $resourceRoot 'banners\home.png'
$stampMasterPath = Join-Path $controlRoot 'cn602-stamp-runtime-master.json'
$honorMasterPath = Join-Path $controlRoot 'cn602-honor-runtime-master.json'
$pvpMasterPath = Join-Path $configRoot 'cn602-pvp-runtime.json'
$playerProgressionPath = Join-Path $configRoot 'cn602-player-progression-runtime.json'
$loginBonusPath = Join-Path $configRoot 'cn602-login-bonus-runtime.json'
$cpkRoot = Join-Path $resourceRoot 'cpk'
$cpkAliasesPath = Join-Path $controlRoot 'cn602-cpk-aliases.json'
$imageRoot = $null
$avatarPatchRoot = Join-Path $controlRoot 'cn602-private-derived-avatar-icons'
$patchRoot = Join-Path $resourceRoot 'patch'
$resourceSetPath = Join-Path $packageRoot 'resource-set\resource-set.json'
$resourceSetSHA256 = $null
$resourceInputs = $null
if (Test-Path -LiteralPath $deploymentPath -PathType Leaf) {
    $deployment = Get-Content -LiteralPath $deploymentPath -Raw | ConvertFrom-Json
    if (-not $PSBoundParameters.ContainsKey('AdvertiseHost') -and
        $deployment.PSObject.Properties.Name -contains 'advertise_host') {
        $AdvertiseHost = [string]$deployment.advertise_host
    }
    if (-not $PSBoundParameters.ContainsKey('Port') -and
        $deployment.PSObject.Properties.Name -contains 'port') {
        $Port = [int]$deployment.port
    }
}
if (Test-Path -LiteralPath $resourceSetPath -PathType Leaf) {
    if (-not (Test-Path -LiteralPath $deploymentPath -PathType Leaf)) { throw 'Resource-set package needs deployment.json.' }
    # Runtime identity is derived from the actual manifest, not duplicated in
    # the editable deployment settings. Used only to detect a running old set.
    $resourceSetSHA256 = (Get-FileHash -LiteralPath $resourceSetPath -Algorithm SHA256).Hash.ToLowerInvariant()
    . (Join-Path $packageRoot 'Read-ResourceSet.ps1')
    $resourceSet = Read-PortableResourceSet -ManifestPath $resourceSetPath
    if ($resourceSet.Manifest.policy.publishable -ne $true -or $deployment.validation_only) {
        if (-not $ValidationOnly) {
            throw 'Candidate package requires -ValidationOnly; use the packaged test launcher or pass the switch explicitly.'
        }
    }
    $resourceInputs = $resourceSet.Entrypoints
    $bindings = @{
        'cn-save-seed'='saveSeedPath'; 'cn-asset-map'='assetMapPath'; 'cn-card-master'='cardMasterPath'
        'cn-explore-master'='exploreMasterPath'; 'cn-story-master'='storyMasterPath'; 'cn-battle-master'='battleMasterPath'
        'cn-combat-card-master'='combatCardPath'; 'cn-combat-master-root'='combatMasterRoot'; 'cn-navi-master'='naviMasterPath'
        'cn-item-master'='itemMasterPath'; 'cn-avatar-master'='avatarMasterPath'; 'cn-gacha-banner'='gachaBannerPath'
        'cn-five-star-gacha-banner'='fiveStarBannerPath'; 'cn-home-banner'='homeBannerPath'; 'cn-stamp-master'='stampMasterPath'
        'cn-honor-master'='honorMasterPath'; 'cn-pvp-master'='pvpMasterPath'; 'cn-player-progression'='playerProgressionPath'
        'cn-login-bonus'='loginBonusPath'; 'cn-cpk-root'='cpkRoot'; 'cn-cpk-aliases'='cpkAliasesPath'; 'cn-patch-root'='patchRoot'
        'cn-image-root'='imageRoot'
    }
    foreach ($name in $bindings.Keys) {
        if (-not $resourceInputs.ContainsKey($name)) { throw "Missing resource entrypoint: $name" }
        Set-Variable -Name $bindings[$name] -Value $resourceInputs[$name]
    }
    $avatarPatchRoot = $patchRoot
}
if ($Port -lt 1 -or $Port -gt 65533) {
    throw "Port must be 1 through 65533: $Port"
}
$battlePort = $Port + 1
$adminPort = $Port + 2
$healthURL = "http://127.0.0.1:$Port/healthz"
$adminHealthURL = "http://127.0.0.1:$adminPort/api/health"

$AdvertiseHost = Assert-AdvertiseIPv4 -Address $AdvertiseHost
foreach ($path in @(
    $serverPath, $saveSeedPath, $assetMapPath, $cardMasterPath, $exploreMasterPath,
    $storyMasterPath, $battleMasterPath, $combatCardPath, $naviMasterPath,
    $itemMasterPath, $avatarMasterPath, $gachaBannerPath, $fiveStarBannerPath,
    $homeBannerPath, $stampMasterPath, $honorMasterPath, $pvpMasterPath,
    $playerProgressionPath, $loginBonusPath, $cpkAliasesPath
)) {
    Assert-RequiredPath -LiteralPath $path
}
foreach ($path in @($combatMasterRoot, $cpkRoot, $avatarPatchRoot, $patchRoot)) {
    Assert-RequiredPath -LiteralPath $path -PathType Container
}
if ($null -ne $imageRoot) {
    Assert-RequiredPath -LiteralPath $imageRoot -PathType Container
}

if (Test-Path -LiteralPath $statePath -PathType Leaf) {
    $oldState = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    if ($null -ne $oldState.process_id -and [int]$oldState.process_id -gt 0) {
        $oldProcess = Get-Process -Id ([int]$oldState.process_id) -ErrorAction SilentlyContinue
        if ($null -ne $oldProcess) {
            $oldProcessPath = $null
            try { $oldProcessPath = [IO.Path]::GetFullPath($oldProcess.Path) } catch {}
            if ($null -ne $oldProcessPath -and
                $oldProcessPath.Equals($serverPath, [StringComparison]::OrdinalIgnoreCase)) {
                if ($oldState.port -ne $Port -or $oldState.listen_host -ne $ListenHost -or
                    $oldState.advertise_host -ne $AdvertiseHost -or
                    ($null -ne $resourceInputs -and
                     ($oldState.PSObject.Properties.Name -notcontains 'resource_set_sha256' -or
                      $oldState.resource_set_sha256 -ne $resourceSetSHA256))) {
                    throw 'Running package uses different resources or network settings; stop it before restarting.'
                }
                [ordered]@{
                    schema_version = 1
                    status = 'ALREADY_RUNNING'
                    process_id = [int]$oldState.process_id
                    health_url = $healthURL
                    admin_url = "http://127.0.0.1:$adminPort/"
                } | ConvertTo-Json -Depth 4
                return
            }
            throw "Recorded PID $($oldState.process_id) belongs to another executable; refusing to stop or replace it."
        }
    }
}

foreach ($candidatePort in @($Port, $battlePort, $adminPort)) {
    if (-not (Test-PortAvailable -CandidatePort $candidatePort)) {
        throw "TCP port $candidatePort is already in use. No existing process was stopped."
    }
}

$arguments = [Collections.Generic.List[string]]::new()
$cdnConfigPath = Join-Path $packageRoot 'cdn.json'
if (Test-Path -LiteralPath $cdnConfigPath -PathType Leaf) {
    Add-NativeArgument -List $arguments -Name '-cdn-config' -Value $cdnConfigPath
}
Add-NativeArgument -List $arguments -Name '-client-profile' -Value 'cn602-bootstrap'
Add-NativeArgument -List $arguments -Name '-request-log' -Value $requestLogPath
Add-NativeArgument -List $arguments -Name '-cn-save-path' -Value $savePath
Add-NativeArgument -List $arguments -Name '-cn-save-seed' -Value $saveSeedPath
Add-NativeArgument -List $arguments -Name '-cn-asset-map' -Value $assetMapPath
Add-NativeArgument -List $arguments -Name '-cn-card-master' -Value $cardMasterPath
Add-NativeArgument -List $arguments -Name '-cn-explore-master' -Value $exploreMasterPath
Add-NativeArgument -List $arguments -Name '-cn-story-master' -Value $storyMasterPath
Add-NativeArgument -List $arguments -Name '-cn-battle-master' -Value $battleMasterPath
Add-NativeArgument -List $arguments -Name '-cn-combat-card-master' -Value $combatCardPath
Add-NativeArgument -List $arguments -Name '-cn-combat-master-root' -Value $combatMasterRoot
Add-NativeArgument -List $arguments -Name '-cn-navi-master' -Value $naviMasterPath
Add-NativeArgument -List $arguments -Name '-cn-item-master' -Value $itemMasterPath
Add-NativeArgument -List $arguments -Name '-cn-avatar-master' -Value $avatarMasterPath
Add-NativeArgument -List $arguments -Name '-cn-gacha-banner' -Value $gachaBannerPath
Add-NativeArgument -List $arguments -Name '-cn-five-star-gacha-banner' -Value $fiveStarBannerPath
Add-NativeArgument -List $arguments -Name '-cn-home-banner' -Value $homeBannerPath
Add-NativeArgument -List $arguments -Name '-cn-stamp-master' -Value $stampMasterPath
Add-NativeArgument -List $arguments -Name '-cn-honor-master' -Value $honorMasterPath
Add-NativeArgument -List $arguments -Name '-cn-pvp-master' -Value $pvpMasterPath
Add-NativeArgument -List $arguments -Name '-cn-player-progression' -Value $playerProgressionPath
Add-NativeArgument -List $arguments -Name '-cn-login-bonus' -Value $loginBonusPath
Add-NativeArgument -List $arguments -Name '-cn-cpk-root' -Value $cpkRoot
Add-NativeArgument -List $arguments -Name '-cn-cpk-aliases' -Value $cpkAliasesPath
if ($null -ne $imageRoot) {
    Add-NativeArgument -List $arguments -Name '-cn-image-root' -Value $imageRoot
}
Add-NativeArgument -List $arguments -Name '-cn-patch-root' -Value $patchRoot
if ($null -eq $resourceInputs) {
    Add-NativeArgument -List $arguments -Name '-cn-patch-root' -Value $avatarPatchRoot
}
Add-NativeArgument -List $arguments -Name '-listen-host' -Value $ListenHost
Add-NativeArgument -List $arguments -Name '-advertise-host' -Value $AdvertiseHost
Add-NativeArgument -List $arguments -Name '-port' -Value ([string]$Port)
Add-NativeArgument -List $arguments -Name '-battle-port' -Value ([string]$battlePort)
Add-NativeArgument -List $arguments -Name '-admin-listen-host' -Value '127.0.0.1'
Add-NativeArgument -List $arguments -Name '-admin-port' -Value ([string]$adminPort)
$shutdownEvent = 'Local\Kairisei-' + [Guid]::NewGuid().ToString('N')
Add-NativeArgument -List $arguments -Name '-shutdown-event' -Value $shutdownEvent

$preview = [ordered]@{
    schema_version = 1
    action = 'START_PORTABLE_CN602_SERVER'
    executable = $serverPath
    resource_set_sha256 = $resourceSetSHA256
    advertise_host = $AdvertiseHost
    listen_host = $ListenHost
    port = $Port
    battle_port = $battlePort
    admin_port = $adminPort
    save_path = $saveDatabasePath
    fresh_save_created_on_first_run = -not (Test-Path -LiteralPath $saveDatabasePath -PathType Leaf)
}
if ($DryRun -or -not $PSCmdlet.ShouldProcess($serverPath, 'Start portable local server')) {
    $preview | ConvertTo-Json -Depth 4
    return
}

New-Item -ItemType Directory -Path $dataRoot -Force | Out-Null
New-Item -ItemType Directory -Path $runRoot -Force | Out-Null
# The server initializes a missing save through the normal onboarding factory.
New-Item -ItemType File -Path $requestLogPath -Force | Out-Null

$process = Start-Process `
    -FilePath $serverPath `
    -ArgumentList $arguments.ToArray() `
    -WorkingDirectory $packageRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput $stdoutPath `
    -RedirectStandardError $stderrPath `
    -PassThru

# Keep the control channel available even while initialization is pending.
[ordered]@{
    schema_version = 1; status = 'STARTING'; process_id = [int]$process.Id
    executable = $serverPath; shutdown_event = $shutdownEvent
    port = $Port; battle_port = $battlePort; admin_port = $adminPort; advertise_host = $AdvertiseHost
    stdout_path = $stdoutPath; stderr_path = $stderrPath
} | ConvertTo-Json | Set-Content -LiteralPath $statePath -Encoding UTF8

$deadline = [DateTime]::UtcNow.AddSeconds($ReadinessTimeoutSeconds)
$health = $null
$adminHealth = $null
while ([DateTime]::UtcNow -lt $deadline) {
    if ($null -eq (Get-Process -Id $process.Id -ErrorAction SilentlyContinue)) { break }
    try {
        $healthResponse = Invoke-WebRequest -Uri $healthURL -UseBasicParsing -TimeoutSec 2
        if ($healthResponse.StatusCode -eq 200) { $health = $healthResponse.Content | ConvertFrom-Json }
    } catch {}
    try {
        $adminResponse = Invoke-WebRequest -Uri $adminHealthURL -UseBasicParsing -TimeoutSec 2
        if ($adminResponse.StatusCode -eq 200) { $adminHealth = $adminResponse.Content | ConvertFrom-Json }
    } catch {}
    if ($null -ne $health -and [string]$health.state -eq 'PASS' -and
        $null -ne $adminHealth -and [string]$adminHealth.state -eq 'PASS') {
        break
    }
    Start-Sleep -Milliseconds 250
}

$ready = ($null -ne $health -and [string]$health.state -eq 'PASS' -and
    $null -ne $adminHealth -and [string]$adminHealth.state -eq 'PASS' -and
    $null -ne (Get-Process -Id $process.Id -ErrorAction SilentlyContinue))
if (-not $ready) {
    $exitedBeforeReady = ($null -eq (Get-Process -Id $process.Id -ErrorAction SilentlyContinue))
    if (-not $exitedBeforeReady) {
        $shutdown = [Threading.EventWaitHandle]::OpenExisting($shutdownEvent)
        try { [void]$shutdown.Set() } finally { $shutdown.Dispose() }
        [void]$process.WaitForExit(10000)
    }
    $stderr = if (Test-Path -LiteralPath $stderrPath -PathType Leaf) {
        Get-Content -LiteralPath $stderrPath -Raw -Encoding UTF8
    } else { '' }
    $stdout = if (Test-Path -LiteralPath $stdoutPath -PathType Leaf) {
        Get-Content -LiteralPath $stdoutPath -Raw -Encoding UTF8
    } else { '' }
    if ($exitedBeforeReady) {
        throw "Portable server exited before becoming ready. STDERR: $stderr STDOUT: $stdout"
    }
    throw "Portable server readiness timed out after $ReadinessTimeoutSeconds seconds. STDERR: $stderr STDOUT: $stdout"
}

$state = [ordered]@{
    schema_version = 1
    status = 'RUNNING'
    process_id = [int]$process.Id
    shutdown_event = $shutdownEvent
    executable = $serverPath
    resource_set_sha256 = $resourceSetSHA256
    executable_sha256 = (Get-FileHash -LiteralPath $serverPath -Algorithm SHA256).Hash.ToLowerInvariant()
    started_utc = [DateTime]::UtcNow.ToString('o')
    advertise_host = $AdvertiseHost
    listen_host = $ListenHost
    port = $Port
    battle_port = $battlePort
    admin_port = $adminPort
    health_url = $healthURL
    admin_url = "http://127.0.0.1:$adminPort/"
    save_path = $saveDatabasePath
    stdout_path = $stdoutPath
    stderr_path = $stderrPath
    request_log_path = $requestLogPath
    health = $health
    admin_health = $adminHealth
}
$state | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $statePath -Encoding UTF8
$state | ConvertTo-Json -Depth 6
