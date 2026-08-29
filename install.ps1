#requires -RunAsAdministrator
[CmdletBinding()]
param(
  [string]$ApiKey = $env:PULSE_API_KEY,
  [string]$BaseUrl = "https://pulse.nimweo.dev/api/v1/",
  [ValidatePattern('^latest$|^v?\d+\.\d+\.\d+$')]
  [string]$Version = "latest",
  [string]$InstallDirectory = "$env:ProgramFiles\Nimweo\Pulse Agent",
  [string]$ConfigPath = "$env:ProgramData\Nimweo\Pulse Agent\config.yaml",
  [switch]$Force
)

$ErrorActionPreference = "Stop"
$repository = "Nimweo/pulse-agent"
$serviceName = "PulseAgent"
$architecture = if ([Environment]::Is64BitOperatingSystem -and $env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$temporary = Join-Path ([IO.Path]::GetTempPath()) ("pulse-agent-install-" + [guid]::NewGuid())

function Fail([string]$Message) { throw "Pulse Agent installation failed: $Message" }
function Download([string]$Url, [string]$Path) {
  Invoke-WebRequest -Uri $Url -OutFile $Path -UseBasicParsing
}

try {
  New-Item -ItemType Directory -Path $temporary -Force | Out-Null
  if ($Version -eq "latest") {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repository/releases/latest" -Headers @{ Accept = "application/vnd.github+json" }
    $tag = $release.tag_name
  } else { $tag = "v$($Version.TrimStart('v'))" }
  if ($tag -notmatch '^v\d+\.\d+\.\d+$') { Fail "invalid release tag: $tag" }
  $releaseVersion = $tag.TrimStart('v')
  $package = "pulse-agent_${releaseVersion}_windows_${architecture}"
  $archive = "$package.zip"
  $releaseUrl = "https://github.com/$repository/releases/download/$tag"
  $archivePath = Join-Path $temporary $archive
  $checksumsPath = Join-Path $temporary "checksums.txt"

  Write-Host "Downloading Pulse Agent $releaseVersion for windows/$architecture..."
  Download "$releaseUrl/$archive" $archivePath
  Download "$releaseUrl/checksums.txt" $checksumsPath
  $checksumLine = Get-Content $checksumsPath | Where-Object { $_ -match "\s$([regex]::Escape($archive))$" } | Select-Object -First 1
  if (-not $checksumLine) { Fail "checksum not found for $archive" }
  $expected = ($checksumLine -split '\s+')[0].ToLowerInvariant()
  $actual = (Get-FileHash $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($expected -ne $actual) { Fail "checksum verification failed for $archive" }

  $extract = Join-Path $temporary "package"
  Expand-Archive -Path $archivePath -DestinationPath $extract -Force
  $binarySource = Join-Path $extract "$package\pulse-agent.exe"
  $configSource = Join-Path $extract "$package\config.example.yaml"
  if (-not (Test-Path $binarySource)) { Fail "release archive does not contain pulse-agent.exe" }
  if (-not (Test-Path $configSource)) { Fail "release archive does not contain config.example.yaml" }

  New-Item -ItemType Directory -Path $InstallDirectory -Force | Out-Null
  New-Item -ItemType Directory -Path (Split-Path $ConfigPath) -Force | Out-Null
  $binaryPath = Join-Path $InstallDirectory "pulse-agent.exe"
  if ((Test-Path $binaryPath) -and -not $Force) {
    Stop-Service -Name $serviceName -ErrorAction SilentlyContinue
  }
  Copy-Item $binarySource $binaryPath -Force
  if (-not (Test-Path $ConfigPath)) { Copy-Item $configSource $ConfigPath }
  $config = Get-Content $ConfigPath -Raw
  $config = $config -replace '(?m)^configured:\s*false\s*$', 'configured: false'
  if ($ApiKey) { $config = $config -replace '(?m)^\s*api_key:\s*.*$', "  api_key: `"$ApiKey`"" }
  if ($BaseUrl) { $config = $config -replace '(?m)^\s*base_url:\s*.*$', "  base_url: `"$BaseUrl`"" }
  Set-Content -Path $ConfigPath -Value $config -Encoding UTF8

  $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
  $binaryArguments = "`"$binaryPath`" --config `"$ConfigPath`""
  if ($service) {
    Stop-Service -Name $serviceName -ErrorAction SilentlyContinue
    & sc.exe config $serviceName binPath= $binaryArguments | Out-Null
  } else {
    New-Service -Name $serviceName -DisplayName "Pulse Agent" -Description "Pulse Agent system metrics collector" -BinaryPathName $binaryArguments -StartupType Automatic | Out-Null
  }
  & sc.exe failure $serviceName reset= 86400 actions= restart/5000/restart/30000/none/0 | Out-Null
  Start-Service -Name $serviceName
  Write-Host "Pulse Agent $releaseVersion installed successfully."
  Write-Host "Configuration: $ConfigPath"
  Write-Host "Service status: Get-Service $serviceName"
  Write-Host "Edit configured: true after reviewing the configuration."
} finally {
  if (Test-Path $temporary) { Remove-Item $temporary -Recurse -Force -ErrorAction SilentlyContinue }
}
