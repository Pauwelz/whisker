import QtQuick
import qs.Commons

// One link of a link row (Chat links, Message links, Media links, Question answers): grey, underlined on hover.
Text {
  id: link
  signal clicked()
  textFormat: Text.PlainText
  color: Color.muted
  font.pixelSize: Style.font.body
  font.underline: hover.hovered
  HoverHandler { id: hover; cursorShape: Qt.PointingHandCursor }
  TapHandler { onTapped: link.clicked() }
}
