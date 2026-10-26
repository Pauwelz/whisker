import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "state/Draft.js" as Draft
import "state/Shortcode.js" as Shortcode

// The composer of the open Chat: one strip under a line with a prompt, the text and Send.
// Enter sends, Shift+Enter adds a line. Ctrl+V of a copied file makes it the Attachment. The pop-out (Panel.qml) owns the Draft; this item only draws it.
// An Emoji Shortcode such as ":smi" shows its emoji in a row above the line, with the code of the selected one; a closing ":" on a full code replaces it at once.
FocusScope {
  id: composer
  property var panel: null // the pop-out: service, Draft and colours
  readonly property var service: panel ? panel.whiskerService : null
  readonly property var draft: panel ? panel.draft : ({ text: "", attachments: [] })
  readonly property bool hasAttachment: Draft.hasAttachment(draft)
  implicitHeight: askRow.height + suggestionList.height + row.implicitHeight + Style.space(8)
  readonly property var pasteQuestion: panel ? Draft.pasteQuestion(panel.view) : null
  property string lastText: "" // the last text within the limit

  // Omarchy's emoji list. Without it, Emoji Shortcodes are off.
  property var emojis: []
  FileView {
    path: Quickshell.env("OMARCHY_PATH") + "/shell/plugins/emojis/emojis.json"
    printErrors: false
    onLoaded: composer.emojis = Shortcode.parseEmojis(text())
  }

  readonly property var shortcode: Shortcode.shortcodeAt(input.text, input.cursorPosition)
  property int dismissedStart: -1 // Escape hides the row for the Emoji Shortcode that starts here
  property int selected: 0
  readonly property var suggestions: shortcode && shortcode.start !== dismissedStart ? Shortcode.shortcodeSuggestions(emojis, shortcode.query) : []
  onShortcodeChanged: {
    selected = 0
    if (!shortcode) dismissedStart = -1
  }

  function setText(change) {
    input.text = change.text
    input.cursorPosition = change.cursor
  }

  function pick(index) {
    setText(Shortcode.insertEmoji(input.text, shortcode.start, input.cursorPosition, suggestions[index].emoji))
  }

  function send() {
    if (Draft.canSend(input.text, composer.hasAttachment)) composer.service.submitComposer(input.text)
  }

  // The line above the strip.
  Rectangle {
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.top: parent.top
    height: 1
    color: Color.muted
  }

  // ‹ and › show when the row can scroll that way. A click moves the selection.
  component ScrollArrow: Text {
    property bool more: false
    property int step: 0
    visible: suggestionList.count > 0
    anchors.verticalCenter: suggestionList.verticalCenter
    width: 16
    horizontalAlignment: Text.AlignHCenter
    text: step < 0 ? "‹" : "›"
    color: composer.panel ? composer.panel.contentForeground : Color.foreground
    opacity: more ? 0.6 : 0
    font.pixelSize: Style.font.body + 4
    MouseArea {
      anchors.fill: parent
      enabled: parent.more
      onClicked: composer.selected = Math.max(0, Math.min(composer.suggestions.length - 1, composer.selected + parent.step))
    }
  }

  ScrollArrow {
    id: leftArrow
    anchors.left: parent.left
    anchors.leftMargin: Style.space(4)
    step: -1
    more: !suggestionList.atXBeginning
  }

  ScrollArrow {
    id: rightArrow
    anchors.right: codeLabel.left
    anchors.rightMargin: Style.space(4)
    step: 1
    more: !suggestionList.atXEnd
  }

  // The question about copied files and text: attach the files, or paste the text.
  Row {
    id: askRow
    visible: composer.pasteQuestion !== null
    height: visible ? 28 + Style.space(4) : 0
    anchors.left: parent.left
    anchors.leftMargin: Style.space(8)
    anchors.top: parent.top
    anchors.topMargin: visible ? Style.space(4) : 0
    spacing: Style.space(8)
    Text {
      anchors.verticalCenter: parent.verticalCenter
      textFormat: Text.PlainText
      text: composer.pasteQuestion ? composer.pasteQuestion.text : ""
      color: composer.panel ? composer.panel.contentForeground : Color.foreground
      font.family: composer.panel ? composer.panel.contentFontFamily : Style.font.family
      font.pixelSize: Style.font.body
    }
    Button {
      text: "Attach"
      onClicked: composer.service.answerPaste(true)
    }
    Button {
      text: "Paste text"
      onClicked: {
        composer.service.answerPaste(false)
        input.forceActiveFocus()
      }
    }
  }

  ListView {
    id: suggestionList
    anchors.left: leftArrow.right
    anchors.right: rightArrow.left
    anchors.top: askRow.bottom
    anchors.topMargin: count > 0 ? Style.space(4) : 0
    height: count > 0 ? 28 : 0
    orientation: ListView.Horizontal
    clip: true
    model: composer.suggestions
    // The row scrolls so that the selected emoji stays in view.
    currentIndex: composer.selected
    highlightMoveDuration: 0
    delegate: Rectangle {
      required property var modelData
      required property int index
      width: 32
      height: 28
      radius: 4
      color: index === composer.selected && composer.panel ? composer.panel.surfaceColor : "transparent"
      Text {
        anchors.centerIn: parent
        textFormat: Text.PlainText
        text: modelData.emoji
        font.pixelSize: Style.font.body + 4
      }
      MouseArea {
        anchors.fill: parent
        hoverEnabled: true
        onEntered: composer.selected = index
        onClicked: composer.pick(index)
      }
    }
  }

  // The code of the selected emoji, at the right end of the row. A fixed width keeps the row still while the selection moves.
  Text {
    id: codeLabel
    visible: suggestionList.count > 0
    anchors.verticalCenter: suggestionList.verticalCenter
    anchors.right: parent.right
    anchors.rightMargin: Style.space(8)
    width: visible ? 160 : 0
    horizontalAlignment: Text.AlignRight
    elide: Text.ElideRight
    textFormat: Text.PlainText
    readonly property var chosen: composer.suggestions[composer.selected] // undefined for a moment while the list shrinks
    text: chosen ? ":" + chosen.code + ":" : ""
    color: composer.panel ? composer.panel.contentForeground : Color.foreground
    opacity: 0.6
    font.family: composer.panel ? composer.panel.contentFontFamily : Style.font.family
    font.pixelSize: Style.font.caption
  }

  Row {
    id: row
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.top: suggestionList.bottom
    anchors.topMargin: Style.space(4)
    anchors.leftMargin: Style.space(8)
    anchors.rightMargin: Style.space(4)
    spacing: Style.space(8)

    Text {
      anchors.verticalCenter: parent.verticalCenter
      text: "›"
      color: Color.accent
      font.family: composer.panel ? composer.panel.contentFontFamily : Style.font.family
      font.pixelSize: Style.font.body
    }

    TextEdit {
      id: input
      anchors.verticalCenter: parent.verticalCenter
      width: row.width - x - sendButton.width - row.spacing
      focus: true
      wrapMode: TextEdit.Wrap
      color: composer.panel ? composer.panel.contentForeground : Color.foreground
      font.family: composer.panel ? composer.panel.contentFontFamily : Style.font.family
      font.pixelSize: Style.font.body
      onTextChanged: {
        var capped = Draft.capText(text, composer.lastText)
        if (!capped.ok) {
          if (composer.service) composer.service.textRefused()
          text = capped.text
          return
        }
        composer.lastText = text
        if (composer.service) composer.service.composerChanged(text)
      }
      readonly property string draftText: composer.draft.text
      onDraftTextChanged: if (text !== draftText) text = draftText
      // Escape first closes the Emoji Shortcode row, then goes back as everywhere else.
      Keys.onEscapePressed: {
        if (composer.suggestions.length > 0) composer.dismissedStart = composer.shortcode.start
        else composer.panel.runShortcut("Escape")
      }
      Keys.onPressed: function(event) {
        if (composer.suggestions.length > 0) {
          var move = Shortcode.shortcodeKeyAction(event.key, (event.modifiers & Qt.ShiftModifier) !== 0)
          if (move !== "default") {
            event.accepted = true
            if (move === "previous") composer.selected = Math.max(0, composer.selected - 1)
            if (move === "next") composer.selected = Math.min(composer.suggestions.length - 1, composer.selected + 1)
            if (move === "pick") composer.pick(composer.selected)
            return
          }
        }
        if (event.text === ":") {
          var closed = Shortcode.closeShortcode(text, cursorPosition, composer.emojis)
          if (closed) {
            event.accepted = true
            composer.setText(closed)
            return
          }
        }
        var action = Draft.composerAction(event.key, (event.modifiers & Qt.ShiftModifier) !== 0, text, composer.hasAttachment,
                                          (event.modifiers & Qt.ControlModifier) !== 0, composer.panel ? composer.panel.editingId : "")
        if (action === "default") return
        event.accepted = true
        if (action === "send") composer.service.submitComposer(text)
        if (action === "paste") composer.service.pasteIntoComposer()
      }

      Connections {
        target: composer.service
        function onPasteText() { input.paste() }
      }

      Text {
        visible: input.text === ""
        text: "type a message · ⏎ send"
        color: input.color
        opacity: 0.5
        font: input.font
      }
    }

    Button {
      id: sendButton
      anchors.verticalCenter: parent.verticalCenter
      enabled: Draft.canSend(input.text, composer.hasAttachment)
      opacity: enabled ? 1 : 0.4
      text: "⏎"
      tooltipText: "Send"
      onClicked: composer.send()
    }
  }
}
