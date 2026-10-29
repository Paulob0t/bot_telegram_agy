import QtQuick
import QtQuick.Layouts
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

BarWidget {
  id: root
  moduleName: "paulobot.telegram-bot"

  property bool isRunning: false
  property bool popupOpen: false

  function close() {
    popupOpen = false
  }

  function open() {
    popupOpen = true
  }

  function toggle() {
    popupOpen = !popupOpen
  }

  function checkStatus() {
    if (!checkProcess.running) {
      checkProcess.running = true
    }
  }

  function toggleBot() {
    if (root.isRunning) {
      root.isRunning = false
      stopProcess.running = true
    } else {
      root.isRunning = true
      startProcess.running = true
    }
  }

  Process {
    id: startProcess
    running: false
    command: ["/home/paulobot/.local/bin/telegram-bot-service", "start"]
    onExited: function() { root.checkStatus() }
  }

  Process {
    id: stopProcess
    running: false
    command: ["/home/paulobot/.local/bin/telegram-bot-service", "stop"]
    onExited: function() { root.checkStatus() }
  }

  Process {
    id: checkProcess
    running: false
    command: ["/home/paulobot/.local/bin/telegram-bot-service", "status"]
    onExited: function(code) {
      root.isRunning = (code === 0)
    }
  }

  Timer {
    interval: 5000
    running: true
    repeat: true
    triggeredOnStart: true
    onTriggered: root.checkStatus()
  }

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: "󰚩"
    foreground: root.isRunning ? (Color.accent || "#00e5ff") : (bar ? bar.barForeground : Color.foreground)
    active: root.isRunning || root.popupOpen
    activeColor: Color.accent || "#00e5ff"
    tooltipText: root.isRunning ? "Telegram Bot: ACTIVO" : "Telegram Bot: INACTIVO"

    onPressed: function(btn) {
      if (btn === Qt.RightButton) {
        root.toggleBot()
      } else {
        root.toggle()
      }
    }
  }

  PopupCard {
    id: popup
    anchorItem: root
    bar: root.bar
    owner: root
    open: root.popupOpen
    contentWidth: popup.fittedContentWidth(Style.space(300))
    contentHeight: popup.fittedContentHeight(mainColumn.implicitHeight)

    onClosed: {
      root.close()
    }

    Column {
      id: mainColumn
      anchors.fill: parent
      spacing: Style.space(12)

      RowLayout {
        width: parent.width
        spacing: Style.space(10)

        Rectangle {
          width: Style.space(40)
          height: Style.space(40)
          radius: Style.cornerRadius > 0 ? Style.cornerRadius : 6
          color: root.isRunning ? (Color.accent || "#00e5ff") : Style.normalFillFor(Color.foreground, Color.accent)
          opacity: root.isRunning ? 0.3 : 0.15

          Text {
            anchors.centerIn: parent
            text: "󰚩"
            font.family: root.bar ? root.bar.fontFamily : "monospace"
            font.pixelSize: Style.font.titleLarge
            color: root.isRunning ? (Color.accent || "#00e5ff") : Color.foreground
          }
        }

        ColumnLayout {
          Layout.fillWidth: true
          spacing: Style.space(2)

          Text {
            text: "Telegram AGY Bot"
            font.family: root.bar ? root.bar.fontFamily : "sans-serif"
            font.pixelSize: Style.font.bodyLarge
            font.bold: true
            color: Color.foreground
          }

          Text {
            text: root.isRunning ? "● En ejecución (online)" : "○ Detenido (offline)"
            font.family: root.bar ? root.bar.fontFamily : "sans-serif"
            font.pixelSize: Style.font.caption
            font.bold: true
            color: root.isRunning ? "#4caf50" : Color.subtext
          }
        }
      }

      Toggle {
        width: parent.width
        label: "Estado del Bot"
        description: root.isRunning ? "Escuchando mensajes de Telegram" : "Bot apagado"
        checked: root.isRunning
        accent: Color.accent || "#00e5ff"
        onClicked: root.toggleBot()
      }

      Button {
        width: parent.width
        text: root.isRunning ? "Detener Servicio" : "Iniciar Servicio"
        iconText: root.isRunning ? "󰓛" : "󰐊"
        accent: Color.accent || "#00e5ff"
        active: root.isRunning
        bordered: true
        onClicked: root.toggleBot()
      }
    }
  }
}
