Add-Type -AssemblyName System.Windows.Forms,System.Drawing

$repoDir = Split-Path -Parent $PSScriptRoot
$botPy = Join-Path $repoDir "bot.py"
$statsPy = Join-Path $repoDir "scripts\ai-token-stats"
$pythonw = (Get-Command pythonw.exe -ErrorAction SilentlyContinue).Source
if (-not $pythonw) {
    $pythonw = "$env:LOCALAPPDATA\Programs\Python\Python311\pythonw.exe"
}
$pythonExe = $pythonw -replace "pythonw\.exe$", "python.exe"

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

$quotaHeader = $menu.Items.Add("📊 Cuotas Antigravity (AGY)")
$quotaHeader.Enabled = $false
$qGem5h = $menu.Items.Add("  • Gemini 5h: --")
$qGem5h.Enabled = $false
$qGemWk = $menu.Items.Add("  • Gemini Semanal: --")
$qGemWk.Enabled = $false
$qCl5h = $menu.Items.Add("  • Claude/GPT 5h: --")
$qCl5h.Enabled = $false
$qClWk = $menu.Items.Add("  • Claude/GPT Semanal: --")
$qClWk.Enabled = $false
$qArgWk = $menu.Items.Add("  • Argon Semanal: --")
$qArgWk.Enabled = $false
$refreshQuotasItem = $menu.Items.Add("🔄 Actualizar Cuotas de Tokens")

$menu.Items.Add("-") | Out-Null
$toggleItem = $menu.Items.Add("Encender / Apagar Bot")
$restartItem = $menu.Items.Add("Reiniciar Bot")
$menu.Items.Add("-") | Out-Null
$exitItem = $menu.Items.Add("Salir del Icono")
$tray.ContextMenuStrip = $menu

$script:lastGem5h = "--"
$script:lastArgWk = "--"

function Apply-QuotaJson([string]$rawJson) {
    if (-not $rawJson) { return }
    try {
        $data = $rawJson | ConvertFrom-Json
        $ag = $data.antigravity
        if ($ag -and $ag.available) {
            $g5 = $ag.gemini.five_hour_remaining_pct
            $gw = $ag.gemini.week_remaining_pct
            $c5 = $ag.claude_gpt.five_hour_remaining_pct
            $cw = $ag.claude_gpt.week_remaining_pct
            $aw = $ag.argon.week_remaining_pct

            $qGem5h.Text = "  • Gemini 5h: ${g5}% libre"
            $qGemWk.Text = "  • Gemini Semanal: ${gw}% libre"
            $qCl5h.Text  = "  • Claude/GPT 5h: ${c5}% libre"
            $qClWk.Text  = "  • Claude/GPT Semanal: ${cw}% libre"
            $qArgWk.Text = "  • Argon Semanal: ${aw}% libre"

            $script:lastGem5h = "${g5}%"
            $script:lastArgWk = "${aw}%"
        }
    } catch {}
}

function Load-CachedQuotas {
    $cacheFile = Join-Path $env:USERPROFILE ".gemini\antigravity-cli\ai_quotas_cache.json"
    if (Test-Path $cacheFile) {
        try {
            $raw = Get-Content $cacheFile -Raw -Encoding UTF8
            Apply-QuotaJson $raw
        } catch {}
    }
}

function Refresh-QuotasLive([bool]$showToast = $false) {
    $refreshQuotasItem.Text = "⏳ Sincronizando cuotas..."
    try {
        $psi = New-Object System.Diagnostics.ProcessStartInfo
        $psi.FileName = $pythonExe
        $psi.Arguments = "`"$statsPy`""
        $psi.WorkingDirectory = $repoDir
        $psi.RedirectStandardOutput = $true
        $psi.UseShellExecute = $false
        $psi.CreateNoWindow = $true
        $p = [System.Diagnostics.Process]::Start($psi)
        $out = $p.StandardOutput.ReadToEnd()
        $p.WaitForExit(15000) | Out-Null
        Apply-QuotaJson $out
        Update-TrayState
        if ($showToast) {
            $msg = "Gemini 5h: $script:lastGem5h libre | Argon Sem: $script:lastArgWk libre"
            $tray.ShowBalloonTip(3500, "Cuotas de Tokens AGY", $msg, [System.Windows.Forms.ToolTipIcon]::Info)
        }
    } catch {}
    $refreshQuotasItem.Text = "🔄 Actualizar Cuotas de Tokens"
}

function Update-TrayState {
    $procs = @(Get-BotProcess)
    if ($procs.Count -gt 0) {
        $pidVal = $procs[0].ProcessId
        $procObj = Get-Process -Id $pidVal -ErrorAction SilentlyContinue
        $ramMb = if ($procObj) { [math]::Round($procObj.WorkingSet64 / 1MB, 1) } else { 0 }
        $tray.Icon = $iconOn
        $tip = "Bot: ACTIVO (${ramMb} MB) | Gem: $script:lastGem5h | Argon: $script:lastArgWk"
        if ($tip.Length -gt 63) { $tip = $tip.Substring(0, 63) }
        $tray.Text = $tip
        $statusItem.Text = "🟢 Bot Activo (PID $pidVal • ${ramMb} MB)"
        $toggleItem.Text = "🛑 Apagar Bot"
    } else {
        $tray.Icon = $iconOff
        $tip = "Bot: APAGADO | Gem: $script:lastGem5h | Argon: $script:lastArgWk"
        if ($tip.Length -gt 63) { $tip = $tip.Substring(0, 63) }
        $tray.Text = $tip
        $statusItem.Text = "⚪ Bot Apagado"
        $toggleItem.Text = "🟢 Encender Bot"
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

$refreshQuotasItem.Add_Click({
    Refresh-QuotasLive $true
})

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
    Refresh-QuotasLive $true
})

$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 4000
$timer.Add_Tick({
    Load-CachedQuotas
    Update-TrayState
})
$timer.Start()

Load-CachedQuotas
if (-not (Get-BotProcess)) {
    Start-Bot
} else {
    Update-TrayState
}
Refresh-QuotasLive $false

[System.Windows.Forms.Application]::Run()
