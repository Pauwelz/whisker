import QtQuick
import qs.Commons
import qs.Ui
import "WhiskerState.js" as State

// The composer of the open Chat: one strip under a line with a prompt, the text and Send.
// Enter sends, Shift+Enter adds a line. Ctrl+V of a copied file makes it the Attachment. The pop-out (Panel.qml) owns the Draft; this item only draws it.
FocusScope {
  id: composer
  property var panel: null // the pop-out: service, Draft and colours
  readonly property var service: panel ? panel.whiskerService : null
  readonly property var draft: panel ? panel.draft : ({ text: "", attachments: [] })
  readonly property bool hasAttachment: State.hasAttachment(draft)
  implicitHeight: row.implicitHeight + Style.space(8)

  function send() {
    if (State.canSend(input.text, composer.hasAttachment)) composer.service.submitComposer(input.text)
  }

  // The line above the strip.
  Rectangle {
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.top: parent.top
    height: 1
    color: Color.muted
  }

  Row {
    id: row
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.verticalCenter: parent.verticalCenter
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
      onTextChanged: if (composer.service) composer.service.composerChanged(text)
      readonly property string draftText: composer.draft.text
      onDraftTextChanged: if (text !== draftText) text = draftText
      Keys.onEscapePressed: composer.panel.runShortcut("Escape")
      Keys.onPressed: function(event) {
        var action = State.composerAction(event.key, (event.modifiers & Qt.ShiftModifier) !== 0, text, composer.hasAttachment,
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
      enabled: State.canSend(input.text, composer.hasAttachment)
      opacity: enabled ? 1 : 0.4
      text: "⏎"
      tooltipText: "Send"
      onClicked: composer.send()
    }
  }
}
