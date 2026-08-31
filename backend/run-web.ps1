$ErrorActionPreference = 'Stop'
# Bindagem web blindada — TLS + CORS estrito + rate limit + headers
# Uso: powershell -ExecutionPolicy Bypass -File backend\run-web.ps1
# Requer: Go 1.21+, Postgres artigos_ana, certs/server.crt+key (gerado via gencert.go)

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# carrega .env.web se existir, senão usa defaults
$envFile = Join-Path $root ".env.web"
if (Test-Path $envFile) {
  Get-Content $envFile | ForEach-Object {
    if ($_ -match '^\s*#' -or $_ -match '^\s*$') { return }
    $kv = $_ -split '=',2
    if ($kv.Count -eq 2) {
      $k = $kv[0].Trim()
      $v = $kv[1].Trim()
      if ($k -and $v) { Set-Item -Path "env:$k" -Value $v }
    }
  }
  Write-Host "carregado $envFile"
} else {
  Write-Host "aviso: .env.web não encontrado, usando .env.web.example como base"
  if (Test-Path (Join-Path $root ".env.web.example")) {
    Copy-Item (Join-Path $root ".env.web.example") $envFile
    Write-Host "copiado .env.web.example -> .env.web (edite JWT_SECRET e PGPASSWORD)"
  }
}

# garante JWT_SECRET 32+ bytes
if (-not $env:JWT_SECRET -or $env:JWT_SECRET.Length -lt 32) {
  $rand = -join ((48..57)+(65..90)+(97..122) | Get-Random -Count 48 | ForEach-Object { [char]$_ })
  $env:JWT_SECRET = $rand
  Write-Host "gerado JWT_SECRET aleatório (defina um fixo em .env.web para sessões persistirem)"
}

# garante ALLOWED_ORIGIN em bind 0.0.0.0
if (-not $env:ALLOWED_ORIGIN) {
  $lan = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -like "192.168.*" } | Select-Object -First 1).IPAddress
  if (-not $lan) { $lan = "192.168.0.19" }
  $env:ALLOWED_ORIGIN = "https://localhost:8734,https://127.0.0.1:8734,https://$lan`:8734"
  Write-Host "ALLOWED_ORIGIN auto: $env:ALLOWED_ORIGIN"
}
$env:STRICT_CORS = "1"
$env:BIND_ADDR = "0.0.0.0"
if (-not $env:PORT) { $env:PORT = "8734" }

# verifica certs
$cert = Join-Path $root "certs\server.crt"
$key  = Join-Path $root "certs\server.key"
if (-not (Test-Path $cert) -or -not (Test-Path $key)) {
  Write-Host "gerando cert self-signed em certs/..."
  $gencert = Join-Path $env:TEMP "gencert.go"
  if (-not (Test-Path $gencert)) {
    Write-Host "erro: gencert.go não encontrado em TEMP, gere manualmente com: go run backend\cmd\gencert"
    exit 1
  }
  go run $gencert
}
$env:TLS_CERT = $cert
$env:TLS_KEY  = $key

# build
$exe = Join-Path $root "bin\artigos-ana.exe"
Write-Host "building $exe..."
go build -o $exe . 2>&1 | Write-Host
if (-not (Test-Path $exe)) { Write-Host "falha no build"; exit 1 }

Write-Host ""
Write-Host "=== BINDAGEM WEB BLINDADA ===" -ForegroundColor Green
Write-Host " Bind: 0.0.0.0:$env:PORT (https)"
Write-Host " TLS: $cert"
Write-Host " CORS: $env:ALLOWED_ORIGIN"
Write-Host " JWT: $($env:JWT_SECRET.Length) bytes"
Write-Host " DB: $env:PGHOST/$env:PGDATABASE"
Write-Host ""
Write-Host "URLs:"
$lanIp = ($env:ALLOWED_ORIGIN -split ",") | Where-Object { $_ -like "https://192.168.*" } | Select-Object -First 1
if ($lanIp) { Write-Host "  $lanIp" }
Write-Host "  https://localhost:$env:PORT"
Write-Host ""
Write-Host "Para confiar no cert self-signed (evitar NET::ERR_CERT_AUTHORITY):"
Write-Host "  Windows: certmgr.msc -> Importar $cert em 'Autoridades de Certificação Raiz Confíeis'"
Write-Host "  Ou rode Chrome com --ignore-certificate-errors (só dev)"
Write-Host ""

& $exe -bind=0.0.0.0 -port=$env:PORT
