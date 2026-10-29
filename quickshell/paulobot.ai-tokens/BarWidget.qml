import QtQuick
import QtQuick.Layouts
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

BarWidget {
  id: root
  moduleName: "paulobot.ai-tokens"

  property bool popupOpen: false
  property bool loading: false

  // Claude Code (suscripción Anthropic) — porcentaje USADO
  property real claudeSessionUsed: 0
  property real claudeWeekUsed: 0
  property string claudeSessionReset: ""
  property string claudeWeekReset: ""

  // Antigravity (Google) — porcentaje RESTANTE
  property real agGemini5h: 0
  property real agGeminiWeek: 0
  property real agClaude5h: 0
  property real agClaudeWeek: 0
  property real agArgonWeek: 0
  property string agGemini5hReset: ""
  property string agGeminiWeekReset: ""
  property string agClaude5hReset: ""
  property string agClaudeWeekReset: ""
  property string agArgonWeekReset: ""

  readonly property color claudeColor: "#d97757"
  readonly property color agColor: "#7aa2f7"
  readonly property color argonColor: "#bb9af7"
  readonly property color dangerColor: "#f7768e"

  function close() { popupOpen = false }
  function open() {
    popupOpen = true
    root.refresh()
  }
  function toggle() {
    popupOpen = !popupOpen
    if (popupOpen) root.refresh()
  }

  function refresh() {
    if (!statsProcess.running) {
      root.loading = true
      statsProcess.running = true
    }
  }

  // ISO UTC -> "jue 2:26 PM" local
  function fmtIso(iso) {
    if (!iso) return ""
    var d = new Date(iso)
    if (isNaN(d.getTime())) return iso
    return Qt.formatDateTime(d, "ddd h:mm AP")
  }

  // "Oct 22, 2pm (America/Mexico_City)" -> "Oct 22, 2pm"
  function fmtClaude(s) {
    return String(s || "").replace(/\s*\(.*\)\s*$/, "")
  }

  Process {
    id: statsProcess
    running: false
    command: ["/home/paulobot/.local/bin/ai-token-stats"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        root.loading = false
        try {
          var res = JSON.parse(text || "{}")
          var c = res.claude_code
          if (c && c.available) {
            root.claudeSessionUsed = Number(c.session_used_pct)
            root.claudeWeekUsed = Number(c.week_used_pct)
            root.claudeSessionReset = root.fmtClaude(c.session_resets)
            root.claudeWeekReset = root.fmtClaude(c.week_resets)
          }
          var ag = res.antigravity
          if (ag && ag.available) {
            if (ag.gemini) {
              root.agGemini5h = Number(ag.gemini.five_hour_remaining_pct)
              root.agGeminiWeek = Number(ag.gemini.week_remaining_pct)
              root.agGemini5hReset = root.fmtIso(ag.gemini.five_hour_reset)
              root.agGeminiWeekReset = root.fmtIso(ag.gemini.week_reset)
            }
            if (ag.claude_gpt) {
              root.agClaude5h = Number(ag.claude_gpt.five_hour_remaining_pct)
              root.agClaudeWeek = Number(ag.claude_gpt.week_remaining_pct)
              root.agClaude5hReset = root.fmtIso(ag.claude_gpt.five_hour_reset)
              root.agClaudeWeekReset = root.fmtIso(ag.claude_gpt.week_reset)
            }
            if (ag.argon) {
              root.agArgonWeek = Number(ag.argon.week_remaining_pct)
              root.agArgonWeekReset = root.fmtIso(ag.argon.week_reset)
            }
          }
        } catch (e) {}
      }
    }
  }

  // On-demand only: consulta /usage únicamente al abrir el popup o pulsar 󰑐

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: "󰧑"
    foreground: root.popupOpen ? (Color.accent || root.agColor) : (bar ? bar.barForeground : Color.foreground)
    active: root.popupOpen
    tooltipText: "Cuotas AI"
    onPressed: function(btn) { root.toggle() }
  }

  // ── Fila de cuota: etiqueta · valor · barra fina · reinicio ──
  component QuotaRow: ColumnLayout {
    id: row
    property string label: ""
    property real value: 0          // porcentaje tal cual lo reporta el proveedor
    property bool isUsed: false     // true = "usado", false = "restante"
    property string reset: ""
    property color accent: root.agColor

    readonly property real fill: Math.max(0, Math.min(100, value)) / 100.0
    readonly property bool critical: isUsed ? value >= 85 : value <= 10
    readonly property color barColor: critical ? root.dangerColor : accent

    Layout.fillWidth: true
    spacing: 5

    RowLayout {
      Layout.fillWidth: true
      spacing: 8

      Text {
        text: row.label
        color: Color.foreground
        opacity: 0.85
        font.family: root.bar ? root.bar.fontFamily : "sans-serif"
        font.pixelSize: 12
        Layout.fillWidth: true
        elide: Text.ElideRight
      }

      Text {
        text: row.value + "%"
        color: row.critical ? root.dangerColor : Color.foreground
        font.family: root.bar ? root.bar.fontFamily : "monospace"
        font.pixelSize: 12
        font.weight: Font.DemiBold
      }

      Text {
        text: row.isUsed ? "usado" : "libre"
        color: "#ffffff"
        opacity: 0.8
        font.family: root.bar ? root.bar.fontFamily : "sans-serif"
        font.pixelSize: 10
      }
    }

    // Barra de 3px
    Item {
      Layout.fillWidth: true
      implicitHeight: 3

      Rectangle {
        anchors.fill: parent
        radius: 1.5
        color: Color.foreground
        opacity: 0.08
      }

      Rectangle {
        width: parent.width * row.fill
        height: parent.height
        radius: 1.5
        color: row.barColor
        Behavior on width { NumberAnimation { duration: 400; easing.type: Easing.OutCubic } }
      }
    }

    Text {
      visible: row.reset !== ""
      text: "Reinicia " + row.reset
      color: "#ffffff"
      opacity: 0.8
      font.family: root.bar ? root.bar.fontFamily : "sans-serif"
      font.pixelSize: 10
    }
  }

  // ── Encabezado de sección ──
  component SectionHeader: RowLayout {
    id: sh
    property string title: ""
    property string icon: ""
    property color accent: root.agColor

    Layout.fillWidth: true
    spacing: 8

    Rectangle {
      width: 6
      height: 6
      radius: 3
      color: sh.accent
    }

    Text {
      text: sh.title.toUpperCase()
      color: "#ffffff"
      opacity: 0.95
      font.family: root.bar ? root.bar.fontFamily : "sans-serif"
      font.pixelSize: 10
      font.weight: Font.DemiBold
      font.letterSpacing: 1.2
      Layout.fillWidth: true
    }
  }

  component Divider: Rectangle {
    Layout.fillWidth: true
    implicitHeight: 1
    color: Color.foreground
    opacity: 0.07
  }

  PopupCard {
    id: popup
    anchorItem: root
    bar: root.bar
    owner: root
    open: root.popupOpen
    contentWidth: popup.fittedContentWidth(340)
    contentHeight: popup.fittedContentHeight(content.implicitHeight)
    onClosed: root.close()

    ColumnLayout {
      id: content
      anchors.fill: parent
      spacing: 14

      // Título
      RowLayout {
        Layout.fillWidth: true
        spacing: 8

        Text {
          text: "Uso de IA"
          color: Color.foreground
          font.family: root.bar ? root.bar.fontFamily : "sans-serif"
          font.pixelSize: 15
          font.weight: Font.DemiBold
          Layout.fillWidth: true
        }

        Text {
          text: root.loading ? "sincronizando…" : "󰑐"
          color: Color.subtext
          font.family: root.bar ? root.bar.fontFamily : "monospace"
          font.pixelSize: root.loading ? 10 : 13

          MouseArea {
            anchors.fill: parent
            anchors.margins: -6
            cursorShape: Qt.PointingHandCursor
            onClicked: root.refresh()
          }
        }
      }

      // ── Claude Code ──
      SectionHeader { title: "Claude Code"; accent: root.claudeColor }

      QuotaRow {
        label: "Sesión · 5 h"
        value: root.claudeSessionUsed
        isUsed: true
        reset: root.claudeSessionReset
        accent: root.claudeColor
      }

      QuotaRow {
        label: "Semanal"
        value: root.claudeWeekUsed
        isUsed: true
        reset: root.claudeWeekReset
        accent: root.claudeColor
      }

      Divider {}

      // ── Antigravity ──
      SectionHeader { title: "Antigravity"; accent: root.agColor }

      QuotaRow {
        label: "Gemini · 5 h"
        value: root.agGemini5h
        reset: root.agGemini5hReset
        accent: root.agColor
      }

      QuotaRow {
        label: "Gemini · Semanal"
        value: root.agGeminiWeek
        reset: root.agGeminiWeekReset
        accent: root.agColor
      }

      QuotaRow {
        label: "Claude & GPT · 5 h"
        value: root.agClaude5h
        reset: root.agClaude5hReset
        accent: root.claudeColor
      }

      QuotaRow {
        label: "Claude & GPT · Semanal"
        value: root.agClaudeWeek
        reset: root.agClaudeWeekReset
        accent: root.claudeColor
      }

      QuotaRow {
        label: "Gemini 4 Argon · Semanal"
        value: root.agArgonWeek
        reset: root.agArgonWeekReset
        accent: root.argonColor
      }
    }
  }
}
