import QtQuick
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
    daemon.write(State.pairCodeRequest(nextRequestId++, phone))
  }

  // The text of the search field. It filters every Chat list request.
  property string chatQuery: ""

  // Called by the "Load more" button of the Chat list.
  function loadMoreChats() {
    daemon.write(State.chatListRequest(nextRequestId++, view.chats ? view.chats.length : 0, chatQuery))
  }

  // Called when the search field changes.
  function searchChats(text) {
    chatQuery = text
    daemon.write(State.chatListRequest(nextRequestId++, 0, chatQuery))
  }

  // Called when the user picks a Chat of the Chat list. `chat` is a row of the list.
  function openChat(chat) {
    view = State.openChat(view, chat)
    daemon.write(State.messagesRequest(nextRequestId++, chat.jid, 0))
  }

  function closeChat() {
    // The Daemon deletes the plain Media copies of the Chat.
    daemon.write(State.mediaReleaseRequest(nextRequestId++, view.openChat.jid))
    view = State.closeChat(view)
  }

  // Media the user clicked to open, by key, until its path arrives.
  property var pendingOpen: ({})

  // Called by the Download button.
  function downloadMedia(message) {
    daemon.write(State.mediaDownloadRequest(nextRequestId++, message))
  }

  // Called by Open and by a tap on an image. A path that is already known opens at once.
  function openMedia(message) {
    var path = State.mediaPath(view, message)
    if (path !== "") {
      launch(message, path)
      return
    }
    pendingOpen[State.mediaKey(message.chat, message.id)] = message
    daemon.write(State.mediaOpenRequest(nextRequestId++, message))
  }

  // Called by Save as, with an absolute path.
  function saveMedia(message, path) {
    daemon.write(State.mediaSaveRequest(nextRequestId++, message, path))
  }

  // video in mpv, the rest in the default app
  function launch(message, path) {
    opener.command = State.openCommand(message, path)
    opener.startDetached()
  }

  // Asks for the path of each image and sticker that is ready, and opens Media that the user clicked.
  function handleMediaFrame(frame) {
    var result = frame.result
    if (result && result.path) {
      var key = State.mediaKey(result.chat, result.id)
      var message = pendingOpen[key]
      if (message) {
        delete pendingOpen[key]
        launch(message, result.path)
      }
    }
    var todo = State.mediaToLoad(view)
    if (todo.length === 0) return
    view = State.markMediaRequested(view, todo)
    todo.forEach(function(m) { daemon.write(State.mediaOpenRequest(nextRequestId++, m)) })
  }

  Process {
    id: opener
  }

  // Called by the "Load older" button of the open Chat.
  function loadOlderMessages() {
    daemon.write(State.messagesRequest(nextRequestId++, view.openChat.jid, view.openChat.messages.length))
  }

  // Called by the composer. `editingId` is the Message under edit, or "" for a new Message.
  // A new Message: the Daemon stores it as pending and answers with it.
  function submitComposer(editingId, text) {
    daemon.write(State.composerRequest(nextRequestId++, view.openChat.jid, editingId, text))
  }

  // Called by the Delete button of a Message of the user. The Message becomes a Tombstone.
  function revokeMessage(messageId) {
    daemon.write(State.revokeRequest(nextRequestId++, view.openChat.jid, messageId))
  }

  // Called by the Retry button of a failed Message.
  function retryMessage(messageId) {
    daemon.write(State.retryMessageRequest(nextRequestId++, view.openChat.jid, messageId))
  }

  // Called by the settings view. `changes` holds only the settings that change.
  function changeSettings(changes) {
    daemon.write(State.settingsSetRequest(nextRequestId++, changes))
  }

  // Called by the Log out button, after the user confirmed.
  function logout() {
    daemon.write(State.logoutRequest(nextRequestId++))
  }

  // Called by the Retry button.
  function retry() {
    var kind = State.retryKind(view)
    if (kind === "keyring") {
      view = State.initialState()
      daemon.write(State.keyringRetryRequest(nextRequestId++))
      return
    }
    if (kind === "link") {
      daemon.write(State.linkRetryRequest(nextRequestId++))
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
        root.view = State.applyFrame(root.view, frame)
        root.handleMediaFrame(frame)
        if (State.requestsSettings(frame)) daemon.write(State.settingsGetRequest(root.nextRequestId++))
        if (State.refreshesChats(frame)) daemon.write(State.chatListRequest(root.nextRequestId++, 0, root.chatQuery))
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
