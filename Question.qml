import QtQuick
import qs.Commons

// A question with its answers as links, in the style of the Chat links (ChatLinks.qml). The text wraps, so a long
// question stays inside the pop-out. Each answer is { label, run }.
Column {
  id: question
  property var panel: null // the pop-out: colours and font
  property string text: ""
  property var answers: []
  spacing: Style.space(2)

  Text {
    width: question.width
    wrapMode: Text.Wrap
    textFormat: Text.PlainText
    text: question.text
    color: question.panel ? question.panel.contentForeground : Color.foreground
    font.family: question.panel ? question.panel.contentFontFamily : Style.font.family
    font.pixelSize: Style.font.body
  }
  Row {
    spacing: Style.space(8)
    Repeater {
      model: question.answers
      Link {
        required property var modelData
        text: modelData.label
        font.family: question.panel ? question.panel.contentFontFamily : Style.font.family
        onClicked: modelData.run()
      }
    }
  }
}
