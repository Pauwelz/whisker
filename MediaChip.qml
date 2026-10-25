import QtQuick
import QtQuick.Effects
import qs.Commons
import qs.Ui
import "state/Media.js" as Media

// The compact chip of a Message's Media: thumbnail or file-type badge, label, and the links.
// The decisions (main action, links, badge, blur, dot) come from the state/ modules; this item only draws them.
Column {
  id: chip
  required property var message
  required property var panel // the pop-out: service, colours and the Save as state
  readonly property var service: panel.whiskerService
  readonly property string thumbnail: Media.thumbnailSource(message, Media.mediaPath(panel.view, message))
  readonly property string badge: Media.fileBadge(message)
  readonly property bool failed: Media.mediaFailed(message)
  readonly property bool voice: Media.isVoice(message)
  readonly property color tone: failed ? Color.urgent : panel.contentForeground
  spacing: Style.space(2)

  // Runs one action of Message.actionsFor. Reply, react and the rest of the rule belong to #53.
  function run(action) {
    if (action === "saveAs") panel.savingId = message.id
    else if (action === "download") service.downloadMedia(message)
    else if (action === "open" || action === "play") service.openMedia(message)
  }

  Row {
    spacing: Style.space(8)

    // Thumbnail, or the file-type badge when there is none. A click runs the main action.
    Item {
      id: preview
      width: Style.space(56)
      height: Style.space(36)
      visible: chip.thumbnail !== "" || chip.badge !== ""
      Image {
        id: thumb
        anchors.fill: parent
        visible: chip.thumbnail !== "" && !Media.chipBlurred(chip.message)
        source: chip.thumbnail
        fillMode: Image.PreserveAspectCrop
        sourceSize: Qt.size(Style.space(112), Style.space(72)) // a downloaded file is decoded small, twice the box for a scaled screen
        asynchronous: true
      }
      MultiEffect {
        anchors.fill: thumb
        source: thumb
        visible: chip.thumbnail !== "" && Media.chipBlurred(chip.message)
        blurEnabled: true
        blurMax: 32
        blur: 1.0
      }
      Rectangle {
        anchors.fill: parent
        visible: chip.thumbnail === ""
        color: "transparent"
        border.color: chip.tone
        border.width: 1
        opacity: 0.6
        Text {
          anchors.centerIn: parent
          text: chip.badge
          textFormat: Text.PlainText
          color: chip.tone
          font.family: chip.panel.contentFontFamily
          font.pixelSize: Style.font.caption
          font.bold: true
        }
      }
      TapHandler { onTapped: chip.run(Media.chipMainAction(chip.message)) }
    }

    Column {
      spacing: Style.space(2)
      Row {
        spacing: Style.space(6)
        // The unplayed dot of a voice note.
        Text {
          visible: Media.unplayedDot(chip.message)
          text: "●"
          color: Color.accent
          font.pixelSize: Style.font.caption
        }
        // The play button of a voice note: Play or Stop.
        Button {
          visible: chip.voice && Media.canOpenMedia(chip.message)
          text: Media.openLabel(chip.message, chip.service.playingKey)
          bordered: true
          onClicked: chip.service.openMedia(chip.message)
        }
        // The label: kind, name and size. A click runs the main action. A failed download shows in red.
        Text {
          text: Media.mediaLabel(chip.message) + (Media.mediaStatus(chip.message) !== "" ? " - " + Media.mediaStatus(chip.message) : "")
          textFormat: Text.PlainText
          color: chip.tone
          font.family: chip.panel.contentFontFamily
          font.pixelSize: Style.font.caption
          TapHandler { onTapped: chip.run(Media.chipMainAction(chip.message)) }
        }
      }
      // Download progress: the Daemon sends no percentage, so the bar is indeterminate.
      Rectangle {
        visible: Media.isDownloading(chip.message)
        width: Style.space(120)
        height: Style.space(3)
        color: Color.accent
        opacity: 0.4
        SequentialAnimation on opacity {
          running: parent.visible
          loops: Animation.Infinite
          NumberAnimation { to: 1; duration: 600 }
          NumberAnimation { to: 0.3; duration: 600 }
        }
      }
      Row {
        spacing: Style.space(8)
        Repeater {
          model: Media.chipLinks(chip.message)
          Text {
            required property var modelData
            text: modelData.label
            textFormat: Text.PlainText
            color: chip.failed && modelData.action === "download" ? Color.urgent : Color.accent
            font.underline: true
            font.family: chip.panel.contentFontFamily
            font.pixelSize: Style.font.caption
            TapHandler { onTapped: chip.run(modelData.action) }
          }
        }
      }
    }
  }
}
