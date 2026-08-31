$ErrorActionPreference = 'Stop'
# Bindagem via GROK https tunnel (sem TLS local, mas com https externo)
# Uso: 1) rode grok http 8734 --url https://abc.grok.io  2) powershell -File run-grok.ps1 -GrokOrigin https://abc.grok.io

param([string]$GrokOrigin = "")

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

if (-not $GrokOrigin -and $env:GROK_ORIGIN) { $GrokOrigin = $env:GROK_ORIGIN }
if (-not $GrokOrigin) {
  Write-Host "informe GROK_ORIGIN: .\run-grok.ps1 -GrokOrigin https://abc.grok.io"
  Write-Host "ou defina env GROK_ORIGIN e rode novamente"
  exit 1
}

# carrega .env.web se existir
$envFile = Join-Path $root ".env.web"
if (Test-Path $envFile) {
  Get-Content $envFile | ForEach-Object {
    if ($_ -match '^\s*#' -or $_ -match '^\s*$') { return }
    $kv = $_ -split '=',2
    if ($kv.Count -eq 2) { Set-Item -Path "env:$($kv[0].Trim())" -Value $kv[1].Trim() }
  }
}
if (-not $env:JWT_SECRET -or $env:JWT_SECRET.Length -lt 32) {
  $env:JWT_SECRET = -join ((48..57)+(65..90)+(97..122) | Get-Random -Count 48 | ForEach-Object { [char]$_ })
  Write-Host "gerado JWT_SECRET temporário"
}
$env:GROK_ORIGIN = $GrokOrigin
$env:ALLOWED_ORIGIN = "$GrokOrigin,https://localhost:8734,https://127.0.0.1:8734"
$env:STRICT_CORS = "1"
$env:BIND_ADDR = "0.0.0.0"
$env:PORT = "8734"
# sem TLS local, GROK faz TLS terminando em https -> backend vê X-Forwarded-Proto https
Remove-Item env:TLS_CERT -ErrorAction SilentlyContinue
Remove-Item env:TLS_KEY -ErrorAction SilentlyContinue

$exe = Join-Path $root "bin\artigos-ana.exe"
Write-Host "building $exe..."
go build -o $exe . | Write-Host

Write-Host ""
Write-Host "=== BINDAGEM GROK ===" -ForegroundColor Green
Write-Host " GROK: $GrokOrigin -> http://127.0.0.1:8734"
Write-Host " CORS: $env:ALLOWED_ORIGIN"
Write-Host " Bind: 0.0.0.0:8734 (http atrás do GROK https)"
Write-Host ""

& $exe -bind=0.0.0.0 -port=8734
