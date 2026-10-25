import QtQuick
import qs.Commons
import qs.Ui
import "WhiskerState.js" as State

// The composer of the open Chat: one strip under a line with a prompt, the text, Attach and Send.
// Enter sends, Shift+Enter adds a line. The pop-out (Panel.qml) owns the Draft; this item only draws it.
FocusScope {
  id: composer
  property var panel: null // the pop-out: service, Draft and colours
  readonly property var service: panel ? panel.whiskerService : null
  readonly property var draft: panel ? panel.draft : ({ text: "", attachment: "" })
  readonly property bool hasAttachment: !!draft.attachment
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
      width: row.width - x - attach.width - sendButton.width - row.spacing * 2
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
        var action = State.composerAction(event.key, (event.modifiers & Qt.ShiftModifier) !== 0, text, composer.hasAttachment)
        if (action === "default") return
        event.accepted = true
        if (action === "send") composer.service.submitComposer(text)
      }

      Text {
        visible: input.text === ""
        text: "type a message · ⏎ send"
        color: input.color
        opacity: 0.5
        font: input.font
      }
    }

    // The picker loads on demand, see MediaPicker.qml. The picked file waits in the Draft; the text becomes its caption.
    Loader {
      id: picker
      active: false
      source: "MediaPicker.qml"
      onLoaded: item.picked.connect(function(file) { composer.service.stageAttachment(file) })
    }
    Button {
      id: attach
      anchors.verticalCenter: parent.verticalCenter
      enabled: State.canAttach(composer.panel ? composer.panel.editingId : "")
      opacity: enabled ? 1 : 0.4
      text: "📎"
      tooltipText: "Attach a file"
      onClicked: { picker.active = false; picker.active = true }
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
