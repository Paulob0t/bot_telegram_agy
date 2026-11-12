$vbsPath = Join-Path $PSScriptRoot "iniciar_tray.vbs"
$regPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
Set-ItemProperty -Path $regPath -Name "TelegramAGYBot" -Value "wscript.exe `"$vbsPath`""
Write-Host "Auto-inicio con System Tray registrado en Windows Run: $vbsPath"
