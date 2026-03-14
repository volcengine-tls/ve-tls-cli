param(
  [string]$BaseUrl = "https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download",
  [string]$InstallDir = "$env:LOCALAPPDATA\Programs\tlsctl"
)

$ErrorActionPreference = "Stop"

function Get-Arch {
  $arch = $env:PROCESSOR_ARCHITECTURE
  if ($arch -eq "AMD64") { return "amd64" }
  if ($arch -eq "ARM64") { return "arm64" }
  return "amd64"
}

$arch = Get-Arch
$pkg = "tlsctl_windows_$arch.zip"
$url = "$BaseUrl/$pkg"
$shaUrl = "$url.sha256"

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$tmp = New-Item -ItemType Directory -Force -Path (Join-Path $env:TEMP ("tlsctl-install-" + [Guid]::NewGuid().ToString()))
try {
  $zipPath = Join-Path $tmp.FullName $pkg
  Invoke-WebRequest -Uri $url -OutFile $zipPath

  try {
    $shaPath = Join-Path $tmp.FullName ($pkg + ".sha256")
    Invoke-WebRequest -Uri $shaUrl -OutFile $shaPath
    $expected = (Get-Content $shaPath -Raw).Split(" ", [System.StringSplitOptions]::RemoveEmptyEntries)[0].Trim()
    $actual = (Get-FileHash -Path $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($expected.ToLowerInvariant() -ne $actual) {
      throw "sha256 mismatch"
    }
  } catch {
  }

  Expand-Archive -Path $zipPath -DestinationPath $tmp.FullName -Force
  $exePath = Join-Path $tmp.FullName "tlsctl.exe"
  if (-not (Test-Path $exePath)) {
    throw "tlsctl.exe not found in package"
  }
  Copy-Item -Force -Path $exePath -Destination (Join-Path $InstallDir "tlsctl.exe")

  Write-Output ("installed: " + (Join-Path $InstallDir "tlsctl.exe"))
  & (Join-Path $InstallDir "tlsctl.exe") --version
  Write-Output ""
  Write-Output "Add to PATH (optional):"
  Write-Output "  setx PATH `"$InstallDir;$env:PATH`""
} finally {
  Remove-Item -Recurse -Force -Path $tmp.FullName
}

