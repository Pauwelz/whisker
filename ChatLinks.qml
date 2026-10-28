import QtQuick
import qs.Commons
import "state/ChatList.js" as ChatList

// The links of a Chat: pin, mark unread and mute, from ChatList.chatLinks. A click on mute shows the mute choices in their place.
// A Chat line shows them on hover, the Chat header after a click on ⋯. Create them when they show, so the mute choices start closed.
Row {
  id: links
  required property var chat
  required property var panel // the pop-out: service and colours
  property color linkColor: Color.muted // the Chat header passes the text colour
  property bool muting: false
  spacing: Style.space(8)

  Repeater {
    // `muting` changes when mute is clicked, so the end times of the mute choices are fresh.
    model: ChatList.chatLinks(links.chat, links.muting, Math.floor(Date.now() / 1000))
    Link {
      required property var modelData
      text: modelData.text
      color: links.linkColor
      font.family: links.panel.contentFontFamily
      onClicked: {
        if (modelData.option) links.panel.whiskerService.chooseChatOption(links.chat, modelData.option)
        links.muting = !modelData.option
      }
    }
  }
}
