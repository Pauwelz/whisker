import QtQuick
import qs.Commons
import qs.Ui
import "WhiskerState.js" as State

// One row of the open Chat's timeline: a day separator, a system line or a Message line.
// The pop-out (Panel.qml) owns the state; this item only draws it. The rows and the line-grouping rule come from State.timelineRows.
// A Message line is: a header (sender and time, first line of a run only), then the body at full width. The body stacks: Quote, text, media area, Reactions row, actions, failed line.
Item {
  id: line
  required property var row
  required property var panel // the pop-out: service, view state and colours
  readonly property var message: row.message
  readonly property bool isMessage: row.kind === "message"
  readonly property bool mine: isMessage && message.fromMe === true
  readonly property bool group: !!panel.openChat && panel.openChat.kind === "group"
  readonly property bool menuOpen: isMessage && panel.menuId === message.id
  readonly property bool pickerOpen: isMessage && panel.reactingId === message.id
  // The toolbar shows on hover, and stays while its quick emoji bar or the menu is open.
  readonly property bool toolsShown: isMessage && (hover.hovered || menuOpen || pickerOpen)
  implicitHeight: content.implicitHeight
  z: menuOpen || pickerOpen ? 10 : 1 // the menu draws over the lines below

  HoverHandler { id: hover }
  // A right click opens the menu.
  TapHandler {
    acceptedButtons: Qt.RightButton
    onTapped: if (line.isMessage) line.panel.menuId = line.message.id
  }

  // Runs one action of State.actionsFor. The toolbar and the menu call it.
  function run(name) {
    var service = line.panel.whiskerService
    var message = line.message
    line.panel.menuId = ""
    line.panel.reactingId = ""
    switch (name) {
    case "reply":
      service.changeDraft({ quote: message.id })
      line.panel.focusComposer()
      break
    case "react": line.panel.reactingId = message.id; break
    case "copy": service.copyText(State.messageBody(message)); break
    case "edit":
      service.changeDraft({ edit: message.id, text: message.text })
      line.panel.focusComposer()
      break
    case "retry": service.retryMessage(message.id); break
    case "download": service.downloadMedia(message); break
    case "play": case "open": service.openMedia(message); break
    case "saveAs": line.panel.savingId = message.id; break
    case "deleteForEveryone": service.revokeMessage(message.id); break
    case "deleteForMe": service.deleteMessageForMe(message.id); break
    }
  }

  // The Message under the pointer, or with its toolbar open: a light tint shows which Message the toolbar acts on.
  Rectangle {
    anchors.fill: parent
    visible: line.toolsShown
    color: line.panel.contentForeground
    opacity: 0.06
  }

  // A Message that mentions the user.
  Rectangle {
    anchors.fill: parent
    visible: line.isMessage && line.message.mentionsMe === true
    color: Color.accent
    opacity: 0.15
  }

  Column {
    id: content
    width: parent.width
    spacing: Style.space(2)

    // Day separator and system line: quiet and centred.
    Text {
      visible: line.row.kind === "day" || line.row.kind === "system"
      width: parent.width
      horizontalAlignment: Text.AlignHCenter
      textFormat: Text.PlainText
      text: line.row.kind === "day" ? line.row.label : (line.message ? line.message.text : "")
      font.italic: line.row.kind === "system"
      opacity: 0.5
      color: line.panel.contentForeground
      font.family: line.panel.contentFontFamily
      font.pixelSize: Style.font.caption
    }

    // Header line of a run: the sender and the time. The next Messages of the run have none.
    Row {
      visible: line.isMessage && !line.row.continued
      spacing: Style.space(8)
      Text {
        textFormat: Text.PlainText
        text: line.isMessage ? State.senderLabel(line.message) : ""
        color: line.mine ? Color.accent : (line.isMessage && line.group ? State.senderColor(line.message.sender) : line.panel.contentForeground)
        font.family: line.panel.contentFontFamily
        font.pixelSize: Style.font.body
        font.bold: true
      }
      Text {
        anchors.verticalCenter: parent.verticalCenter
        textFormat: Text.PlainText
        text: line.isMessage ? State.timeLabel(line.message.time) : ""
        opacity: 0.5
        color: line.panel.contentForeground
        font.family: line.panel.contentFontFamily
        font.pixelSize: Style.font.caption
      }
    }

    // The body, indented under the header at the full line width. The Delivery State sits at the right end of its first line.
    Item {
      id: bodyArea
      readonly property real indent: Style.space(8)
      readonly property real markWidth: Style.space(20)
      visible: line.isMessage
      x: indent
      width: parent.width - indent
      height: body.height

      // The mark shows on each Message of the user, also inside a run.
      Text {
        readonly property var mark: line.isMessage ? State.deliveryMark(line.message) : ({ text: "", tone: "normal" })
        anchors.right: parent.right
        anchors.top: parent.top
        horizontalAlignment: Text.AlignRight
        textFormat: Text.PlainText
        text: mark.text
        color: mark.tone === "error" ? Color.urgent : (mark.tone === "read" ? Color.accent : line.panel.contentForeground)
        opacity: mark.tone === "normal" ? 0.5 : 1
        font.bold: mark.tone === "error"
        font.family: line.panel.contentFontFamily
        font.pixelSize: Style.font.caption
      }

      Column {
        id: body
        width: parent.width - bodyArea.markWidth
        spacing: Style.space(2)

        // The Quote of a reply: the quoted author and a one-line preview. A click jumps to the quoted Message.
        Text {
          readonly property var quote: line.isMessage ? State.quoteView(line.message) : null
          visible: quote !== null
          width: parent.width
          elide: Text.ElideRight
          textFormat: Text.PlainText
          text: quote ? quote.sender + ": " + quote.text : ""
          opacity: 0.6
          color: line.panel.contentForeground
          font.family: line.panel.contentFontFamily
          font.pixelSize: Style.font.caption
          TapHandler { onTapped: line.panel.jumpToMessage(parent.quote.id) }
        }
        // The text. An edited Message ends with "(edited)". A Tombstone shows its fixed text in italics.
        Text {
          visible: text !== ""
          width: parent.width
          wrapMode: Text.Wrap
          textFormat: Text.StyledText
          text: line.isMessage ? State.messageHtml(line.message) + (State.editedLabel(line.message) !== "" ? " <i>" + State.editedLabel(line.message) + "</i>" : "") : ""
          font.italic: line.isMessage && line.message.revoked === true
          opacity: line.isMessage && line.message.revoked === true ? 0.6 : 1
          color: line.panel.contentForeground
          font.family: line.panel.contentFontFamily
          font.pixelSize: Style.font.body
        }

        // Media area: the chip of the Media, and the Save as field.
        Column {
          id: mediaArea
          width: parent.width
          spacing: Style.space(2)
          Button {
            readonly property var map: line.isMessage ? State.mapView(line.message) : null
            visible: map !== null
            text: "Open map"
            bordered: true
            onClicked: line.panel.whiskerService.openLink(map.url)
          }
          // A sticker shows here once the Daemon holds it. The path comes from media.open, never bytes.
          Image {
            readonly property string file: line.isMessage ? State.mediaPath(line.panel.view, line.message) : ""
            visible: file !== "" && State.showsInline(line.message)
            source: visible ? "file://" + file : ""
            width: Math.min(parent.width, Style.space(240))
            fillMode: Image.PreserveAspectFit
            asynchronous: true
            TapHandler { onTapped: line.panel.whiskerService.openMedia(line.message) }
          }
          Loader {
            active: line.isMessage && State.chipShown(line.message)
            visible: active
            sourceComponent: MediaChip { message: line.message; panel: line.panel }
          }
          // Save as: type an absolute path, then press Enter. Escape cancels.
          TextEdit {
            visible: line.isMessage && line.panel.savingId === line.message.id
            width: parent.width
            color: line.panel.contentForeground
            font.family: line.panel.contentFontFamily
            font.pixelSize: Style.font.body
            onVisibleChanged: if (visible) forceActiveFocus()
            Keys.onPressed: function(event) {
              if (event.key === Qt.Key_Escape) {
                line.panel.savingId = ""
              } else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
                line.panel.whiskerService.saveMedia(line.message, text.trim())
                line.panel.savingId = ""
                text = ""
              } else {
                return
              }
              event.accepted = true
            }
          }
        }

        // Reactions row: one chip per emoji with its count. Your own Reaction is outlined. A click adds or removes yours.
        // Hover lists who reacted.
        Row {
          id: reactionsRow
          spacing: Style.space(4)
          Repeater {
            model: line.isMessage ? State.reactionChips(line.message) : []
            Item {
              id: chip
              required property var modelData
              width: chipText.implicitWidth + Style.space(10)
              height: chipText.implicitHeight + 2
              // A small chip as in the prototype: 1px border, the user's own Reaction in the accent colour. A click adds or removes it.
              Rectangle {
                anchors.fill: parent
                radius: 3
                color: "transparent"
                border.width: 1
                border.color: chip.modelData.mine ? Color.accent : Color.muted
              }
              Text {
                id: chipText
                anchors.centerIn: parent
                textFormat: Text.PlainText
                text: chip.modelData.emoji + " " + chip.modelData.count
                color: line.panel.contentForeground
                font.family: line.panel.contentFontFamily
                font.pixelSize: Style.font.caption
              }
              TapHandler {
                onTapped: line.panel.whiskerService.reactToMessage(line.message, chip.modelData.emoji)
              }
              HoverHandler { id: chipHover }
              Rectangle {
                visible: chipHover.hovered
                y: -height - Style.space(2)
                width: who.implicitWidth + Style.space(8)
                height: who.implicitHeight + Style.space(4)
                color: Color.background
                border.color: Color.muted
                border.width: 1
                z: 20
                Text {
                  id: who
                  anchors.centerIn: parent
                  textFormat: Text.PlainText
                  text: chip.modelData.names
                  color: line.panel.contentForeground
                  font.family: line.panel.contentFontFamily
                  font.pixelSize: Style.font.caption
                }
              }
            }
          }
        }
        // A failed Message: a red line below it. A click sends it again.
        Text {
          visible: text !== ""
          textFormat: Text.PlainText
          text: line.isMessage ? State.failedLine(line.message) : ""
          color: Color.urgent
          font.family: line.panel.contentFontFamily
          font.pixelSize: Style.font.caption
          TapHandler { onTapped: line.panel.whiskerService.retryMessage(line.message.id) }
        }
      }
    }
  }

  // Toolbar: React, Reply, More. It floats at the top right of the Message, over the content, so the text does not move.
  // React turns it into the quick emoji, the current Reaction outlined.
  Rectangle {
    anchors.right: parent.right
    y: bodyArea.y // on the first text line, not on the sender header; content sits at y 0
    visible: line.toolsShown
    width: toolRow.width + 2
    height: toolRow.height + 2
    color: Color.background
    border.color: Color.muted
    border.width: 1
    Row {
      id: toolRow
      anchors.centerIn: parent
      spacing: 0
      Repeater {
        model: line.pickerOpen ? State.REACTION_EMOJI : []
        Button {
          required property string modelData
          text: modelData
          horizontalPadding: Style.space(4)
          verticalPadding: 0
          fontSize: Style.font.caption
          bordered: State.myReaction(line.message) === modelData
          onClicked: {
            line.panel.whiskerService.reactToMessage(line.message, modelData)
            line.panel.reactingId = ""
          }
        }
      }
      Repeater {
        model: line.pickerOpen ? [] : [{ name: "react", icon: "☺" }, { name: "reply", icon: "↩" }, { name: "more", icon: "⋯" }]
        Button {
          required property var modelData
          visible: line.isMessage && (modelData.name === "more" || State.menuActions(line.message, Math.floor(Date.now() / 1000)).indexOf(modelData.name) >= 0)
          text: modelData.icon
          horizontalPadding: Style.space(4)
          verticalPadding: 0
          fontSize: Style.font.caption
          onClicked: modelData.name === "more" ? line.panel.menuId = line.message.id : line.run(modelData.name)
        }
      }
    }
  }

  // The menu: every action the rule allows. It opens upward in the lower half of the timeline.
  Rectangle {
    readonly property bool up: !!line.ListView.view && line.y - line.ListView.view.contentY > line.ListView.view.height / 2
    visible: line.menuOpen
    x: bodyArea.indent
    y: up ? -height : line.height
    width: Style.space(180)
    height: menuColumn.height + Style.space(8)
    color: Color.background
    border.color: Color.muted
    border.width: 1
    Column {
      id: menuColumn
      anchors.centerIn: parent
      width: parent.width - Style.space(8)
      Repeater {
        model: line.menuOpen ? State.menuActions(line.message, Math.floor(Date.now() / 1000)) : []
        Button {
          required property string modelData
          width: parent.width
          text: State.ACTION_LABELS[modelData]
          onClicked: line.run(modelData)
        }
      }
    }
  }
}
