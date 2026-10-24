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
  // Which linking method the pop-out shows. The user picks it, so it is view state only.
  property bool pairMode: false

  readonly property color contentForeground: bar ? bar.foreground : Color.foreground
  readonly property string contentFontFamily: bar ? bar.fontFamily : Style.font.family

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: ""
    dimmed: root.view.screen !== "ready" && root.view.screen !== "linked"
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
    contentHeight: panel.fittedContentHeight(column.implicitHeight)

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onCloseRequested: root.close()

      Column {
        id: column
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        spacing: Style.space(8)

        Text {
          id: message
          width: parent.width
          wrapMode: Text.Wrap
          textFormat: Text.PlainText
          text: root.view.message
          color: root.view.screen === "error" ? Color.urgent : root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
        }

        // The Daemon sends the QR code as a data: URI.
        Image {
          visible: root.view.screen === "linking" && !!root.view.qr && !root.pairMode
          source: root.view.qr || ""
          width: Style.space(232)
          height: width
          fillMode: Image.PreserveAspectFit
          smooth: false
        }

        Text {
          visible: !!root.view.pairCode
          text: root.view.pairCode || ""
          color: root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body * 2
        }

        TextInput {
          id: phoneInput
          visible: root.view.screen === "linking" && root.pairMode
          width: parent.width
          color: root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
          inputMethodHints: Qt.ImhDialableCharactersOnly
          onAccepted: root.whiskerService.requestPairCode(text)
        }

        Button {
          visible: root.view.screen === "linking" && root.pairMode
          text: "Get Pair Code"
          bordered: true
          onClicked: root.whiskerService.requestPairCode(phoneInput.text)
        }

        Button {
          visible: root.view.screen === "linking"
          text: root.pairMode ? "Use a QR code" : "Use a Pair Code"
          bordered: true
          onClicked: root.pairMode = !root.pairMode
        }

        // The Chat list: the Daemon sends the Chats in order, with the name and the marks.
        ListView {
          visible: root.view.screen === "linked"
          width: parent.width
          height: visible ? Style.space(320) : 0
          clip: true
          model: root.view.chats || []
          delegate: Row {
            width: ListView.view.width
            spacing: Style.space(8)
            Text {
              text: modelData.unread ? "●" : " "
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
            }
            Text {
              width: parent.width - Style.space(100)
              elide: Text.ElideRight
              textFormat: Text.PlainText
              text: modelData.name
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
              font.bold: modelData.unread
            }
            Text {
              text: (modelData.pinned ? "pinned " : "") + (modelData.muted ? "muted" : "")
              color: root.contentForeground
              opacity: 0.6
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
            }
          }
        }

        Button {
          visible: root.view.screen === "linked" && root.view.moreChats === true
          text: "Load more"
          bordered: true
          onClicked: root.whiskerService.loadMoreChats()
        }

        Button {
          id: retryButton
          visible: root.view.canRetry === true
          text: "Retry"
          bordered: true
          onClicked: root.whiskerService.retry()
        }
      }
    }
  }
}
