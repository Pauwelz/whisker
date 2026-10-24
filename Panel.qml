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
  readonly property var view: whiskerService ? whiskerService.view : State.initialState()
  // Which linking method the pop-out shows. The user picks it, so it is view state only.
  property bool pairMode: false
  // Settings view and Log out confirmation: the user picks them, so they are view state only.
  property bool showSettings: false
  property bool confirmLogout: false
  property string savingId: "" // the Message whose Save as path is being typed, or empty
  // The Draft of the open Chat comes from the Daemon state. The Plugin changes it through Service.changeDraft.
  readonly property var draft: State.draftOf(root.view)
  readonly property string editingId: root.draft.edit // the Message under edit in the composer, or empty
  readonly property var quoting: State.draftTarget(root.view, "quote") // the Message that the next send quotes, or null
  property string reactingId: "" // the Message whose Reaction picker is open, or empty
  readonly property var settings: root.view.settings || null
  // The open Chat and its timeline come from the Daemon state; the Chat view only shows them.
  readonly property var openChat: root.view.openChat || null
  // Which parts show. The decision is State.panelParts.
  readonly property var parts: State.panelParts(root.view, { pairMode: root.pairMode, showSettings: root.showSettings })

  readonly property color contentForeground: bar ? bar.foreground : Color.foreground
  readonly property string contentFontFamily: bar ? bar.fontFamily : Style.font.family

  // Runs a fixed shortcut. The decision is State.shortcutAction.
  function runShortcut(shortcut) {
    var action = State.shortcutAction(shortcut, root.view, { showSettings: root.showSettings })
    if (action === "closeChat") {
      root.reactingId = ""
      root.whiskerService.closeChat()
    } else if (action === "closeSettings") {
      root.showSettings = false
      root.confirmLogout = false
    } else if (action === "closePopout") {
      root.close()
    } else if (action === "focusSearch") {
      searchInput.forceActiveFocus()
    }
  }

  Shortcut {
    sequence: "Ctrl+F"
    onActivated: root.runShortcut("Ctrl+F")
  }

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
      // A text field that has focus gets every key, so typing never runs a catcher action.
      blocked: composer.activeFocus || searchInput.activeFocus || root.savingId !== ""
      onCloseRequested: root.runShortcut("Escape")

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
          color: root.parts.error ? Color.urgent : root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
        }

        // "Connecting…" or "Offline" while the connection is not up.
        Text {
          visible: root.parts.banner
          width: parent.width
          text: State.connectionBanner(root.view)
          color: root.view.connection === "offline" ? Color.urgent : root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        // The Daemon sends the QR code as a data: URI.
        Image {
          visible: root.parts.qr
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
          visible: root.parts.pairForm
          width: parent.width
          color: root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
          inputMethodHints: Qt.ImhDialableCharactersOnly
          onAccepted: root.whiskerService.requestPairCode(text)
        }

        Button {
          visible: root.parts.pairForm
          text: "Get Pair Code"
          bordered: true
          onClicked: root.whiskerService.requestPairCode(phoneInput.text)
        }

        Button {
          visible: root.parts.pairToggle
          text: root.pairMode ? "Use a QR code" : "Use a Pair Code"
          bordered: true
          onClicked: root.pairMode = !root.pairMode
        }

        TextInput {
          id: searchInput
          visible: root.parts.chatList
          width: parent.width
          color: root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
          onTextChanged: root.whiskerService.searchChats(text)
          Keys.onEscapePressed: root.runShortcut("Escape")
        }

        Button {
          visible: root.parts.settingsButton
          text: root.showSettings ? "Back to Chats" : "Settings"
          bordered: true
          onClicked: {
            root.showSettings = !root.showSettings
            root.confirmLogout = false
          }
        }

        // Profile Settings. The Daemon owns the values: each button sends one change.
        Column {
          visible: root.parts.settings
          width: parent.width
          spacing: Style.space(8)

          Repeater {
            model: State.READ_RECEIPT_MODES
            Button {
              text: State.readReceiptLabel(modelData, root.settings)
              bordered: true
              onClicked: root.whiskerService.changeSettings({ readReceipts: modelData.value })
            }
          }
          Button {
            text: State.onOffLabel("Send online", root.settings && root.settings.sendOnline)
            bordered: true
            onClicked: root.whiskerService.changeSettings({ sendOnline: !root.settings.sendOnline })
          }
          Button {
            text: State.onOffLabel("Send typing", root.settings && root.settings.sendTyping)
            bordered: true
            onClicked: root.whiskerService.changeSettings({ sendTyping: !root.settings.sendTyping })
          }
          Button {
            text: State.onOffLabel("Notifications", root.settings && root.settings.notifications)
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
          visible: root.parts.chat
          width: parent.width
          spacing: Style.space(8)
          onVisibleChanged: if (visible) composer.forceActiveFocus()

          Button {
            text: "Back: " + (root.openChat ? root.openChat.name : "")
            bordered: true
            onClicked: {
              root.reactingId = ""
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
              id: messageItem
              readonly property var message: modelData
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
              // The Quote of a reply: the quoted author and a one-line preview.
              Text {
                readonly property var quote: State.quoteView(modelData)
                visible: quote !== null
                width: parent.width
                elide: Text.ElideRight
                textFormat: Text.PlainText
                text: quote ? quote.sender + ": " + quote.text : ""
                opacity: 0.6
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.caption
              }
              Text {
                visible: text !== ""
                width: parent.width
                wrapMode: Text.Wrap
                textFormat: Text.StyledText
                text: State.messageHtml(modelData)
                onLinkActivated: link => root.whiskerService.openLink(link)
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
                  color: State.mediaFailed(modelData) ? Color.urgent : root.contentForeground
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.caption
                }
                Button {
                  visible: State.canDownloadMedia(modelData)
                  text: State.downloadLabel(modelData)
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
              Text {
                visible: text !== ""
                text: State.reactionChips(modelData).map(function (c) { return c.emoji + " " + c.count }).join("  ")
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
              }
              // The Reaction picker: the six default emoji. Picking your current Reaction removes it.
              Row {
                visible: root.reactingId === modelData.id
                spacing: Style.space(4)
                Repeater {
                  model: State.REACTION_EMOJI
                  Button {
                    text: modelData
                    bordered: State.myReaction(messageItem.message) === modelData
                    onClicked: {
                      root.whiskerService.reactToMessage(messageItem.message, modelData)
                      root.reactingId = ""
                    }
                  }
                }
              }
              Row {
                spacing: Style.space(8)
                Text {
                  visible: text !== ""
                  text: State.deliveryLabel(modelData)
                  color: State.canRetryMessage(modelData) ? Color.urgent : root.contentForeground
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
                  visible: State.canReactToMessage(modelData)
                  text: "React"
                  bordered: true
                  onClicked: root.reactingId = root.reactingId === modelData.id ? "" : modelData.id
                }
                Button {
                  visible: State.canQuoteMessage(modelData)
                  text: "Quote"
                  bordered: true
                  onClicked: {
                    root.whiskerService.changeDraft({ quote: modelData.id })
                    composer.forceActiveFocus()
                  }
                }
                Button {
                  visible: State.canEditMessage(modelData, Math.floor(Date.now() / 1000))
                  text: "Edit"
                  bordered: true
                  onClicked: {
                    root.whiskerService.changeDraft({ edit: modelData.id, text: modelData.text })
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
            onClicked: root.whiskerService.changeDraft({ edit: "", text: "" })
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

          Text {
            readonly property var quote: State.composerQuoteView(root.quoting)
            visible: quote !== null
            width: parent.width
            elide: Text.ElideRight
            textFormat: Text.PlainText
            text: quote ? "Replying to " + quote.sender + ": " + quote.text : ""
            color: root.contentForeground
            font.family: root.contentFontFamily
            font.pixelSize: Style.font.caption
          }

          Button {
            visible: root.quoting !== null
            text: "Cancel quote"
            bordered: true
            onClicked: root.whiskerService.changeDraft({ quote: "" })
          }

          TextEdit {
            id: composer
            width: parent.width
            wrapMode: TextEdit.Wrap
            color: root.contentForeground
            font.family: root.contentFontFamily
            font.pixelSize: Style.font.body
            onTextChanged: root.whiskerService.composerChanged(text)
            readonly property string draftText: root.draft.text
            onDraftTextChanged: if (text !== draftText) text = draftText
            Keys.onEscapePressed: root.runShortcut("Escape")
            Keys.onPressed: function(event) {
              var action = State.composerAction(event.key, (event.modifiers & Qt.ShiftModifier) !== 0, text)
              if (action === "default") return
              event.accepted = true
              if (action === "send") {
                root.whiskerService.submitComposer(text)
              }
            }
          }
        }

        // The Chat list: the Daemon sends the Chats in order, with the name and the marks.
        ListView {
          visible: root.parts.chatList
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
          visible: root.parts.loadMoreChats
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
