$ErrorActionPreference = 'Stop'
$appDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $appDir
# Banco unico: carrega .env.local da raiz (DATABASE_URL do Neon) para o Electron/backend herdarem.
# Sem isso o backend local usa o Postgres localhost e diverge da producao (Render).
$envLocal = Join-Path (Split-Path -Parent $appDir) ".env.local"
if (Test-Path -LiteralPath $envLocal) {
  Get-Content -LiteralPath $envLocal | ForEach-Object {
    if ($_ -match '^\s*#' -or $_ -match '^\s*$') { return }
    $kv = $_ -split '=', 2
    if ($kv.Count -eq 2) {
      $k = $kv[0].Trim()
      $v = $kv[1].Trim().Trim('"').Trim("'")
      if ($k -and $v -and -not (Test-Path "env:$k")) { Set-Item -Path "env:$k" -Value $v }
    }
  }
  Write-Host "carregado $envLocal (banco unico Neon)"
} else {
  Write-Host "aviso: .env.local nao encontrado, backend local usara Postgres localhost"
}
$proc = Start-Process -FilePath "npm.cmd" -ArgumentList "start" -WorkingDirectory $appDir -WindowStyle Hidden -PassThru
$proc.WaitForExit()
