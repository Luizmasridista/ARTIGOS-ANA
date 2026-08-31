$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$popplerDir = Join-Path $scriptDir "poppler"

Write-Host "Consultando último release de oschwartz10612/poppler-windows..."
$release = Invoke-RestMethod -Uri "https://api.github.com/repos/oschwartz10612/poppler-windows/releases/latest" -Headers @{ "User-Agent" = "artigos-ana-backend" }
$asset = $release.assets | Where-Object { $_.name -like "*.zip" -and $_.name -notlike "*.sha256*" } | Select-Object -First 1
if (-not $asset) {
    throw "Nenhum asset zip encontrado no release mais recente."
}

$zipPath = Join-Path $env:TEMP $asset.name
Write-Host "Baixando $($asset.name) ($([math]::Round($asset.size / 1MB, 1)) MB)..."
Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zipPath

$extractDir = Join-Path $env:TEMP ("poppler-extract-" + [guid]::NewGuid().ToString("N"))
Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force

$pdftoppm = Get-ChildItem -Path $extractDir -Recurse -Filter "pdftoppm.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $pdftoppm) {
    throw "pdftoppm.exe não encontrado dentro do zip."
}
$srcBin = $pdftoppm.DirectoryName

New-Item -ItemType Directory -Path $popplerDir -Force | Out-Null
Copy-Item -Path (Join-Path $srcBin "*") -Destination $popplerDir -Force -Recurse

Remove-Item -Path $zipPath -Force -ErrorAction SilentlyContinue
Remove-Item -Path $extractDir -Recurse -Force -ErrorAction SilentlyContinue

$exes = Get-ChildItem -Path $popplerDir -Filter "*.exe"
Write-Host "Poppler instalado em $popplerDir ($($exes.Count) executáveis)."
