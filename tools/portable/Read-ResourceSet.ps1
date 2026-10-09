# Resource-set packages need only Windows PowerShell at runtime.
function Read-PortableResourceSet {
    param([string]$ManifestPath)
    $ManifestPath = [IO.Path]::GetFullPath($ManifestPath)
    $root = Split-Path -Parent ([IO.Path]::GetFullPath($ManifestPath))
    $manifest = Get-Content -LiteralPath $ManifestPath -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($manifest.schema_version -ne 1 -or $manifest.client_profile -ne 'cn602-bootstrap' -or
        $manifest.path_base -ne 'RESOURCE_SET_ROOT') { throw 'Unsupported resource-set identity.' }
    function Resolve-ResourcePath {
        param([string]$Name)
        if (-not $Name -or $Name.Contains('\') -or $Name.Contains(':') -or
            $Name.StartsWith('/') -or @($Name.Split('/') | Where-Object { $_ -in @('', '.', '..') }).Count) {
            throw "Unsafe resource path: $Name"
        }
        $path = Join-Path $root $Name
        if (-not $inventory.ContainsKey($path)) { throw "Missing resource path: $Name" }
        return $path
    }
    $inventory = [Collections.Generic.Dictionary[string,object]]::new([StringComparer]::OrdinalIgnoreCase)
    foreach ($item in (Get-ChildItem -LiteralPath $root -Recurse -Force)) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Linked resource path: $($item.FullName)" }
        $inventory.Add($item.FullName, $item)
    }
    $entrypoints = @{}
    $critical = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    foreach ($entry in $manifest.entrypoints.PSObject.Properties) {
        $entrypoints[$entry.Name] = Resolve-ResourcePath -Name ([string]$entry.Value)
        [void]$critical.Add([string]$entry.Value)
    }
    # Only these shipped, operator-editable banners may differ from inventory.
    # Disk names stay fixed; the server publishes content-versioned URLs.
    $editableBanners = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    foreach ($bannerName in @(
        '_local/control/server/cn602-gacha-five-star-banner.png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdfire_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdice_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdlight_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdwindy_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuixing_xddark_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_duozi_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaidai_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaifu_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaigeju_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaimo_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaina_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaiyan_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_jixiyou58_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_tianke_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_weirushou58_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_youmo10_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_youmo_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_HOMEBANNER_20180409_banner_event_wang_home1_png',
        'resources/banners/https___ma43_gdl_netease_com_web_netease_HOMEBANNER_20180409_banner_shuijing_duozi_home_png',
        'resources/banners/local_first.png',
        'resources/banners/local_first_multi.png',
        'resources/banners/local_friend.png',
        'resources/banners/local_new_year.png',
        'resources/banners/local_standard.png'
    )) { [void]$editableBanners.Add($bannerName) }
    [long]$bannerBytesDelta = 0
    $listed = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    [long]$totalBytes = 0
    foreach ($row in $manifest.files) {
        $name = [string]$row.path
        $path = Resolve-ResourcePath -Name $name
        $item = $inventory[$path]
        if (-not $listed.Add($name) -or $item.PSIsContainer -or
            [string]$row.sha256 -notmatch '^[0-9a-f]{64}$') { throw "Invalid resource inventory entry: $name" }
        $isEditableBanner = $editableBanners.Contains($name)
        if ($isEditableBanner) {
            if ($item.Length -lt 8 -or $item.Length -gt 4MB) {
                throw "Custom banner must be a PNG within four MiB: $name"
            }
            $stream = [IO.File]::OpenRead($path)
            try {
                $signature = New-Object byte[] 8
                $read = $stream.Read($signature, 0, 8)
                if ($read -ne 8 -or [BitConverter]::ToString($signature) -ne '89-50-4E-47-0D-0A-1A-0A') {
                    throw "Custom banner is not a PNG: $name"
                }
            } finally { $stream.Dispose() }
            $bannerBytesDelta += $item.Length - [long]$row.bytes
        } elseif ($item.Length -ne $row.bytes) {
            throw "Resource size mismatch: $name"
        }
        $totalBytes += $item.Length
        if ($critical.Contains($name) -and -not $isEditableBanner -and
            (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $row.sha256) {
            throw "Resource entrypoint hash mismatch: $name"
        }
    }
    foreach ($name in $critical) {
        if (-not $inventory[(Join-Path $root $name)].PSIsContainer -and -not $listed.Contains($name)) {
            throw "Unregistered resource entrypoint: $name"
        }
    }
    $actual = @($inventory.Values | Where-Object { -not $_.PSIsContainer -and $_.FullName -ne $ManifestPath })
    if ($actual.Count -ne $listed.Count -or $listed.Count -ne $manifest.summary.file_count -or
        $totalBytes -ne ([long]$manifest.summary.bytes + $bannerBytesDelta)) { throw 'Resource inventory differs from manifest. Keep backup and temporary files outside resource-set.' }
    return @{ Manifest = $manifest; Entrypoints = $entrypoints }
}
