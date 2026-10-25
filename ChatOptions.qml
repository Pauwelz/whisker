import QtQuick
import qs.Commons
import "WhiskerState.js" as State

// The Chat menu: one right-aligned line of links, like the hover buttons of a Chat line. The items come from State.chatOptions.
// The pop-out (Panel.qml) opens it for one Chat through `optionsFor`; create it when it opens, so the mute end times are fresh.
Item {
  id: menu
  required property var chat
  required property var panel // the pop-out: service, colours and menu state
  readonly property var options: State.chatOptions(chat, Math.floor(Date.now() / 1000))
  implicitHeight: links.implicitHeight + Style.space(4)

  Row {
    id: links
    anchors.right: parent.right
    anchors.verticalCenter: parent.verticalCenter
    spacing: Style.space(8)

    Repeater {
      model: menu.options
      Text {
        required property var modelData
        text: modelData.text
        color: menu.panel.contentForeground
        opacity: modelData.option ? 1 : 0.5 // "mute" is a label for the choices after it
        font.family: menu.panel.contentFontFamily
        font.pixelSize: Style.font.body
        font.underline: link.hovered && !!modelData.option
        HoverHandler { id: link; cursorShape: modelData.option ? Qt.PointingHandCursor : Qt.ArrowCursor }
        TapHandler {
          enabled: !!modelData.option
          onTapped: {
            menu.panel.optionsFor = ""
            menu.panel.whiskerService.chooseChatOption(menu.chat, modelData.option)
          }
        }
      }
    }
  }
}
