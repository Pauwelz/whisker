import QtQuick
import Quickshell
import Quickshell.Io
import "WhiskerState.js" as State

// Owns the Daemon process. keepLoaded: one Daemon serves the bar widget of every monitor.
// Views read `view` and hold no decision logic (see WhiskerState.js).
Item {
  id: root

  property var shell: null
  property var view: State.initialState()

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
    var step = State.versionStep(expectedVersion, versionOutput, downloaded)
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
    send(State.pairCodeRequest, phone)
  }

  // The text of the search field. It filters every Chat list request.
  property string chatQuery: ""
  // True while the "archived" filter shows. The Daemon then lists only the archived Chats.
  property bool chatArchived: false

  // Called by the "Load more" button of the Chat list.
  function loadMoreChats() {
    send(State.chatListRequest, view.chats ? view.chats.length : 0, chatQuery, chatArchived)
  }

  // Called when the search field changes.
  function searchChats(text) {
    chatQuery = text
    send(State.chatListRequest, 0, chatQuery, chatArchived)
  }

  // Called when the user picks or leaves the "archived" filter.
  function showArchived(on) {
    chatArchived = on
    send(State.chatListRequest, 0, chatQuery, chatArchived)
  }

  // The new Chat view: the people that match the search field, and the pick of one of them.
  function searchContacts(text) {
    send(State.contactsSearchRequest, text)
    var digits = State.lookupPhone(text)
    if (digits === "") {
      lookupTimer.stop()
      return
    }
    view = State.lookupStarted(view, digits)
    lookupTimer.digits = digits
    lookupTimer.restart() // the lookup waits until the user stops typing
  }

  Timer {
    id: lookupTimer
    property string digits: ""
    interval: 400
    onTriggered: root.send(State.lookupRequest, digits)
  }

  // Called when the user picks a person. The Daemon stores the Chat first; the Messages and the Draft follow in order.
  function startChat(contact) {
    send(State.chatStartRequest, contact.jid)
    openChat({ jid: contact.jid, name: contact.name, kind: "direct" })
  }

  // Called when the user picks a Chat of the Chat list. `chat` is a row of the list.
  function openChat(chat) {
    if (view.openChat) leaveChat() // a notification click can open a Chat while another is open
    view = State.openChat(view, chat)
    syncAttention()
    send(State.messagesRequest, chat.jid, 0)
    send(State.draftGetRequest, chat.jid)
    requestPicture()
  }

  // Asks for the Profile picture of the open Chat when the view has no answer for it yet.
  function requestPicture() {
    var jid = State.pictureToRequest(view)
    if (jid !== "") send(State.chatPictureRequest, jid)
  }

  // Called when the user changes the Draft: the composer text, or a Quote or Edit target. The Daemon stores it.
  function changeDraft(changes) {
    var next = State.changeDraft(view, changes)
    if (next === view) return
    view = next
    if (State.draftSetDue(view)) send(State.draftSetRequest, view.openChat.jid, view.openChat.draft)
  }

  // Called by the IPC handler `openChat`, which a click on a notification runs.
  function openChatByJid(jid) {
    openChat(State.chatByJid(view, jid))
  }

  // Ends what belongs to the open Chat. The Daemon deletes the plain Media copies of the Chat.
  function leaveChat() {
    stopTyping()
    stopPlayer()
    send(State.mediaReleaseRequest, view.openChat.jid)
  }

  function closeChat() {
    leaveChat()
    view = State.closeChat(view)
    syncAttention()
    send(State.chatListRequest, 0, chatQuery, chatArchived) // a Draft can list a Chat
  }

  // Outgoing Composing. The Daemon drops it when "send typing" is off.
  property string typingChat: "" // the Chat that got Composing, or empty

  // Called by the composer when its text changes.
  function composerChanged(text) {
    if (!view.openChat) return
    changeDraft({ text: text })
    var action = State.typingAction(typingChat !== "", text)
    if (action === "start") {
      typingChat = view.openChat.jid
      send(State.typingRequest, typingChat, true)
    } else if (action === "stop") {
      stopTyping()
    }
  }

  function stopTyping() {
    if (typingChat === "") return
    send(State.typingRequest, typingChat, false)
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
    send(State.attentionRequest, popoutOpen, view.openChat ? view.openChat.jid : "")
  }

  // Media the user clicked to open, by key, until its path arrives.
  property var pendingOpen: ({})

  // Called by the Download button.
  function downloadMedia(message) {
    send(State.mediaDownloadRequest, message)
  }

  // Called by Open and by a tap on an image. A path that is already known opens at once.
  function openMedia(message) {
    var step = State.openMediaStep(view, playingKey, message)
    if (step.action === "stop") {
      stopPlayer()
    } else if (step.action === "launch") {
      launch(message, step.path)
    } else {
      pendingOpen = State.rememberOpen(pendingOpen, message)
      send(State.mediaOpenRequest, message)
    }
  }

  // Called by Save as, with an absolute path.
  function saveMedia(message, path) {
    send(State.mediaSaveRequest, message, path)
  }

  // The voice note that plays now, by State.mediaKey. Empty when nothing plays.
  property string playingKey: ""

  function stopPlayer() {
    player.running = false
    playingKey = ""
  }

  function playVoice(message, path) {
    stopPlayer()
    playingKey = State.mediaKey(message.chat, message.id)
    player.command = State.openCommand(message, path)
    player.running = true
    send(State.playedRequest, message)
  }

  // voice in mpv without a window, video in mpv, the rest in the default app
  function launch(message, path) {
    if (State.isVoice(message)) {
      playVoice(message, path)
      return
    }
    opener.command = State.openCommand(message, path)
    opener.startDetached()
  }

  // Asks for the path of each image and sticker that is ready, and opens Media that the user clicked.
  function handleMediaFrame(frame) {
    var step = State.mediaFrameStep(view, pendingOpen, frame)
    view = step.state
    pendingOpen = step.pending
    if (step.launch) launch(step.launch.message, step.launch.path)
    step.load.forEach(function(m) { send(State.mediaOpenRequest, m) })
  }

  Process {
    id: opener
  }

  Process {
    id: linkOpener
  }

  // Called by the Open map button of a location. Only a map link opens.
  function openLink(url) {
    var command = State.linkCommand(url)
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
    send(State.messagesRequest, view.openChat.jid, view.openChat.messages.length)
  }

  // Called by the composer. The Draft says whether this is an edit, and what the new Message quotes.
  // The Daemon clears the Draft when it accepts the send or edit; the view clears its copy at once.
  // When the Daemon refuses a send with Attachments, the reply handler puts the Draft back (State.submitDraft keeps it).
  function submitComposer(text) {
    var draft = State.draftOf(view)
    var id = nextRequestId // the id that send() gives this request
    send(State.composerRequest, view.openChat.jid, draft.edit, text, draft.quote, draft.attachments)
    view = State.submitDraft(view, id)
  }

  // Called by the Chat links. `option` is the option of a State.chatLinks link.
  function chooseChatOption(chat, option) {
    if (State.leavesChatFor(view, chat.jid, option)) closeChat() // before the request: the Chat is no longer attended
    send(State.chatOptionRequest, chat.jid, option)
  }

  // Called when the notice of an error has shown long enough.
  function dismissNotice() {
    view = State.clearNotice(view)
  }

  // Called by the Reaction picker. `emoji` is the picked emoji; picking the current Reaction removes it.
  function reactToMessage(message, emoji) {
    send(State.reactRequest, view.openChat.jid, message.id, State.reactionChoice(message, emoji))
  }

  // Called by Ctrl+V in the composer. Copied files become Attachments; anything else comes back as pasteText.
  function pasteIntoComposer() {
    send(State.pasteRequest, view.openChat.jid)
  }

  // Called by the Delete button of a Message of the user. The Message becomes a Tombstone.
  function revokeMessage(messageId) {
    send(State.revokeRequest, view.openChat.jid, messageId)
  }

  // Called by "Delete for me" in the Message menu. The Daemon sends messageExpired, which removes the Message.
  function deleteMessageForMe(messageId) {
    send(State.deleteForMeRequest, view.openChat.jid, messageId)
  }

  // Called by the Retry button of a failed Message.
  function retryMessage(messageId) {
    send(State.retryMessageRequest, view.openChat.jid, messageId)
  }

  // Called by the settings view. `changes` holds only the settings that change.
  function changeSettings(changes) {
    send(State.settingsSetRequest, changes)
  }

  // Called by the Log out button, after the user confirmed.
  function logout() {
    send(State.logoutRequest)
  }

  // Called by the Retry button.
  function retry() {
    var kind = State.retryKind(view)
    if (kind === "keyring") {
      view = State.initialState()
      send(State.keyringRetryRequest)
      return
    }
    if (kind === "link") {
      send(State.linkRetryRequest)
      return
    }
    downloaded = false
    view = State.initialState()
    versionProbe.running = true
  }

  // Exit codes are in docs/releasing.md. On success, probe again to confirm the new version.
  Process {
    id: fetch
    command: ["sh", root.pluginDir + "scripts/fetch-daemon.sh"]
    onExited: function(exitCode) {
      if (exitCode === 0) versionProbe.running = true
      else root.view = State.downloadFailed(exitCode)
    }
  }

  property int nextRequestId: 1

  // Sends one request to the Daemon. `builder` is a State.xRequest function; this gives it the next request id, then the other arguments.
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

  Process {
    id: daemon
    command: [root.daemonPath]
    stdinEnabled: true
    stdout: SplitParser {
      onRead: function(line) {
        var frame = State.parseLine(line)
        if (!frame) return
        var draftSetDue = State.draftSetDue(root.view, frame) // the answer to a send: store the restored or typed Draft
        root.view = State.applyFrame(root.view, frame)
        if (draftSetDue) root.send(State.draftSetRequest, root.view.openChat.jid, State.draftOf(root.view))
        if (State.isNotify(frame)) Quickshell.execDetached(State.notifyCommand(frame.params))
        root.handleMediaFrame(frame)
        var pasted = State.pastedAttachment(root.view, frame)
        if (pasted !== null && pasted.length === 0) root.pasteText()
        else if (pasted !== null) root.changeDraft({ attachments: State.appendAttachments(State.draftOf(root.view), pasted) })
        if (State.resendsAttention(frame)) root.syncAttention()
        if (State.reloadsOpenChat(root.view, frame)) {
          root.syncAttention()
          root.send(State.messagesRequest, root.view.openChat.jid, 0)
          root.send(State.draftGetRequest, root.view.openChat.jid)
        }
        // A merge changes the JID of the open Chat, and a changed picture drops its entry. Both need a new request.
        if (State.reloadsOpenChat(root.view, frame) || frame.method === "pictureChanged") root.requestPicture()
        if (State.requestsSettings(frame)) root.send(State.settingsGetRequest)
        if (State.refreshesChats(frame)) root.send(State.chatListRequest, 0, root.chatQuery, root.chatArchived)
      }
    }
    stderr: SplitParser {
      onRead: function(line) { console.warn("whisker-daemon: " + line) }
    }
    onExited: function(exitCode) {
      if (root.replacing) root.replacing = false
      else root.view = State.daemonExited(root.view, exitCode)
    }
  }
}
