$repoDir = Split-Path -Parent $PSScriptRoot
$vbsPath = Join-Path $PSScriptRoot "iniciar_bot_oculto.vbs"
$regPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
Set-ItemProperty -Path $regPath -Name "TelegramAGYBot" -Value "wscript.exe `"$vbsPath`""
Write-Host "Auto-inicio registrado en Windows Run: $vbsPath"
