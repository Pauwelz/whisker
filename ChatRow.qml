import QtQuick
import QtQuick.Layouts
import qs.Commons
import "WhiskerState.js" as State

// One dense line of the Chat list: sign, pin mark, name, mute and mention marks, "is typing…", then the trailing slot.
// The pop-out (Panel.qml) owns the state. The marks and labels come from WhiskerState.js.
Item {
  id: row
  required property var chat
  required property var panel // the pop-out: service, colours and the view state
  readonly property string typing: State.chatActivityLabel(panel.view, chat) // "typing…", or ""
  // The slot at the right end. The unread count and the time sit in it; on hover the pin and mute buttons take their place.
  // Keep the slot at a fixed width: the line must not move on hover.
  readonly property alias trailing: trailingSlot
  readonly property real trailingWidth: Style.space(88)
  readonly property bool optionsOpen: panel.optionsFor === chat.jid

  implicitHeight: line.implicitHeight + (optionsOpen ? menu.height : 0)
  height: implicitHeight

  HoverHandler { id: hover }
  // A tap opens the Chat, but not on the trailing slot: its buttons take the tap.
  Item {
    anchors.fill: parent
    anchors.rightMargin: row.trailingWidth
    TapHandler { onTapped: panel.whiskerService.openChat(chat) }
  }
  // A right click opens the Chat menu below the line, or closes it.
  TapHandler {
    acceptedButtons: Qt.RightButton
    onTapped: panel.optionsFor = row.optionsOpen ? "" : chat.jid
  }

  component Mark: Text {
    color: row.panel.contentForeground
    font.family: row.panel.contentFontFamily
    font.pixelSize: Style.font.body
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
      Mark { anchors.centerIn: parent; text: State.chatSign(row.chat); opacity: 0.6 }
    }
    Mark {
      Layout.maximumWidth: parent.width * 0.5
      elide: Text.ElideRight
      textFormat: Text.PlainText
      text: row.chat.name
      font.bold: row.chat.unread
    }
    Mark { text: "▲"; visible: row.chat.pinned === true; opacity: 0.8 } // Pinned: after the name, so names line up
    Mark { text: "󰝟"; visible: row.chat.muted === true; opacity: 0.6 } // Muted
    Mark { text: "@"; visible: row.chat.mention === true; font.bold: true } // the Chat mentions the user
    Mark {
      Layout.fillWidth: true
      elide: Text.ElideRight
      textFormat: Text.PlainText
      text: row.typing
      font.italic: true
    }
    Item {
      id: trailingSlot
      Layout.preferredWidth: row.trailingWidth
      implicitHeight: countText.implicitHeight
      Mark {
        id: countText
        visible: !hover.hovered
        anchors.left: parent.left
        text: State.chatCountLabel(row.chat)
        font.bold: true
        opacity: row.chat.muted ? 0.4 : 1 // a Muted Chat shows a grey count
      }
      Mark {
        visible: !hover.hovered
        anchors.right: parent.right
        text: State.chatTimeLabel(row.chat.lastActive, Math.floor(Date.now() / 1000))
        opacity: 0.5
      }
      // Hover buttons, from State.chatRowButtons.
      Row {
        visible: hover.hovered
        anchors.right: parent.right
        spacing: Style.space(8)
        Mark {
          text: State.chatRowButtons(row.chat).pin.text
          TapHandler { onTapped: row.panel.whiskerService.chooseChatOption(row.chat, State.chatRowButtons(row.chat).pin.option) }
        }
        Mark {
          text: State.chatRowButtons(row.chat).mute.text
          TapHandler {
            onTapped: {
              var option = State.chatRowButtons(row.chat).mute.option
              if (option) row.panel.whiskerService.chooseChatOption(row.chat, option)
              else row.panel.optionsFor = row.chat.jid
            }
          }
        }
      }
    }
  }

  Loader {
    id: menu
    active: row.optionsOpen
    anchors.top: line.bottom
    sourceComponent: ChatOptions { chat: row.chat; panel: row.panel }
  }
}
