param(
    [string]$Version = $env:CHATGPT2CODEX_VERSION,
    [string]$InstallDir = $env:CHATGPT2CODEX_INSTALL_DIR
)

$ErrorActionPreference = 'Stop'

$AppName = 'chatgpt2codex'
$Owner = 'icattlecoder'
$Repo = 'chatgpt2codex'
$ChecksumsName = "${AppName}_checksums.txt"

if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = 'latest'
}

if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\chatgpt2codex\bin'
}

$Arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    ([System.Runtime.InteropServices.Architecture]::X64) { 'amd64'; break }
    ([System.Runtime.InteropServices.Architecture]::Arm64) { 'arm64'; break }
    default { throw "Unsupported architecture: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" }
}

$AssetName = "${AppName}_windows_${Arch}.zip"
if ($Version -eq 'latest') {
    $DownloadBase = "https://github.com/$Owner/$Repo/releases/latest/download"
}
else {
    if (-not $Version.StartsWith('v')) {
        $Version = "v$Version"
    }
    $DownloadBase = "https://github.com/$Owner/$Repo/releases/download/$Version"
}

$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) "$AppName-install-$([System.Guid]::NewGuid().ToString('N'))"
$ArchivePath = Join-Path $TempDir $AssetName
$ChecksumsPath = Join-Path $TempDir $ChecksumsName
$ExtractDir = Join-Path $TempDir 'extract'
$InstallPath = Join-Path $InstallDir "$AppName.exe"
$PathUpdated = $false

New-Item -ItemType Directory -Path $TempDir -Force | Out-Null
New-Item -ItemType Directory -Path $ExtractDir -Force | Out-Null

try {
    Write-Host "Downloading $AssetName"
    Invoke-WebRequest -Uri "$DownloadBase/$AssetName" -OutFile $ArchivePath
    Invoke-WebRequest -Uri "$DownloadBase/$ChecksumsName" -OutFile $ChecksumsPath

    $ExpectedSum = (
        Get-Content $ChecksumsPath |
            Where-Object { $_ -match ([regex]::Escape($AssetName) + '$') } |
            Select-Object -First 1
    )
    if (-not $ExpectedSum) {
        throw "Failed to find checksum for $AssetName"
    }

    $ExpectedHash = $ExpectedSum.Split()[0].ToLowerInvariant()
    $ActualHash = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($ExpectedHash -ne $ActualHash) {
        throw "Checksum verification failed for $AssetName"
    }

    Expand-Archive -Path $ArchivePath -DestinationPath $ExtractDir -Force
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item -Path (Join-Path $ExtractDir "$AppName.exe") -Destination $InstallPath -Force

    $env:Path = "$InstallDir;$env:Path"
    $CurrentUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $PathEntries = @()
    if (-not [string]::IsNullOrWhiteSpace($CurrentUserPath)) {
        $PathEntries = $CurrentUserPath.Split(';', [System.StringSplitOptions]::RemoveEmptyEntries)
    }
    if ($PathEntries -notcontains $InstallDir) {
        $NewPath = if ([string]::IsNullOrWhiteSpace($CurrentUserPath)) {
            $InstallDir
        }
        else {
            "$CurrentUserPath;$InstallDir"
        }
        [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
        $PathUpdated = $true
    }

    Write-Host "Installed $AppName to $InstallPath"
    if ($PathUpdated) {
        Write-Host 'Updated PATH for the current user. Open a new terminal to use chatgpt2codex everywhere.'
    }
}
finally {
    Remove-Item -Path $TempDir -Recurse -Force -ErrorAction SilentlyContinue
}
