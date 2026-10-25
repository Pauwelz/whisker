import QtQuick
import qs.Commons
import "WhiskerState.js" as State

// The links of a Chat: pin, mark unread and mute, from State.chatLinks. A click on mute shows the mute choices in their place.
// A Chat line shows them on hover, the Chat header after a click on ⋯. Create them when they show, so the mute choices start closed.
Row {
  id: links
  required property var chat
  required property var panel // the pop-out: service and colours
  property bool muting: false
  spacing: Style.space(8)

  Repeater {
    // `muting` changes when mute is clicked, so the end times of the mute choices are fresh.
    model: State.chatLinks(links.chat, links.muting, Math.floor(Date.now() / 1000))
    Text {
      required property var modelData
      text: modelData.text
      color: links.panel.contentForeground
      font.family: links.panel.contentFontFamily
      font.pixelSize: Style.font.body
      font.underline: hover.hovered
      HoverHandler { id: hover; cursorShape: Qt.PointingHandCursor }
      TapHandler {
        onTapped: {
          if (modelData.option) links.panel.whiskerService.chooseChatOption(links.chat, modelData.option)
          links.muting = !modelData.option
        }
      }
    }
  }
}
