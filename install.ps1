# Hiroto Windows installer
# Run: powershell -ExecutionPolicy Bypass -File install.ps1
# Or: irm https://raw.githubusercontent.com/hirotomasato/hiroto/main/install.ps1 | iex

$ErrorActionPreference = "Stop"
$InstallDir = "$env:USERPROFILE\.local\bin"
$ConfigDir = "$env:USERPROFILE\.hiroto"
$SkillsDir = "$ConfigDir\skills"

Write-Host "◆ Hiroto — Windows installer" -ForegroundColor Cyan

# 1. Prerequisites
Write-Host "[1/4] Checking prerequisites..." -ForegroundColor Green
if (!(Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "  Go not found. Install from https://go.dev/dl/" -ForegroundColor Red
    exit 1
}
Write-Host "  Go: $(go version)"

# 2. Install Hiroto
Write-Host "[2/4] Installing Hiroto..." -ForegroundColor Green
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
go build -o "$InstallDir\hiroto.exe" ./cmd/hiroto
Write-Host "  Hiroto: installed to $InstallDir"

# 3. Setup config
Write-Host "[3/4] Setting up config..." -ForegroundColor Green
New-Item -ItemType Directory -Force -Path $ConfigDir, $SkillsDir, "$ConfigDir\sessions", "$ConfigDir\memory" | Out-Null

if (!(Test-Path "$ConfigDir\config.yaml")) {
    @"
model:
  base_url: http://localhost:20128/v1
  model: your-model-name
  api_key: `${HIROTO_API_KEY}

agent:
  max_turns: 40
  terminal_timeout: 180
  compression_budget_tokens: 25000
  compression_keep_turns: 6

skills:
  dirs: []

plugins:
  dirs:
    - ~/.hiroto/plugins
"@ | Out-File -FilePath "$ConfigDir\config.yaml" -Encoding utf8
    Write-Host "  Created config.yaml — edit your model and API key!"
}

# 4. Done
Write-Host "[4/4] Done!" -ForegroundColor Green
Write-Host ""
Write-Host "  Add to PATH: setx PATH `"%PATH%;$InstallDir`""
Write-Host "  Run: hiroto.exe"
Write-Host "  Config: $ConfigDir\config.yaml"
Write-Host ""
Write-Host "  Note: Windows uses cmd.exe for terminal tool, not bash."
Write-Host "  For security tools, install WSL or use Linux."