$ErrorActionPreference = 'Stop'
$appDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $appDir
$proc = Start-Process -FilePath "npm.cmd" -ArgumentList "start" -WorkingDirectory $appDir -WindowStyle Hidden -PassThru
$proc.WaitForExit()
