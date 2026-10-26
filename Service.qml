import QtQuick
import Quickshell
import Quickshell.Io
import "state/ChatList.js" as ChatList
import "state/Daemon.js" as Daemon
import "state/Draft.js" as Draft
import "state/Media.js" as Media
import "state/Message.js" as Message
import "state/NewChat.js" as NewChat
import "state/Timeline.js" as Timeline

// Owns the Daemon process. keepLoaded: one Daemon serves the bar widget of every monitor.
// Views read `view` and hold no decision logic (see state/Daemon.js).
Item {
  id: root

  property var shell: null
  property var view: Daemon.initialState()

  readonly property string pluginDir: decodeURIComponent(Qt.resolvedUrl(".").toString().replace("file://", ""))
  readonly property string daemonPath: pluginDir + "bin/whisker-daemon"
  property string expectedVersion: ""

  property bool downloaded: false
  property bool replacing: false

  // A Ctrl+V that gave no Attachment: the composer pastes text.
  signal pasteText()

  // The Plugin never runs a Daemon with another version. It never restarts a Daemon that exited.
  // A wrong or missing binary is fetched once; a running wrong Daemon is stopped first.
  function startDaemon(versionOutput) {
    var step = Daemon.versionStep(expectedVersion, versionOutput, downloaded)
    view = step.view
    if (step.action === "start") daemon.running = true
    if (step.action !== "download") return
    downloaded = true
    if (daemon.running) {
      replacing = true
      daemon.running = false
    }
    fetch.running = true
  }

  // Called by the Pair Code form.
  function requestPairCode(phone) {
    send(Daemon.pairCodeRequest, phone)
  }

  // The text of the search field. It filters every Chat list request.
  property string chatQuery: ""
  // True while the "archived" filter shows. The Daemon then lists only the archived Chats.
  property bool chatArchived: false

  // Called by the "Load more" button of the Chat list.
  function loadMoreChats() {
    send(ChatList.chatListRequest, view.chats ? view.chats.length : 0, chatQuery, chatArchived)
  }

  // Called when the search field changes.
  function searchChats(text) {
    chatQuery = text
    send(ChatList.chatListRequest, 0, chatQuery, chatArchived)
  }

  // Called when the user picks or leaves the "archived" filter.
  function showArchived(on) {
    chatArchived = on
    send(ChatList.chatListRequest, 0, chatQuery, chatArchived)
  }

  // The new Chat view: the people that match the search field, and the pick of one of them.
  function searchContacts(text) {
    send(NewChat.contactsSearchRequest, text)
    var digits = NewChat.lookupPhone(text)
    if (digits === "") {
      lookupTimer.stop()
      return
    }
    view = NewChat.lookupStarted(view, digits)
    lookupTimer.digits = digits
    lookupTimer.restart() // the lookup waits until the user stops typing
  }

  Timer {
    id: lookupTimer
    property string digits: ""
    interval: 400
    onTriggered: root.send(NewChat.lookupRequest, digits)
  }

  // Called when the user picks a person. The Daemon stores the Chat first; the Messages and the Draft follow in order.
  function startChat(contact) {
    send(NewChat.chatStartRequest, contact.jid)
    openChat({ jid: contact.jid, name: contact.name, kind: "direct" })
  }

  // Called when the user picks a Chat of the Chat list. `chat` is a row of the list.
  function openChat(chat) {
    if (view.openChat) leaveChat() // a notification click can open a Chat while another is open
    view = Timeline.openChat(view, chat)
    syncAttention()
    send(Timeline.messagesRequest, chat.jid, 0)
    send(Draft.draftGetRequest, chat.jid)
    requestPicture()
  }

  // Asks for the Profile picture of the open Chat when the view has no answer for it yet.
  function requestPicture() {
    var jid = ChatList.pictureToRequest(view)
    if (jid !== "") send(ChatList.chatPictureRequest, jid)
  }

  // Called when the user changes the Draft: the composer text, or a Quote or Edit target. The Daemon stores it.
  function changeDraft(changes) {
    var next = Draft.changeDraft(view, changes)
    if (next === view) return
    view = next
    if (Daemon.draftSetDue(view)) send(Draft.draftSetRequest, view.openChat.jid, view.openChat.draft)
  }

  // Called by the IPC handler `openChat`, which a click on a notification runs.
  function openChatByJid(jid) {
    openChat(ChatList.chatByJid(view, jid))
  }

  // Ends what belongs to the open Chat. The Daemon deletes the plain Media copies of the Chat.
  function leaveChat() {
    stopTyping()
    stopPlayer()
    send(Media.mediaReleaseRequest, view.openChat.jid)
  }

  function closeChat() {
    leaveChat()
    view = Timeline.closeChat(view)
    syncAttention()
    send(ChatList.chatListRequest, 0, chatQuery, chatArchived) // a Draft can list a Chat
  }

  // Outgoing Composing. The Daemon drops it when "send typing" is off.
  property string typingChat: "" // the Chat that got Composing, or empty

  // Called by the composer when its text changes.
  function composerChanged(text) {
    if (!view.openChat) return
    var draftText = Draft.draftOf(view).text
    changeDraft({ text: text })
    var action = Draft.typingAction(typingChat !== "", text, draftText)
    if (action === "start") {
      typingChat = view.openChat.jid
      send(Draft.typingRequest, typingChat, true)
    } else if (action === "stop") {
      stopTyping()
    }
  }

  function stopTyping() {
    if (typingChat === "") return
    send(Draft.typingRequest, typingChat, false)
    typingChat = ""
  }

  // Called by the pop-out when it opens or closes. The Daemon derives the Attended Chat from this and the shown Chat.
  // shortcut: with several monitors the last pop-out to report wins, upgrade if two pop-outs can be open at once.
  property bool popoutOpen: false

  function setPopoutOpen(open) {
    popoutOpen = open
    if (!open) {
      stopPlayer()
      stopTyping()
    }
    syncAttention()
  }

  function syncAttention() {
    if (!daemon.running) return
    send(Daemon.attentionRequest, popoutOpen, view.openChat ? view.openChat.jid : "")
  }

  // Media the user clicked to open, by key, until its path arrives.
  property var pendingOpen: ({})

  // Called by the Download button.
  function downloadMedia(message) {
    send(Media.mediaDownloadRequest, message)
  }

  // Called by Open and by a tap on an image. A path that is already known opens at once.
  function openMedia(message) {
    var step = Media.openMediaStep(view, playingKey, message)
    if (step.action === "stop") {
      stopPlayer()
    } else if (step.action === "launch") {
      launch(message, step.path)
    } else {
      pendingOpen = Media.rememberOpen(pendingOpen, message)
      send(Media.mediaOpenRequest, message)
    }
  }

  // Called by Save as, with an absolute path.
  function saveMedia(message, path) {
    send(Media.mediaSaveRequest, message, path)
  }

  // The voice note that plays now, by Media.mediaKey. Empty when nothing plays.
  property string playingKey: ""

  function stopPlayer() {
    player.running = false
    playingKey = ""
  }

  function playVoice(message, path) {
    stopPlayer()
    playingKey = Media.mediaKey(message.chat, message.id)
    player.command = Media.openCommand(message, path)
    player.running = true
    send(Media.playedRequest, message)
  }

  // voice in mpv without a window, audio and video in mpv, the rest in the default app
  function launch(message, path) {
    if (Media.isVoice(message)) {
      playVoice(message, path)
      return
    }
    opener.command = Media.openCommand(message, path)
    opener.startDetached()
  }

  // Asks for the path of each image and sticker that is ready, and opens Media that the user clicked.
  function handleMediaFrame(frame) {
    var step = Media.mediaFrameStep(view, pendingOpen, frame)
    view = step.state
    pendingOpen = step.pending
    if (step.launch) launch(step.launch.message, step.launch.path)
    step.load.forEach(function(m) { send(Media.mediaOpenRequest, m) })
  }

  Process {
    id: opener
  }

  Process {
    id: linkOpener
  }

  // Called by the Open map button of a location. Only a map link opens.
  function openLink(url) {
    var command = Message.linkCommand(url)
    if (command.length === 0) return
    linkOpener.command = command
    linkOpener.startDetached()
  }

  // Called by the Copy text action. The text goes to the clipboard.
  function copyText(text) {
    Quickshell.clipboardText = text
  }

  // The child process of the voice note. Stop ends it. An exit that is not from a newer note clears playingKey.
  Process {
    id: player
    onExited: if (!running) root.playingKey = ""
  }

  // Called by the "Load older" button of the open Chat.
  function loadOlderMessages() {
    var action = Timeline.olderAction(view)
    if (action === "messages") {
      send(Timeline.messagesRequest, view.openChat.jid, view.openChat.messages.length)
    } else if (action === "history") {
      view = Timeline.historyRequested(view)
      send(Timeline.historyRequest, view.openChat.jid)
    }
  }

  // Called by the composer. The Draft says whether this is an edit, and what the new Message quotes.
  // The Daemon clears the Draft when it accepts the send or edit; the view clears its copy at once.
  // When the Daemon refuses a send with Attachments, the reply handler puts the Draft back (Draft.submitDraft keeps it).
  function submitComposer(text) {
    var draft = Draft.draftOf(view)
    var id = nextRequestId // the id that send() gives this request
    send(Draft.composerRequest, view.openChat.jid, draft.edit, text, draft.quote, draft.attachments)
    view = Draft.submitDraft(view, id)
  }

  // Called by the Chat links. `option` is the option of a ChatList.chatLinks link.
  function chooseChatOption(chat, option) {
    if (ChatList.leavesChatFor(view, chat.jid, option)) closeChat() // before the request: the Chat is no longer attended
    send(ChatList.chatOptionRequest, chat.jid, option)
  }

  // Called when the notice of an error has shown long enough.
  function dismissNotice() {
    view = Daemon.clearNotice(view)
  }

  // Called by the Reaction picker. `emoji` is the picked emoji; picking the current Reaction removes it.
  function reactToMessage(message, emoji) {
    send(Message.reactRequest, view.openChat.jid, message.id, Message.reactionChoice(message, emoji))
  }

  // Called by Ctrl+V in the composer. Copied files become Attachments; anything else comes back as pasteText.
  function pasteIntoComposer() {
    send(Draft.pasteRequest, view.openChat.jid)
  }

  // Called by the Delete button of a Message of the user. The Message becomes a Tombstone.
  function revokeMessage(messageId) {
    send(Message.revokeRequest, view.openChat.jid, messageId)
  }

  // Called by "Delete for me" in the Message menu. The Daemon sends messageExpired, which removes the Message.
  function deleteMessageForMe(messageId) {
    send(Message.deleteForMeRequest, view.openChat.jid, messageId)
  }

  // Called by the Retry button of a failed Message.
  function retryMessage(messageId) {
    send(Timeline.retryMessageRequest, view.openChat.jid, messageId)
  }

  // Called by the settings view. `changes` holds only the settings that change.
  function changeSettings(changes) {
    send(Daemon.settingsSetRequest, changes)
  }

  // Called by the Log out button, after the user confirmed.
  function logout() {
    send(Daemon.logoutRequest)
  }

  // Called by the Retry button.
  function retry() {
    var kind = Daemon.retryKind(view)
    if (kind === "keyring") {
      view = Daemon.initialState()
      send(Daemon.keyringRetryRequest)
      return
    }
    if (kind === "link") {
      send(Daemon.linkRetryRequest)
      return
    }
    downloaded = false
    view = Daemon.initialState()
    versionProbe.running = true
  }

  // Exit codes are in docs/releasing.md. On success, probe again to confirm the new version.
  Process {
    id: fetch
    command: ["sh", root.pluginDir + "scripts/fetch-daemon.sh"]
    onExited: function(exitCode) {
      if (exitCode === 0) versionProbe.running = true
      else root.view = Daemon.downloadFailed(exitCode)
    }
  }

  property int nextRequestId: 1

  // Sends one request to the Daemon. `builder` is a request function of the state/ modules; this gives it the next request id, then the other arguments.
  function send(builder) {
    daemon.write(builder.apply(State, [nextRequestId++].concat(Array.prototype.slice.call(arguments, 1))))
  }

  FileView {
    path: root.pluginDir + "manifest.json"
    printErrors: false
    onLoaded: {
      root.expectedVersion = JSON.parse(text()).version
      versionProbe.running = true
    }
  }

  // Run through sh so that a missing binary still ends the stream, with empty output.
  Process {
    id: versionProbe
    command: ["sh", "-c", "exec \"$0\" --version 2>/dev/null", root.daemonPath]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.startDaemon(text)
    }
  }

  // EOF on stdin lets the Daemon wipe the plain Media copies before Quickshell kills it. The start wipe covers a lost race.
  Component.onDestruction: daemon.stdinEnabled = false

  Process {
    id: daemon
    command: [root.daemonPath]
    stdinEnabled: true
    stdout: SplitParser {
      onRead: function(line) {
        var frame = Daemon.parseLine(line)
        if (!frame) return
        var draftSetDue = Daemon.draftSetDue(root.view, frame) // the answer to a send: store the restored or typed Draft
        root.view = Daemon.applyFrame(root.view, frame)
        if (draftSetDue) root.send(Draft.draftSetRequest, root.view.openChat.jid, Draft.draftOf(root.view))
        root.handleMediaFrame(frame)
        var pasted = Draft.pastedAttachment(root.view, frame)
        if (pasted !== null && pasted.length === 0) root.pasteText()
        else if (pasted !== null) root.changeDraft({ attachments: Draft.appendAttachments(Draft.draftOf(root.view), pasted) })
        if (Daemon.resendsAttention(frame)) root.syncAttention()
        if (Timeline.reloadsOpenChat(root.view, frame)) {
          root.syncAttention()
          root.send(Timeline.messagesRequest, root.view.openChat.jid, 0)
          root.send(Draft.draftGetRequest, root.view.openChat.jid)
        }
        if (Timeline.reloadsHistory(root.view, frame)) root.send(Timeline.messagesRequest, root.view.openChat.jid, root.view.openChat.messages.length)
        // A merge changes the JID of the open Chat, and a changed picture drops its entry. Both need a new request.
        if (Timeline.reloadsOpenChat(root.view, frame) || frame.method === "pictureChanged") root.requestPicture()
        if (Daemon.requestsSettings(frame)) root.send(Daemon.settingsGetRequest)
        if (Daemon.refreshesChats(frame)) root.send(ChatList.chatListRequest, 0, root.chatQuery, root.chatArchived)
      }
    }
    stderr: SplitParser {
      onRead: function(line) { console.warn("whisker-daemon: " + line) }
    }
    onExited: function(exitCode) {
      if (root.replacing) root.replacing = false
      else root.view = Daemon.daemonExited(root.view, exitCode)
    }
  }
}
