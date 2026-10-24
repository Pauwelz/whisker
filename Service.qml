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

  // The Plugin never runs a Daemon with another version. It never restarts a Daemon that exited.
  function startDaemon(versionOutput) {
    var failure = State.checkVersion(expectedVersion, versionOutput)
    if (failure) view = failure
    else daemon.running = true
  }

  property int nextRequestId: 1

  // Ask the Daemon to try the keyring again. The pop-out offers Retry only when the Daemon asks for it.
  function retryKeyring() {
    daemon.write(State.keyringRetryRequest(nextRequestId++))
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
        if (frame) root.view = State.applyFrame(root.view, frame)
      }
    }
    stderr: SplitParser {
      onRead: function(line) { console.warn("whisker-daemon: " + line) }
    }
    onExited: function(exitCode) { root.view = State.daemonExited(root.view, exitCode) }
  }
}
