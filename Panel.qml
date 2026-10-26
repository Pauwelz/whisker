import QtQuick
import QtQuick.Effects
import QtQuick.Layouts
import qs.Commons
import qs.Ui
import "state/ChatList.js" as ChatList
import "state/Daemon.js" as Daemon
import "state/Draft.js" as Draft
import "state/Message.js" as Message
import "state/NewChat.js" as NewChat
import "state/Timeline.js" as Timeline

// The bar widget entry point: a bar icon and its pop-out. It reads the Daemon state from the Service.
Panel {
  id: root
  moduleName: "pauwelz.whisker"
  ipcTarget: "pauwelz.whisker"
  manageIpc: false // the handler below adds openChat to the standard methods

  readonly property var whiskerService: bar && bar.shell ? bar.shell.serviceFor("pauwelz.whisker") : null
  readonly property var view: whiskerService ? whiskerService.view : Daemon.initialState()
  // Which linking method the pop-out shows. The user picks it, so it is view state only.
  property bool pairMode: false
  // Settings view and Log out confirmation: the user picks them, so they are view state only.
  property bool showSettings: false
  property bool confirmLogout: false
  property bool showNewChat: false // the new Chat view: the user picks it, so it is view state only
  property string chatFilter: "all" // the Chat list filter the user picked: all, unread, groups or archived
  readonly property var shownChats: ChatList.filterChats(root.view.chats || [], root.chatFilter)
  onShownChatsChanged: chatListView.showChats()
  property string savingId: "" // the Message whose Save as path is being typed, or empty
  // The Draft of the open Chat comes from the Daemon state. The Plugin changes it through Service.changeDraft.
  readonly property var draft: Draft.draftOf(root.view)
  readonly property string editingId: root.draft.edit // the Message under edit in the composer, or empty
  readonly property var quoting: Draft.draftTarget(root.view, "quote") // the Message that the next send quotes, or null
  property string reactingId: "" // the Message whose Reaction picker is open, or empty
  property string chatLinksFor: "" // the jid of the Chat whose links show in the Chat header, or empty
  // The open Chat as the Chat list holds it, for the pin and mute state of its links. Null when a search hides it.
  readonly property var openChatRow: root.openChat ? ChatList.chatRow(root.view, root.openChat.jid) : null
  property string menuId: "" // the Message whose action menu is open, or empty
  // Not `settings`: the base Panel owns that name, and the bar writes its shell.json settings into it.
  readonly property var profileSettings: root.view.settings || null
  // The open Chat and its timeline come from the Daemon state; the Chat view only shows them.
  readonly property var openChat: root.view.openChat || null
  // Keyed on the jid: the Daemon replaces the openChat object on every Draft edit and Message.
  readonly property string openJid: root.openChat ? root.openChat.jid : ""
  onOpenJidChanged: { // also after mark unread, which closes the Chat
    closeOverlays()
    chatLinksFor = ""
  }
  // Only a new messages array rebuilds the timeline; a Draft edit keeps the same array.
  readonly property var openMessages: root.openChat ? root.openChat.messages : null
  readonly property var timeline: root.openMessages ? Timeline.timelineRows(root.openMessages) : [] // oldest first
  onTimelineChanged: timelineView.showTimeline()
  // Which parts show. The decision is Daemon.panelParts.
  readonly property var parts: Daemon.panelParts(root.view, { pairMode: root.pairMode, showSettings: root.showSettings, showNewChat: root.showNewChat })

  // The pop-out keeps one size in every view, so it never grows or shrinks on a view switch.
  readonly property real popoutHeight: Style.space(560)

  readonly property color contentForeground: bar ? bar.foreground : Color.foreground
  // The bars: the foreground colour at low opacity over the pop-out background.
  readonly property color surfaceColor: Qt.rgba(contentForeground.r, contentForeground.g, contentForeground.b, 0.06)
  readonly property string contentFontFamily: bar ? bar.fontFamily : Style.font.family

  // One on/off Profile Setting. `key` is its wire name; Daemon.SETTING_TOGGLES holds its texts.
  // Smaller than the shell's Toggle (at least 54 px), so all settings fit the pop-out without a scroll.
  component SettingToggle: Toggle {
    required property string key
    property bool available: true // false greys the switch out, as for a setting that needs another one
    enabled: available
    opacity: available ? 1 : 0.4
    width: parent.width
    implicitHeight: Style.space(46)
    titleSize: Style.font.body
    label: Daemon.SETTING_TOGGLES[key].label
    description: Daemon.SETTING_TOGGLES[key].description
    checked: !!root.profileSettings && root.profileSettings[key]
    foreground: root.contentForeground
    fontFamily: root.contentFontFamily
    onClicked: root.whiskerService.changeSettings({ [key]: !root.profileSettings[key] })
  }

  // A setting with a few choices: its label and the buttons on one line.
  component SettingChoice: Row {
    property alias label: choiceLabel.text
    property alias options: choices.options
    property alias value: choices.value
    signal changed(string choice)
    spacing: Style.space(12)
    Text {
      id: choiceLabel
      anchors.verticalCenter: parent.verticalCenter
      color: root.contentForeground
      font.family: root.contentFontFamily
      font.pixelSize: Style.font.body
      font.bold: true
    }
    ButtonGroup {
      id: choices
      foreground: root.contentForeground
      fontFamily: root.contentFontFamily
      onChanged: function(choice) { parent.changed(choice) }
    }
  }

  // Runs a fixed shortcut. The decision is Daemon.shortcutAction.
  function runShortcut(shortcut) {
    var action = Daemon.shortcutAction(shortcut, root.view, { showSettings: root.showSettings, showNewChat: root.showNewChat })
    if (action === "closeChat") {
      root.whiskerService.closeChat()
    } else if (action === "closeSettings") {
      root.showSettings = false
      root.confirmLogout = false
    } else if (action === "closeNewChat") {
      root.showNewChat = false
    } else if (action === "closePopout") {
      root.close()
    } else if (action === "focusSearch") {
      searchInput.forceActiveFocus()
    }
  }

  // Closes the Message overlays: the Reaction picker, the menu and the Save as field. Open one only after this.
  function closeOverlays() {
    root.reactingId = ""
    root.menuId = ""
    root.savingId = ""
  }

  // Called by MessageLine, which cannot reach the composer.
  function focusComposer() { composer.forceActiveFocus() }

  // A click on a Quote: scroll the timeline to the quoted Message. The model is newest first, so the row index flips.
  function jumpToMessage(id) {
    var index = Timeline.rowIndexOf(root.timeline, id)
    if (index >= 0) timelineView.positionViewAtIndex(root.timeline.length - 1 - index, ListView.Center)
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
    function openChat(token: string): void {
      if (root.whiskerService) root.whiskerService.openNotification(token)
      root.open()
    }
  }

  BarIconButton {
    id: button
    labelText: Daemon.unreadLabel(root.view)
    anchors.fill: parent
    bar: root.bar
    // One-colour cat. MultiEffect paints it in the bar foreground, so it follows the theme.
    iconComponent: Component {
      Image {
        source: Qt.resolvedUrl("assets/whisker-bar.svg")
        sourceSize.height: button.opticalSize * Screen.devicePixelRatio
        fillMode: Image.PreserveAspectFit
        layer.enabled: true
        layer.effect: MultiEffect {
          colorization: 1.0
          colorizationColor: button.foreground
        }
      }
    }
    dimmed: Daemon.barDimmed(root.view)
    tooltipText: root.view.message
    onPressed: function(b) { root.toggle() }
  }

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    centerOnBar: false // open under the bar button, as other plugins do
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(480))
    contentHeight: panel.fittedContentHeight(root.popoutHeight)

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

        // Top bar: the small logo, the name, "● online", the unread count, "+ new chat" and "settings". The Chat view and the new Chat view have their own bar.
        Rectangle {
          visible: !root.parts.chat && !root.parts.newChat
          width: parent.width
          height: headerRow.implicitHeight + Style.space(8)
          color: root.surfaceColor
          RowLayout {
            id: headerRow
            anchors.fill: parent
            anchors.leftMargin: Style.space(8)
            anchors.rightMargin: Style.space(4)
            spacing: Style.space(8)
            Image {
              source: Qt.resolvedUrl("assets/whisker.svg")
              Layout.preferredWidth: Style.space(16)
              Layout.preferredHeight: Style.space(16)
              sourceSize.width: Style.space(16) * Screen.devicePixelRatio
              sourceSize.height: Style.space(16) * Screen.devicePixelRatio
            }
            Text {
              text: "whisker"
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
              font.bold: true
            }
            Text {
              visible: Daemon.ownOnline(root.view)
              text: "● online"
              color: Color.accent
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
            }
            Text {
              Layout.fillWidth: true
              textFormat: Text.PlainText
              text: Daemon.unreadSuffix(root.view)
              opacity: 0.5
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
            }
            // The switch between the QR code and the Pair Code, while linking.
            Button {
              visible: root.parts.pairToggle
              text: root.pairMode ? "qr code" : "pair code"
              tooltipText: root.pairMode ? "Link with a QR code" : "Link with a Pair Code"
              onClicked: root.pairMode = !root.pairMode
            }
            Button {
              visible: root.parts.newChatButton
              text: "+ new chat"
              onClicked: root.showNewChat = true
            }
            // "settings" turns into "←" in its own place, so the bar does not shift.
            Button {
              visible: root.parts.settingsButton
              text: root.showSettings ? "←" : "settings"
              tooltipText: root.showSettings ? "Back to Chats" : ""
              onClicked: {
                root.showSettings = !root.showSettings
                root.confirmLogout = false
              }
            }
          }
        }

        Text {
          id: message
          visible: Daemon.statusLineShown(root.view)
          width: parent.width
          wrapMode: Text.Wrap
          textFormat: Text.PlainText
          text: Daemon.linkMessage(root.view, root.pairMode)
          color: root.parts.error ? Color.urgent : root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
        }

        // The notice of an error code, for example "Could not reach WhatsApp". It goes away by itself.
        Text {
          visible: text !== ""
          width: parent.width
          wrapMode: Text.Wrap
          textFormat: Text.PlainText
          text: root.view.notice || ""
          color: Color.urgent
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
          onTextChanged: if (text !== "") noticeTimer.restart()
        }
        Timer {
          id: noticeTimer
          interval: 4000
          onTriggered: root.whiskerService.dismissNotice()
        }

        // "Connecting…" or "Offline" while the connection is not up.
        Text {
          visible: root.parts.banner
          width: parent.width
          text: Daemon.connectionBanner(root.view)
          color: root.view.connection === "offline" ? Color.urgent : root.contentForeground
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        // The Daemon sends the QR code as a data: URI, at error correction level Q.
        // Q restores a quarter of the code, so the logo in the middle (under 5% of the area) keeps it readable.
        Image {
          id: qrCode
          anchors.horizontalCenter: parent.horizontalCenter
          visible: root.parts.qr
          source: root.view.qr || ""
          width: Math.min(parent.width, Style.space(360))
          height: width
          fillMode: Image.PreserveAspectFit
          smooth: false
          // A white ring keeps the logo apart from the modules around it.
          Rectangle {
            anchors.centerIn: parent
            width: parent.width * 0.22
            height: width
            radius: width * 0.25
            color: "white"
            Image {
              anchors.fill: parent
              anchors.margins: Style.space(3)
              source: Qt.resolvedUrl("assets/whisker.svg")
              sourceSize: Qt.size(width * Screen.devicePixelRatio, height * Screen.devicePixelRatio)
              fillMode: Image.PreserveAspectFit
            }
          }
        }

        // The Pair Code form: a prompt strip like the composer. Enter asks the Daemon for the Pair Code.
        Item {
          visible: root.parts.pairForm
          width: parent.width
          implicitHeight: phoneRow.implicitHeight + Style.space(8)
          Rectangle {
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.top: parent.top
            height: 1
            color: Color.muted
          }
          Rectangle {
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.bottom: parent.bottom
            height: 1
            color: Color.muted
          }
          Row {
            id: phoneRow
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
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
            }
            TextInput {
              id: phoneInput
              anchors.verticalCenter: parent.verticalCenter
              width: phoneRow.width - x - getCode.width - phoneRow.spacing
              focus: root.parts.pairForm
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
              inputMethodHints: Qt.ImhDialableCharactersOnly
              onAccepted: root.whiskerService.requestPairCode(text)
              Text {
                visible: phoneInput.text === ""
                text: "phone number with country code · ⏎ get code"
                color: phoneInput.color
                opacity: 0.5
                font: phoneInput.font
              }
            }
            Button {
              id: getCode
              anchors.verticalCenter: parent.verticalCenter
              enabled: phoneInput.text !== ""
              opacity: enabled ? 1 : 0.4
              text: "⏎"
              tooltipText: "Get Pair Code"
              onClicked: root.whiskerService.requestPairCode(phoneInput.text)
            }
          }
        }

        // The Pair Code, large and spaced, so it is easy to type on the phone.
        Text {
          anchors.horizontalCenter: parent.horizontalCenter
          visible: root.parts.pairCode
          topPadding: Style.space(16)
          bottomPadding: Style.space(16)
          text: root.view.pairCode || ""
          color: Color.accent
          font.family: root.contentFontFamily
          font.pixelSize: Style.font.body * 3
          font.bold: true
          font.letterSpacing: Style.space(4)
        }


        // The search row: a glyph, the search field and the filters. A line below it.
        RowLayout {
          visible: root.parts.chatList
          width: parent.width - Style.space(16)
          x: Style.space(8)
          spacing: Style.space(8)
          Text {
            text: "󰍉"
            opacity: 0.6
            color: Color.accent
            font.family: root.contentFontFamily
            font.pixelSize: Style.font.body
          }
          TextInput {
            id: searchInput
            Layout.fillWidth: true
            color: root.contentForeground
            font.family: root.contentFontFamily
            font.pixelSize: Style.font.body
            property string lastText: ""
            onTextChanged: {
              var capped = Draft.capText(text, lastText)
              if (!capped.ok) {
                root.whiskerService.textRefused()
                text = capped.text
                return
              }
              lastText = text
              root.whiskerService.searchChats(text)
            }
            Keys.onEscapePressed: root.runShortcut("Escape")
            Text {
              visible: searchInput.text === ""
              text: "search chats"
              opacity: 0.5
              color: searchInput.color
              font: searchInput.font
            }
          }
          Repeater {
            model: ChatList.CHAT_FILTERS
            Button {
              text: modelData.label
              selected: root.chatFilter === modelData.value
              onClicked: {
                const archivedChanged = ChatList.archivedFilterChanged(root.chatFilter, modelData.value)
                root.chatFilter = modelData.value
                if (archivedChanged) root.whiskerService.showArchived(modelData.value === "archived")
              }
            }
          }
        }
        Rectangle {
          visible: root.parts.chatList
          width: parent.width
          height: 1
          color: Color.muted
        }

        // The new Chat view: search the people, pick one to open the Chat.
        Column {
          id: newChatView
          visible: root.parts.newChat
          width: parent.width
          spacing: Style.space(8)
          onVisibleChanged: if (visible) {
            newChatInput.text = ""
            newChatInput.forceActiveFocus()
            root.whiskerService.searchContacts("")
          }

          Rectangle {
            width: parent.width
            height: newChatBar.implicitHeight + Style.space(8)
            color: root.surfaceColor
            RowLayout {
              id: newChatBar
              anchors.fill: parent
              anchors.leftMargin: Style.space(8)
              anchors.rightMargin: Style.space(4)
              spacing: Style.space(8)
              Image {
                source: Qt.resolvedUrl("assets/whisker.svg")
                Layout.preferredWidth: Style.space(16)
                Layout.preferredHeight: Style.space(16)
                sourceSize.width: Style.space(16) * Screen.devicePixelRatio
                sourceSize.height: Style.space(16) * Screen.devicePixelRatio
              }
              Text {
                Layout.fillWidth: true
                text: "new chat"
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
                font.bold: true
              }
              Button {
                text: "←"
                tooltipText: "Back to Chats"
                onClicked: root.showNewChat = false
              }
            }
          }

          RowLayout {
            width: parent.width - Style.space(16)
            x: Style.space(8)
            spacing: Style.space(8)
            Text {
              text: "󰍉"
              opacity: 0.6
              color: Color.accent
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
            }
            TextInput {
              id: newChatInput
              Layout.fillWidth: true
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
              property string lastText: ""
              onTextChanged: {
                var capped = Draft.capText(text, lastText)
                if (!capped.ok) {
                  root.whiskerService.textRefused()
                  text = capped.text
                  return
                }
                lastText = text
                if (parent.visible) root.whiskerService.searchContacts(text)
              }
              Keys.onEscapePressed: root.runShortcut("Escape")
              Text {
                visible: newChatInput.text === ""
                text: "name or phone number, e.g. +32 470 …"
                opacity: 0.5
                color: newChatInput.color
                font: newChatInput.font
              }
            }
          }
          Rectangle {
            width: parent.width
            height: 1
            color: Color.muted
          }
          Text {
            x: Style.space(8)
            text: "contacts"
            opacity: 0.5
            color: root.contentForeground
            font.family: root.contentFontFamily
            font.pixelSize: Style.font.caption
          }

          ListView {
            id: newChatList
            WheelScroll { view: newChatList }
            width: parent.width
            height: Math.max(0, root.popoutHeight - newChatView.y - y)
            clip: true
            model: NewChat.newChatRows(root.view.contacts, newChatInput.text, root.view.lookup)
            delegate: Item {
              required property var modelData
              readonly property bool note: modelData.kind === "note"
              width: ListView.view.width
              implicitHeight: rowLine.implicitHeight
              height: implicitHeight
              TapHandler {
                enabled: modelData.kind === "contact" || modelData.status === "found"
                onTapped: {
                  root.showNewChat = false
                  root.whiskerService.startChat(NewChat.newChatTarget(modelData))
                }
              }
              // Contact row: "@ name    +phone  open|new". A phone row shows the number and its status. The note is a dim footer line.
              RowLayout {
                id: rowLine
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.leftMargin: Style.space(8)
                anchors.rightMargin: Style.space(8)
                spacing: Style.space(8)
                Text {
                  visible: !note
                  text: "@"
                  opacity: 0.6
                  color: root.contentForeground
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.body
                }
                Text {
                  Layout.fillWidth: true
                  wrapMode: Text.Wrap
                  textFormat: Text.PlainText
                  text: note ? modelData.text : NewChat.newChatRowName(modelData)
                  color: root.contentForeground
                  opacity: note || modelData.pushNameOnly ? 0.6 : 1
                  font.family: root.contentFontFamily
                  font.pixelSize: note ? Style.font.caption : Style.font.body
                }
                Text {
                  visible: !note
                  textFormat: Text.PlainText
                  text: (modelData.kind === "contact" ? modelData.phone + "  " : "") + modelData.label
                  opacity: 0.6
                  color: root.contentForeground
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.body
                }
              }
            }
          }
        }

        // Profile Settings in four sections: Privacy, Notifications, Media, Account. The Daemon owns the values: each control sends one change.
        // They scroll when they do not fit the pop-out, as the Chat list does.
        Flickable {
          id: settingsView
          WheelScroll { view: settingsView }
          visible: root.parts.settings
          width: parent.width
          height: visible ? Math.max(0, root.popoutHeight - y) : 0
          contentHeight: settingsColumn.height
          boundsBehavior: Flickable.StopAtBounds
          clip: true
          Column {
            id: settingsColumn
            width: settingsView.width
            spacing: Style.space(6)

            Text {
              visible: text !== ""
              width: parent.width
              wrapMode: Text.Wrap
              text: Daemon.keyringNotice(root.view)
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
            }
            PanelSectionHeader { text: "Privacy"; foreground: root.contentForeground; fontFamily: root.contentFontFamily }
            SettingChoice {
              label: "Read Receipts"
              options: Daemon.READ_RECEIPT_MODES
              value: root.profileSettings ? root.profileSettings.readReceipts : ""
              onChanged: function(mode) { root.whiskerService.changeSettings({ readReceipts: mode }) }
            }
            Text {
              width: parent.width
              wrapMode: Text.Wrap
              opacity: 0.6
              text: Daemon.readReceiptDescription(root.profileSettings)
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.caption
            }
            SettingToggle { key: "sendOnline" }
            SettingToggle { key: "sendTyping" }

            PanelSeparator { width: parent.width; foreground: root.contentForeground }
            PanelSectionHeader { text: "Notifications"; foreground: root.contentForeground; fontFamily: root.contentFontFamily }
            SettingToggle { key: "notifications" }
            SettingToggle { key: "notificationPreview"; available: !!root.profileSettings && root.profileSettings.notifications }

            PanelSeparator { width: parent.width; foreground: root.contentForeground }
            PanelSectionHeader { text: "Media"; foreground: root.contentForeground; fontFamily: root.contentFontFamily }
            SettingToggle { key: "autoDownload" }
            SettingChoice {
              visible: !!root.profileSettings && root.profileSettings.autoDownload
              label: "Up to"
              options: Daemon.AUTO_DOWNLOAD_LIMITS
              value: root.profileSettings ? root.profileSettings.autoDownloadLimit : ""
              onChanged: function(limit) { root.whiskerService.changeSettings({ autoDownloadLimit: limit }) }
            }

            PanelSeparator { width: parent.width; foreground: root.contentForeground }
            PanelSectionHeader { text: "Account"; foreground: root.contentForeground; fontFamily: root.contentFontFamily }
            Text {
              visible: root.confirmLogout
              width: parent.width
              wrapMode: Text.Wrap
              text: "Log out removes this device from your phone and deletes your local chats."
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.body
            }
            Row {
              spacing: Style.space(8)
              Button {
                visible: !root.confirmLogout
                text: "Log out"
                bordered: true
                onClicked: root.confirmLogout = true
              }
              Button {
                visible: root.confirmLogout
                text: "Cancel"
                bordered: true
                onClicked: root.confirmLogout = false
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
            }
          }
        }

        // The open Chat: its top bar, its timeline (newest at the bottom) and the composer. Enter sends, Shift+Enter adds a line.
        // The Chat view has a fixed height; the timeline takes what the bar and the composer leave.
        ColumnLayout {
          visible: root.parts.chat
          width: parent.width
          height: root.popoutHeight - y
          spacing: Style.space(4)
          onVisibleChanged: if (visible) composer.forceActiveFocus()

          // Top bar: the picture or sign, "name · status" on the left; the pin and mute marks, "⋯" and "←" on the right.
          Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: chatBar.implicitHeight + Style.space(8)
            color: root.surfaceColor
            RowLayout {
              id: chatBar
              anchors.fill: parent
              anchors.leftMargin: Style.space(8)
              anchors.rightMargin: Style.space(4)
              spacing: Style.space(8)
              // The icon slot has the size and place of the logo in the main header, so the name starts where "whisker" does.
              // It holds the Profile picture or group icon, round, or else the Chat sign.
              Item {
                Layout.preferredWidth: Style.space(16)
                Layout.preferredHeight: Style.space(16)
                Image {
                  id: chatPicture
                  readonly property string pictureSource: root.openChat ? ChatList.chatPictureSource(root.view, root.openChat.jid) : ""
                  anchors.fill: parent
                  visible: pictureSource !== ""
                  source: pictureSource
                  sourceSize: Qt.size(Style.space(32), Style.space(32)) // twice the size, sharp on a scaled screen
                  fillMode: Image.PreserveAspectCrop
                  layer.enabled: true
                  layer.effect: MultiEffect {
                    maskEnabled: true
                    maskSource: pictureMask
                  }
                }
                Rectangle {
                  id: pictureMask
                  anchors.fill: parent
                  radius: width / 2
                  layer.enabled: true
                  visible: false
                }
                Text {
                  anchors.centerIn: parent
                  visible: !chatPicture.visible
                  text: root.openChat ? ChatList.chatSign(root.openChat) : ""
                  opacity: 0.6 // as in the Chat list row
                  color: root.contentForeground
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.body
                }
              }
              Text {
                // While the Chat links show, the name fills but never grows past itself: it shrinks and elides when the links need the room.
                // Otherwise the status takes the cut, as before.
                Layout.fillWidth: chatLinks.active
                Layout.maximumWidth: Math.min(implicitWidth, parent.width * 0.6)
                elide: Text.ElideRight
                textFormat: Text.PlainText
                text: root.openChat ? ChatList.chatName(root.openChat) : ""
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
                font.bold: true
              }
              Text {
                // The number of a Push Name stays whole: the name elides, never the number.
                visible: text !== ""
                textFormat: Text.PlainText
                text: root.openChat ? ChatList.chatNumber(root.openChat) : ""
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
                font.bold: true
              }
              // Presence of the other side: Online or Last Seen, and "typing…" or "recording…" in the accent colour.
              Text {
                Layout.fillWidth: true
                elide: Text.ElideRight
                textFormat: Text.PlainText
                readonly property string status: ChatList.chatStatusLabel(root.view, Math.floor(Date.now() / 1000))
                // Empty while the Chat links show: it stays as the spacer that keeps ⋯ and Back at the right.
                text: status !== "" && !chatLinks.active ? " · " + status : ""
                color: ChatList.activityLabel(root.view) !== "" ? Color.accent : root.contentForeground
                opacity: ChatList.activityLabel(root.view) !== "" ? 1 : 0.6
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
              }
              Text {
                visible: !!root.openChatRow && root.openChatRow.pinned === true
                text: ChatList.PIN_MARK
                opacity: 0.8
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
              }
              Text {
                visible: !!root.openChatRow && root.openChatRow.muted === true
                text: visible ? "󰝟 " + ChatList.muteLeftLabel(root.openChatRow, Math.floor(Date.now() / 1000)) : ""
                opacity: 0.6
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
              }
              Loader {
                id: chatLinks
                active: root.openChatRow !== null && root.chatLinksFor === root.openChat.jid
                visible: active
                sourceComponent: ChatLinks { chat: root.openChatRow; panel: root }
              }
              Button {
                visible: root.openChatRow !== null
                text: "⋯"
                tooltipText: "Chat links"
                onClicked: root.chatLinksFor = root.chatLinksFor === root.openChat.jid ? "" : root.openChat.jid
              }
              // Back sits at the right in every bar, so the left side never shifts.
              Button {
                text: "←"
                tooltipText: "Back to Chats"
                onClicked: root.whiskerService.closeChat()
              }
            }
          }

          ListView {
            WheelScroll { view: timelineView }
            id: timelineView
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            verticalLayoutDirection: ListView.BottomToTop
            // Qt bug: in BottomToTop, a newest row that gets shorter (a removed Reaction) slides below the view. Keep the view on it.
            // Qt can move contentY before it knows the content height, so check again when the height changes.
            property bool atNewest: true
            onContentYChanged: atNewest = atYEnd
            onContentHeightChanged: if (atNewest) positionViewAtBeginning(); else atNewest = atYEnd
            // A new model array scrolls the list back to the newest row. So the rows live in a ListModel of keys that only gets edits.
            // Rows added above or below the view then leave the rows in view in place.
            model: KeyedModel { id: timelineKeys }
            property string shownChat: ""
            Component.onCompleted: showTimeline()
            function showTimeline() {
              // Newest first: BottomToTop puts row 0 at the bottom. Another Chat opens at its newest row.
              timelineKeys.show(root.timeline.slice().reverse(), Timeline.rowKey, root.openJid !== shownChat)
              shownChat = root.openJid
            }
            // BottomToTop puts the footer at the top of the timeline: a quiet link that loads the older Messages.
            footer: Item {
              visible: Timeline.olderLink(root.view) !== ""
              width: ListView.view.width
              height: visible ? olderText.implicitHeight + Style.space(4) : 0
              Text {
                id: olderText
                anchors.horizontalCenter: parent.horizontalCenter
                text: Timeline.olderLink(root.view)
                opacity: 0.5
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.caption
                TapHandler { onTapped: root.whiskerService.loadOlderMessages() }
              }
            }
            // A click on empty space closes the quick emoji bar and the menu.
            MouseArea {
              anchors.fill: parent
              z: -1
              enabled: root.reactingId !== "" || root.menuId !== ""
              onClicked: root.closeOverlays()
            }
            delegate: MessageLine {
              required property string key
              // A removed row's delegate lives until Qt destroys it, after its key left the rows: it then draws nothing.
              row: timelineKeys.rows[key] ?? ({ kind: "removed" })
              panel: root
              width: ListView.view.width
            }
          }

          Text {
            visible: root.parts.emptyChat
            Layout.fillWidth: true
            wrapMode: Text.Wrap
            text: NewChat.EMPTY_CHAT_TEXT
            color: root.contentForeground
            opacity: 0.6
            font.family: root.contentFontFamily
            font.pixelSize: Style.font.body
          }

          // One row per Attachment, each with its own ✕. A row that the Daemon refused shows in the error colour. After 4 rows the list scrolls.
          ListView {
            id: attachmentList
            visible: Draft.hasAttachment(root.draft)
            Layout.fillWidth: true
            Layout.preferredHeight: Math.min(count, 4) * 28
            clip: true
            model: Draft.attachmentRows(root.view)
            delegate: RowLayout {
              id: attachmentRow
              required property var modelData
              width: attachmentList.width
              height: 28
              Text {
                Layout.fillWidth: true
                elide: Text.ElideMiddle
                textFormat: Text.PlainText
                text: attachmentRow.modelData.label
                color: attachmentRow.modelData.bad ? Color.urgent : root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.caption
              }
              Button {
                text: "✕"
                onClicked: root.whiskerService.changeDraft({ attachments: root.draft.attachments.filter(function(path) { return path !== attachmentRow.modelData.path }) })
              }
            }
          }

          // The target of the next send: the Message under Quote or Edit, with a ✕ that cancels it.
          RowLayout {
            readonly property var quote: Message.composerQuoteView(root.quoting)
            visible: quote !== null || root.editingId !== ""
            Layout.fillWidth: true
            Text {
              Layout.fillWidth: true
              elide: Text.ElideRight
              textFormat: Text.PlainText
              text: parent.quote ? "Replying to " + parent.quote.sender + ": " + parent.quote.text : (root.editingId !== "" ? "Editing a Message" : "")
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.caption
            }
            Button {
              text: "✕"
              onClicked: root.whiskerService.changeDraft(root.editingId !== "" ? { edit: "", text: "" } : { quote: "" })
            }
          }

          Composer {
            id: composer
            Layout.fillWidth: true
            panel: root
          }
        }

        // The Chat list: the Daemon sends the Chats in order, with the name and the marks.
        ListView {
          id: chatListView
          WheelScroll { view: chatListView }
          visible: root.parts.chatList && root.shownChats.length > 0
          width: parent.width
          height: visible ? Math.max(0, root.popoutHeight - y - (loadMoreButton.visible ? loadMoreButton.height + column.spacing : 0)) : 0
          clip: true
          // Presence and typing frames replace the Chats array; edits keep the scroll place and the hover links.
          model: KeyedModel { id: chatKeys }
          Component.onCompleted: showChats()
          function showChats() { chatKeys.show(root.shownChats, function (chat) { return chat.jid }, false) }
          delegate: ChatRow {
            required property string key
            // A removed row's delegate lives until Qt destroys it, after its key left the rows.
            chat: chatKeys.rows[key] ?? ({ jid: key, name: "" })
            panel: root
            width: ListView.view.width
          }
        }

        // No Chat matches: the faded logo and a short text.
        Column {
          visible: root.parts.chatList && root.shownChats.length === 0
          width: parent.width
          spacing: Style.space(8)
          Image {
            anchors.horizontalCenter: parent.horizontalCenter
            source: Qt.resolvedUrl("assets/whisker.svg")
            width: Style.space(96)
            height: width
            opacity: 0.2
            sourceSize.width: width * Screen.devicePixelRatio
            sourceSize.height: height * Screen.devicePixelRatio
          }
          Text {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            textFormat: Text.PlainText
            text: ChatList.emptyChatsText(root.chatFilter, searchInput.text)
            color: root.contentForeground
            opacity: 0.6
            font.family: root.contentFontFamily
            font.pixelSize: Style.font.body
          }
        }

        Button {
          id: loadMoreButton
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
