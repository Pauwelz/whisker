import QtQuick
import qs.Commons
import qs.Ui
import "WhiskerState.js" as State

// The bar widget entry point: a bar icon and its pop-out. It reads the Daemon state from the Service.
Panel {
  id: root
  moduleName: "pauwelz.whisker"
  ipcTarget: "pauwelz.whisker"
  manageIpc: false // the handler below adds openChat to the standard methods

  readonly property var whiskerService: bar && bar.shell ? bar.shell.serviceFor("pauwelz.whisker") : null
  readonly property var view: whiskerService ? whiskerService.view : ({ screen: "starting", message: "" })
  // Which linking method the pop-out shows. The user picks it, so it is view state only.
  property bool pairMode: false
  // Settings view and Log out confirmation: the user picks them, so they are view state only.
  property bool showSettings: false
  property bool confirmLogout: false
  property string savingId: "" // the Message whose Save as path is being typed, or empty
  property string editingId: "" // the Message under edit in the composer, or empty
  readonly property var settings: root.view.settings || null
  // The open Chat and its timeline come from the Daemon state; the Chat view only shows them.
  readonly property var openChat: root.view.openChat || null

  readonly property color contentForeground: bar ? bar.foreground : Color.foreground
  readonly property string contentFontFamily: bar ? bar.fontFamily : Style.font.family

  onOpenedChanged: if (whiskerService) whiskerService.setPopoutOpen(opened)

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  ShellIpc {
    target: root.ipcTarget

    function open(): void { root.open() }
    function close(): void { root.close() }
    function show(): void { root.open() }
    function hide(): void { root.close() }
    function toggle(): void { root.toggle() }
    function openChat(jid: string): void {
      if (root.whiskerService) root.whiskerService.openChatByJid(jid)
      root.open()
    }
  }

  BarIconButton {
    id: button
    labelText: State.unreadLabel(root.view)
    anchors.fill: parent
    bar: root.bar
    text: ""
    dimmed: State.barDimmed(root.view)
    tooltipText: root.view.message
    onPressed: function(b) { root.toggle() }
  }

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    centerOnBar: true
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(380))
    contentHeight: panel.fittedContentHeight(column.implicitHeight)

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onCloseRequested: root.close()

      Column {
        id: column
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        spacing: Style.space(8)

        Text {
          id: message
          width: parent.width
          wrapMode: Text.Wrap
          textFormat: Text.PlainText
          text: root.view.message
          color: root.view.screen === "error" ? Color.urgent : root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
        }

        // "Connecting…" or "Offline" while the connection is not up.
        Text {
          visible: root.view.screen === "linked" && State.connectionBanner(root.view) !== ""
          width: parent.width
          text: State.connectionBanner(root.view)
          color: root.view.connection === "offline" ? Color.urgent : root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        // The Daemon sends the QR code as a data: URI.
        Image {
          visible: root.view.screen === "linking" && !!root.view.qr && !root.pairMode
          source: root.view.qr || ""
          width: Style.space(232)
          height: width
          fillMode: Image.PreserveAspectFit
          smooth: false
        }

        Text {
          visible: !!root.view.pairCode
          text: root.view.pairCode || ""
          color: root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body * 2
        }

        TextInput {
          id: phoneInput
          visible: root.view.screen === "linking" && root.pairMode
          width: parent.width
          color: root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
          inputMethodHints: Qt.ImhDialableCharactersOnly
          onAccepted: root.whiskerService.requestPairCode(text)
        }

        Button {
          visible: root.view.screen === "linking" && root.pairMode
          text: "Get Pair Code"
          bordered: true
          onClicked: root.whiskerService.requestPairCode(phoneInput.text)
        }

        Button {
          visible: root.view.screen === "linking"
          text: root.pairMode ? "Use a QR code" : "Use a Pair Code"
          bordered: true
          onClicked: root.pairMode = !root.pairMode
        }

        TextInput {
          id: searchInput
          visible: root.view.screen === "linked" && !root.showSettings && !root.openChat
          width: parent.width
          color: root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
          onTextChanged: root.whiskerService.searchChats(text)
        }

        Button {
          visible: root.view.screen === "linked" && !!root.settings && !root.openChat
          text: root.showSettings ? "Back to Chats" : "Settings"
          bordered: true
          onClicked: {
            root.showSettings = !root.showSettings
            root.confirmLogout = false
          }
        }

        // Profile Settings. The Daemon owns the values: each button sends one change.
        Column {
          visible: root.view.screen === "linked" && root.showSettings && !!root.settings
          width: parent.width
          spacing: Style.space(8)

          Repeater {
            model: State.READ_RECEIPT_MODES
            Button {
              text: "Read Receipts: " + modelData.label + (root.settings && root.settings.readReceipts === modelData.value ? " (selected)" : "")
              bordered: true
              onClicked: root.whiskerService.changeSettings({ readReceipts: modelData.value })
            }
          }
          Button {
            text: "Send online: " + (root.settings && root.settings.sendOnline ? "On" : "Off")
            bordered: true
            onClicked: root.whiskerService.changeSettings({ sendOnline: !root.settings.sendOnline })
          }
          Button {
            text: "Send typing: " + (root.settings && root.settings.sendTyping ? "On" : "Off")
            bordered: true
            onClicked: root.whiskerService.changeSettings({ sendTyping: !root.settings.sendTyping })
          }
          Button {
            text: "Notifications: " + (root.settings && root.settings.notifications ? "On" : "Off")
            bordered: true
            onClicked: root.whiskerService.changeSettings({ notifications: !root.settings.notifications })
          }
          Button {
            visible: !root.confirmLogout
            text: "Log out"
            bordered: true
            onClicked: root.confirmLogout = true
          }
          Text {
            visible: root.confirmLogout
            width: parent.width
            wrapMode: Text.Wrap
            text: "Log out removes this device from your phone and deletes your local chats."
            color: root.contentForeground
            font.family: root.contentFontFamily
            font.pixelSize: Style.font.body
          }
          Button {
            visible: root.confirmLogout
            text: "Confirm Log out"
            bordered: true
            onClicked: {
              root.confirmLogout = false
              root.showSettings = false
              root.whiskerService.logout()
            }
          }
          Button {
            visible: root.confirmLogout
            text: "Cancel"
            bordered: true
            onClicked: root.confirmLogout = false
          }
        }

        // The open Chat: its timeline, newest at the bottom, and the composer. Enter sends, Shift+Enter adds a line.
        Column {
          visible: root.view.screen === "linked" && !root.showSettings && !!root.openChat
          width: parent.width
          spacing: Style.space(8)
          onVisibleChanged: if (visible) composer.forceActiveFocus()

          Button {
            text: "Back: " + (root.openChat ? root.openChat.name : "")
            bordered: true
            onClicked: {
              root.editingId = ""
              root.whiskerService.closeChat()
            }
          }

          // Presence of the other side: Online or Last Seen, and "typing…" or "recording…".
          Text {
            visible: text !== ""
            width: parent.width
            textFormat: Text.PlainText
            text: State.chatStatusLabel(root.view, Math.floor(Date.now() / 1000))
            color: root.contentForeground
            opacity: 0.6
            font.family: root.contentFontFamily
            font.pixelSize: Style.font.caption
          }

          Button {
            visible: !!root.openChat && root.openChat.moreMessages === true
            text: "Load older"
            bordered: true
            onClicked: root.whiskerService.loadOlderMessages()
          }

          ListView {
            width: parent.width
            height: Style.space(260)
            clip: true
            verticalLayoutDirection: ListView.BottomToTop
            model: root.openChat ? root.openChat.messages.slice().reverse() : []
            delegate: Column {
              width: ListView.view.width
              spacing: Style.space(2)
              Text {
                visible: text !== ""
                text: State.senderLabel(root.openChat, modelData)
                textFormat: Text.PlainText
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
                font.bold: true
              }
              Text {
                visible: text !== ""
                width: parent.width
                wrapMode: Text.Wrap
                textFormat: Text.PlainText
                text: State.messageBody(modelData)
                font.italic: modelData.revoked === true
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
              }
              // Media: an image or sticker shows here once the Daemon holds it. The path comes from media.open, never bytes.
              Image {
                readonly property string file: State.mediaPath(root.view, modelData)
                visible: file !== "" && State.showsInline(modelData)
                source: visible ? "file://" + file : ""
                width: Math.min(parent.width, Style.space(240))
                fillMode: Image.PreserveAspectFit
                asynchronous: true
                TapHandler { onTapped: root.whiskerService.openMedia(modelData) }
              }
              Row {
                visible: !!modelData.media
                spacing: Style.space(8)
                Text {
                  text: State.mediaLabel(modelData) + (State.mediaStatus(modelData) !== "" ? " - " + State.mediaStatus(modelData) : "")
                  textFormat: Text.PlainText
                  color: modelData.media && modelData.media.state === "failed" ? Color.urgent : root.contentForeground
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.caption
                }
                Button {
                  visible: State.canDownloadMedia(modelData)
                  text: modelData.media && modelData.media.state === "failed" ? "Retry download" : "Download"
                  bordered: true
                  onClicked: root.whiskerService.downloadMedia(modelData)
                }
                Button {
                  visible: State.canOpenMedia(modelData)
                  text: State.openLabel(modelData, root.whiskerService.playingKey)
                  bordered: true
                  onClicked: root.whiskerService.openMedia(modelData)
                }
                Button {
                  visible: State.canOpenMedia(modelData)
                  text: "Save as"
                  bordered: true
                  onClicked: root.savingId = modelData.id
                }
              }
              // Save as: type an absolute path, then press Enter. Escape cancels.
              TextEdit {
                visible: root.savingId === modelData.id
                width: parent.width
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
                onVisibleChanged: if (visible) forceActiveFocus()
                Keys.onPressed: function(event) {
                  if (event.key === Qt.Key_Escape) {
                    root.savingId = ""
                  } else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
                    root.whiskerService.saveMedia(modelData, text.trim())
                    root.savingId = ""
                    text = ""
                  } else {
                    return
                  }
                  event.accepted = true
                }
              }
              Row {
                spacing: Style.space(8)
                Text {
                  visible: text !== ""
                  text: State.deliveryLabel(modelData)
                  color: modelData.state === "failed" ? Color.urgent : root.contentForeground
                  opacity: 0.6
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.caption
                }
                Text {
                  visible: text !== ""
                  text: State.editedLabel(modelData)
                  color: root.contentForeground
                  opacity: 0.6
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.caption
                }
                Button {
                  visible: State.canEditMessage(modelData, Math.floor(Date.now() / 1000))
                  text: "Edit"
                  bordered: true
                  onClicked: {
                    root.editingId = modelData.id
                    composer.text = modelData.text
                    composer.forceActiveFocus()
                  }
                }
                Button {
                  visible: State.canRevokeMessage(modelData)
                  text: "Delete"
                  bordered: true
                  onClicked: root.whiskerService.revokeMessage(modelData.id)
                }
                Button {
                  visible: State.canRetryMessage(modelData)
                  text: "Retry"
                  bordered: true
                  onClicked: root.whiskerService.retryMessage(modelData.id)
                }
              }
            }
          }

          Button {
            visible: root.editingId !== ""
            text: "Cancel edit"
            bordered: true
            onClicked: {
              root.editingId = ""
              composer.text = ""
            }
          }

          // The picker loads on demand, see MediaPicker.qml. The composer text becomes the caption.
          Loader {
            id: picker
            active: false
            source: "MediaPicker.qml"
            onLoaded: item.picked.connect(function(file) {
              root.whiskerService.sendMedia(file, composer.text)
              composer.text = ""
            })
          }
          Button {
            text: "Attach"
            bordered: true
            onClicked: { picker.active = false; picker.active = true }
          }

          TextEdit {
            id: composer
            width: parent.width
            wrapMode: TextEdit.Wrap
            color: root.contentForeground
            font.family: root.contentFontFamily
            font.pixelSize: Style.font.body
            onTextChanged: root.whiskerService.composerChanged(text)
            Keys.onPressed: function(event) {
              var action = State.composerAction(event.key, (event.modifiers & Qt.ShiftModifier) !== 0, text)
              if (action === "default") return
              event.accepted = true
              if (action === "send") {
                root.whiskerService.submitComposer(root.editingId, text)
                root.editingId = ""
                text = ""
              }
            }
          }
        }

        // The Chat list: the Daemon sends the Chats in order, with the name and the marks.
        ListView {
          visible: root.view.screen === "linked" && !root.showSettings && !root.openChat
          width: parent.width
          height: visible ? Style.space(320) : 0
          clip: true
          model: root.view.chats || []
          delegate: Row {
            width: ListView.view.width
            spacing: Style.space(8)
            TapHandler { onTapped: root.whiskerService.openChat(modelData) }
            Text {
              text: modelData.unread ? "●" : " "
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
            }
            Text {
              width: parent.width - Style.space(100)
              elide: Text.ElideRight
              textFormat: Text.PlainText
              text: modelData.name
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
              font.bold: modelData.unread
            }
            Text {
              text: (modelData.pinned ? "pinned " : "") + (modelData.muted ? "muted" : "")
              color: root.contentForeground
              opacity: 0.6
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
            }
          }
        }

        Button {
          visible: root.view.screen === "linked" && root.view.moreChats === true && !root.openChat
          text: "Load more"
          bordered: true
          onClicked: root.whiskerService.loadMoreChats()
        }

        Button {
          id: retryButton
          visible: root.view.canRetry === true
          text: "Retry"
          bordered: true
          onClicked: root.whiskerService.retry()
        }
      }
    }
  }
}
