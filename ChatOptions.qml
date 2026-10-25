import QtQuick
import qs.Commons
import qs.Ui
import "WhiskerState.js" as State

// The Chat menu: Pin or Unpin, Mark as read or unread, then the mute choices or Unmute. The options come from State.chatOptions.
// The pop-out (Panel.qml) opens it for one Chat through `optionsFor`; create it when it opens, so the mute end times are fresh.
Column {
  id: menu
  required property var chat
  required property var panel // the pop-out: service and menu state
  readonly property var options: State.chatOptions(chat, Math.floor(Date.now() / 1000))
  spacing: Style.space(2)

  Repeater {
    model: menu.options
    Button {
      required property var modelData
      text: modelData.label
      bordered: true
      onClicked: {
        menu.panel.optionsFor = ""
        menu.panel.whiskerService.chooseChatOption(menu.chat, modelData)
      }
    }
  }
}
