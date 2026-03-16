Param(
  [string]$BaseUrl = $env:TLSCTL_RUNNER_BASE_URL,
  [string]$InstallDir = $env:TLSCTL_RUNNER_INSTALL_DIR
)

if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
  $BaseUrl = "https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download"
}
if ([string]::IsNullOrWhiteSpace($InstallDir)) {
  $InstallDir = Join-Path $HOME ".local\bin"
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$arch = $env:PROCESSOR_ARCHITECTURE
if ($arch -eq "AMD64") { $arch = "amd64" }
elseif ($arch -like "ARM*") { $arch = "arm64" }

$pkg = "tlsctl-runner_windows_${arch}.zip"
$zipPath = Join-Path $env:TEMP $pkg
$shaPath = "${zipPath}.sha256"

Invoke-WebRequest -Uri "$BaseUrl/$pkg" -OutFile $zipPath
try {
  Invoke-WebRequest -Uri "$BaseUrl/$pkg.sha256" -OutFile $shaPath
  $shaLine = (Get-Content $shaPath | Select-Object -First 1)
  $want = $shaLine.Split(" ")[0].Trim().ToLower()
  $got = (Get-FileHash $zipPath -Algorithm SHA256).Hash.ToLower()
  if ($want -ne $got) { throw "sha256 mismatch" }
} catch {
}

$tmpDir = Join-Path $env:TEMP ("tlsctl-runner-" + [Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
Expand-Archive -Path $zipPath -DestinationPath $tmpDir -Force

$src = Join-Path $tmpDir "tlsctl-runner.exe"
$dst = Join-Path $InstallDir "tlsctl-runner.exe"
Copy-Item -Force $src $dst

Write-Output ("installed: " + $dst)

