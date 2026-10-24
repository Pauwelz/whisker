import QtQuick
import qs.Commons
import qs.Ui

// The bar widget entry point: a bar icon and its pop-out. It reads the Daemon state from the Service.
Panel {
  id: root
  moduleName: "pauwelz.whisker"
  ipcTarget: "pauwelz.whisker"

  readonly property var whiskerService: bar && bar.shell ? bar.shell.serviceFor("pauwelz.whisker") : null
  readonly property var view: whiskerService ? whiskerService.view : ({ screen: "starting", message: "" })
  readonly property color contentForeground: bar ? bar.foreground : Color.foreground
  readonly property string contentFontFamily: bar ? bar.fontFamily : Style.font.family

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: ""
    dimmed: root.view.screen !== "ready"
    tooltipText: root.view.message
    onPressed: function(b) { root.toggle() }
  }

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    centerOnBar: true
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(380))
    contentHeight: panel.fittedContentHeight(message.implicitHeight)

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onCloseRequested: root.close()

      Text {
        id: message
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        wrapMode: Text.Wrap
        textFormat: Text.PlainText
        text: root.view.message
        color: root.view.screen === "error" ? Color.urgent : root.contentForeground
        font.family: root.contentFontFamily
        font.pixelSize: Style.font.body
      }
    }
  }
}
