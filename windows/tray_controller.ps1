Add-Type -AssemblyName System.Windows.Forms,System.Drawing

$repoDir = Split-Path -Parent $PSScriptRoot
$botPy = Join-Path $repoDir "bot.py"
$pythonw = (Get-Command pythonw.exe -ErrorAction SilentlyContinue).Source
if (-not $pythonw) {
    $pythonw = "$env:LOCALAPPDATA\Programs\Python\Python311\pythonw.exe"
}

function Get-BotProcess {
    Get-CimInstance Win32_Process -Filter "Name = 'pythonw.exe' OR Name = 'python.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -like "*bot.py*" }
}

function New-StatusIcon([System.Drawing.Color]$color) {
    $bmp = New-Object System.Drawing.Bitmap 16, 16
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $brush = New-Object System.Drawing.SolidBrush $color
    $g.FillEllipse($brush, 2, 2, 12, 12)
    $g.Dispose()
    $brush.Dispose()
    return [System.Drawing.Icon]::FromHandle($bmp.GetHicon())
}

$iconOn = New-StatusIcon ([System.Drawing.Color]::FromArgb(46, 204, 113))
$iconOff = New-StatusIcon ([System.Drawing.Color]::FromArgb(149, 165, 166))

$tray = New-Object System.Windows.Forms.NotifyIcon
$tray.Visible = $true

$menu = New-Object System.Windows.Forms.ContextMenuStrip
$statusItem = $menu.Items.Add("Estado: Verificando...")
$statusItem.Enabled = $false
$menu.Items.Add("-") | Out-Null
$toggleItem = $menu.Items.Add("Encender / Apagar Bot")
$restartItem = $menu.Items.Add("Reiniciar Bot")
$menu.Items.Add("-") | Out-Null
$exitItem = $menu.Items.Add("Salir del Icono")
$tray.ContextMenuStrip = $menu

function Update-TrayState {
    $procs = @(Get-BotProcess)
    if ($procs.Count -gt 0) {
        $pidVal = $procs[0].ProcessId
        $procObj = Get-Process -Id $pidVal -ErrorAction SilentlyContinue
        $ramMb = if ($procObj) { [math]::Round($procObj.WorkingSet64 / 1MB, 1) } else { 0 }
        $tray.Icon = $iconOn
        $tray.Text = "Telegram AGY Bot: ACTIVO (${ramMb} MB)"
        $statusItem.Text = "Activo (PID $pidVal - ${ramMb} MB)"
        $toggleItem.Text = "Apagar Bot"
    } else {
        $tray.Icon = $iconOff
        $tray.Text = "Telegram AGY Bot: APAGADO"
        $statusItem.Text = "Apagado"
        $toggleItem.Text = "Encender Bot"
    }
}

function Start-Bot {
    if (-not (Get-BotProcess)) {
        Start-Process -FilePath $pythonw -ArgumentList "`"$botPy`"" -WorkingDirectory $repoDir -WindowStyle Hidden
        Start-Sleep -Milliseconds 700
        Update-TrayState
        $tray.ShowBalloonTip(2500, "Telegram AGY Bot", "Bot encendido y conectado en segundo plano.", [System.Windows.Forms.ToolTipIcon]::Info)
    }
}

function Stop-Bot {
    $procs = @(Get-BotProcess)
    foreach ($p in $procs) {
        Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Milliseconds 400
    Update-TrayState
    $tray.ShowBalloonTip(2500, "Telegram AGY Bot", "Bot detenido.", [System.Windows.Forms.ToolTipIcon]::Warning)
}

$toggleItem.Add_Click({
    if (Get-BotProcess) { Stop-Bot } else { Start-Bot }
})

$restartItem.Add_Click({
    Stop-Bot
    Start-Bot
})

$exitItem.Add_Click({
    $tray.Visible = $false
    $tray.Dispose()
    [System.Windows.Forms.Application]::Exit()
})

$tray.Add_DoubleClick({
    if (Get-BotProcess) { Stop-Bot } else { Start-Bot }
})

$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 4000
$timer.Add_Tick({ Update-TrayState })
$timer.Start()

if (-not (Get-BotProcess)) {
    Start-Bot
} else {
    Update-TrayState
}

[System.Windows.Forms.Application]::Run()
