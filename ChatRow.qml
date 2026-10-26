import QtQuick
import QtQuick.Layouts
import qs.Commons
import "state/ChatList.js" as ChatList

// One dense line of the Chat list: Chat Sign, name, "is typing…", the Chat Marks pin, mute and mention, then the trailing slot.
// The pop-out (Panel.qml) owns the state. The marks and labels come from the state/ modules.
Item {
  id: row
  required property var chat
  required property var panel // the pop-out: service, colours and the view state
  readonly property string typing: ChatList.chatActivityLabel(panel.view, chat) // "typing…", or ""
  // The slot at the right end. The unread count and the time sit in it; on hover the Chat links take their place.
  // Keep the slot at a fixed width: the line must not move on hover.
  readonly property alias trailing: trailingSlot
  readonly property real trailingWidth: Style.space(88)

  implicitHeight: line.implicitHeight
  height: implicitHeight

  HoverHandler { id: hover }
  // A tap opens the Chat, but not on the hover links: they take the tap. They can be wider than the trailing slot.
  Item {
    anchors.fill: parent
    anchors.rightMargin: Math.max(row.trailingWidth, hoverLinks.width)
    TapHandler { onTapped: panel.whiskerService.openChat(chat) }
  }

  component Mark: Text {
    color: row.panel.contentForeground
    font.family: row.panel.contentFontFamily
    font.pixelSize: Style.font.body
  }

  component MarkColumn: Mark {
    property bool shown
    Layout.preferredWidth: Style.space(16)
    horizontalAlignment: Text.AlignHCenter
    color: shown && !hover.hovered ? row.panel.contentForeground : "transparent"
  }

  RowLayout {
    id: line
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.leftMargin: Style.space(8)
    spacing: Style.space(6)

    // The sign slot has the size and place of the icon slot in the top bars, so the name does not move when the Chat opens.
    Item {
      Layout.preferredWidth: Style.space(16)
      Layout.preferredHeight: Style.space(16)
      Layout.rightMargin: Style.space(2) // with the spacing, the 8 of the top bars
      Mark { anchors.centerIn: parent; text: ChatList.chatSign(row.chat); opacity: 0.6 }
    }
    Mark {
      Layout.maximumWidth: parent.width * 0.5
      elide: Text.ElideRight
      textFormat: Text.PlainText
      text: ChatList.chatName(row.chat)
      font.bold: row.chat.unread
    }
    Mark {
      visible: text !== ""
      textFormat: Text.PlainText
      text: ChatList.chatNumber(row.chat)
      font.bold: row.chat.unread
    }
    Mark {
      Layout.fillWidth: true
      elide: Text.ElideRight
      textFormat: Text.PlainText
      text: row.typing
      font.italic: true
      opacity: hover.hovered ? 0 : 1 // the hover links may reach over it
    }
    // The Chat Marks: one fixed column each, so a mark has the same place on every line. The hover links reach over them.
    MarkColumn { text: ChatList.PIN_MARK; shown: row.chat.pinned === true; opacity: 0.8 }
    MarkColumn { text: "󰝟"; shown: row.chat.muted === true; opacity: 0.6 }
    MarkColumn { text: "@"; shown: row.chat.mention === true; font.bold: true } // the Chat mentions the user
    Item {
      id: trailingSlot
      Layout.preferredWidth: row.trailingWidth
      implicitHeight: countText.implicitHeight
      Mark {
        id: countText
        visible: !hover.hovered
        anchors.left: parent.left
        text: ChatList.chatCountLabel(row.chat)
        font.bold: true
        opacity: row.chat.muted ? 0.4 : 1 // a Muted Chat shows a grey count
      }
      Mark {
        visible: !hover.hovered
        anchors.right: parent.right
        text: ChatList.chatTimeLabel(row.chat.lastActive, Math.floor(Date.now() / 1000))
        opacity: 0.5
      }
      // The Chat links, on hover. They grow to the left out of the slot; the name has room, since it takes at most half the line.
      Loader {
        id: hoverLinks
        active: hover.hovered
        anchors.right: parent.right
        sourceComponent: ChatLinks { chat: row.chat; panel: row.panel }
      }
    }
  }
}
