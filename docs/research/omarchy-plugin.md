# Omarchy plugin system: findings for whisker

Checked on this machine on 2026-10-24. Versions: quickshell 0.3.2.r7.g2b4501a-3, qt6-declarative 6.12.0-2.
`$OMARCHY` below means `/usr/share/omarchy` (the value of `$OMARCHY_PATH`).
The shell reference `docs/omarchy-shell.md` is not installed here. The Omarchy README links to it. I used the installed source instead.

## Answer

### 1. manifest.json schema, bar item and pop-out

The registry checks these rules (`$OMARCHY/shell/services/PluginRegistry.qml` lines 52-85, and `$OMARCHY/bin/omarchy-plugin-validate`):

- `schemaVersion` must be the number `1`.
- Required fields: `id`, `name`, `version`, `kinds` (non-empty array), `entryPoints` (object).
- `id` matches `^[A-Za-z0-9][A-Za-z0-9._-]*$`. It has no `..`. It must not start with `omarchy.`. `pauwelz.whisker` is valid.
- Every `entryPoints` value is a relative path to an existing file. It has no `..`.
- A kind needs its entry point: `bar`->`bar`, `bar-widget`->`barWidget`, `menu`->`menu`, `overlay`->`overlay`, `panel`->`panel`, `service`->`service`.
- No symlink may exist in the plugin folder. (The validator skips `.git`.)
- Optional: `keepLoaded: true`, `barWidget` (`displayName`, `description`, `category`, `allowMultiple`, `defaultSection`, `defaults`, `schema`), `author`, `license`, `description`, `homepage`.
- `defaultSection` is one of `left`, `centerBegin`, `center`, `centerEnd`, `tray`, `right`.

There is no separate "pop-out" kind. A `bar-widget` entry point is one QML file. That file draws the bar item and holds the pop-out.
The pop-out is a `KeyboardPanel` inside the widget (see section 2).

Minimal real example, `~/.config/omarchy/plugins/kenny.nightlight/manifest.json`. It has a service and a bar item:

```json
{
  "schemaVersion": 1,
  "id": "kenny.nightlight",
  "name": "Night Light",
  "version": "1.1.1",
  "kinds": ["service", "bar-widget"],
  "keepLoaded": true,
  "entryPoints": { "service": "Service.qml", "barWidget": "BarWidget.qml" },
  "barWidget": {
    "displayName": "Night Light",
    "description": "...",
    "category": "Display",
    "allowMultiple": false,
    "defaultSection": "center"
  }
}
```

A single-file widget with its pop-out is `$OMARCHY/shell/plugins/panels/network/manifest.json`: `"kinds": ["bar-widget"]`, `"entryPoints": {"barWidget": "Panel.qml"}`.

Findings that matter for whisker:

- The bar creates one widget instance per monitor. `BarWidget.qml` says: "a bar surface exists per monitor" (`$OMARCHY/shell/Ui/BarWidget.qml`).
- A widget instance must not own the Daemon. Nightlight puts shared state in a `service` kind. The widget reads it with `bar.shell.serviceFor("kenny.nightlight")` (`kenny.nightlight/BarWidget.qml` line 8).
- `keepLoaded` services survive plugin hot-reload (`$OMARCHY/shell/shell.qml` lines 1052-1062). This matches the "edit loads only after shell restart" note for a service.
- Recommended shape: `kinds: ["service", "bar-widget"]`, `keepLoaded: true`. The `Process` lives in the Service.
- Saving a file under `~/.config/omarchy/plugins/` reloads plugin code (`$OMARCHY/default/agents/skills/omarchy/plugins.md`). `omarchy-shell shell rescanPlugins` forces it.
- `omarchy plugin validate <folder>` checks a manifest locally.

### 2. Building the pop-out

Pattern used by `$OMARCHY/shell/plugins/panels/network/Panel.qml` and by `kenny.nightlight/Panel.qml`:

```qml
import qs.Ui
import qs.Commons
Panel {                                  // qs.Ui Panel.qml: open()/close()/toggle(), opened
  id: root
  moduleName: "pauwelz.whisker"
  ipcTarget: "pauwelz.whisker"           // IPC: omarchy-shell pauwelz.whisker open
  BarIconButton { id: button; anchors.fill: parent; bar: root.bar
                  text: "..."; labelText: count; dimmed: !online
                  onPressed: function(b) { root.toggle() } }
  KeyboardPanel {
    anchorItem: button; owner: root; bar: root.bar; open: root.opened
    focusTarget: panel.loadedContent
    contentWidth:  panel.fittedContentWidth(Style.space(380))
    contentHeight: panel.fittedContentHeight(panel.loadedContentHeight)
    content: PanelKeyCatcher { implicitHeight: column.implicitHeight; ... }
  }
}
```

- Components: `Panel` (`Ui/Panel.qml`), `PanelController` (`Ui/PanelController.qml`), `BarIconButton` / `WidgetButton`, `KeyboardPanel`, `PanelKeyCatcher`, `TextField`. All are in `$OMARCHY/shell/Ui/`. Import them with `import qs.Ui`.
- `BarIconButton` has `text` (glyph), `labelText` (text next to the icon, fits an unread count), `dimmed` (dims the icon, fits "offline"), `active`, `tooltipText`, signal `onPressed(button)` (`Ui/BarIconButton.qml`, `Ui/WidgetButton.qml` lines 8-33).
- Sizing: set `contentWidth` and `contentHeight`. Use `fittedContentWidth(w, cap)` and `fittedContentHeight(h, cap)`. They clamp to the screen minus the bar (`Ui/KeyboardPanel.qml` lines 155-185). Lengths use `Style.space(n)`.
- `content:` is a lazy `Component`. It builds on first open and stays alive. The state inside survives close. Reach it through `panel.loadedContent`. Put anything that must run while closed outside it.
- The card is a layer-shell `PanelWindow` on `WlrLayer.Overlay`. It is placed under the bar icon and clamped to the screen. `centerOnBar: true` centers it.
- Keyboard focus: the window takes `WlrKeyboardFocus.Exclusive` for 75 ms, then `OnDemand` (comment block at `KeyboardPanel.qml` lines 1-25 and lines 90-100). `focusTarget` gets `forceActiveFocus()` after it maps (lines 235-250).
- Text field: `Ui/TextField.qml` wraps Qt Quick Controls `TextField`. Call `forceActiveFocus()` on it, as network `Panel.qml` lines 2009-2030 does (`onVisibleChanged: if (visible) Qt.callLater(forceActiveFocus)`).
- `PanelKeyCatcher` uses `Keys.priority: Keys.BeforeItem`. It swallows letters (`j`, `k`, `h`, `l`, `x`). It has a `blocked` property. Set `blocked: field.activeFocus` while a text field is in use, or the field gets no typing (`Ui/PanelKeyCatcher.qml` header comment; network uses `blocked: root.passwordSsid !== ""`). For a pure compose view, a plain `FocusScope` instead of `PanelKeyCatcher` also works.
- Escape: `PanelKeyCatcher.closeRequested`. In a text field, handle `Keys.onEscapePressed` yourself (network does).
- Input methods: `Ui/FilterInput.qml` uses `TextInput` so fcitx5 composing works. A `Text` fed by key events does not.
- Outside click: `KeyboardPanel` has a full-screen `MouseArea` that calls `root.close()`. Its `mask` leaves the bar clickable. A transparent twin window on every other monitor does the same (lines 255-340). No code is needed in the plugin.
- Only one pop-out is open per bar (`bar.requestPopout` / `activePopout`). Opening another bar icon closes yours.
- Open from outside: `Panel.qml` provides `ShellIpc` with `open/close/show/hide/toggle`. A plugin that needs extra methods sets `manageIpc: false` and declares one `IpcHandler { target: "..." }` itself (nightlight and network do).
- Safety: bind untrusted text to `Text` with `textFormat: Text.PlainText` (`$OMARCHY/default/agents/skills/omarchy/plugins.md`, "Plugin UI Safety"). WhatsApp message text is untrusted.

### 3. Following the active theme live

Import `qs.Commons`. Two singletons (`$OMARCHY/shell/Commons/qmldir`):

- `Color` (`Commons/Color.qml`): `Color.foreground`, `background`, `accent`, `urgent`, `muted`. Surface groups: `Color.bar.{background,text,active}`, `Color.popups.{background,text,border}`, `Color.tooltip.*`, `Color.notifications.*`, `Color.menu.*`.
- `Style` (`Commons/Style.qml`): `Style.font.family`, `Style.font.{caption,bodySmall,body,subtitle,title,heading}`, `Style.space(n)`, `Style.cornerRadius`, `Style.spacing.*`, `Style.duration(ms)`.

`Color.qml` reloads `~/.local/state/omarchy/current/theme/colors.toml` and `shell.toml` on a theme switch. It reassigns `shellValues` as a whole, so bindings re-evaluate. Bind to the properties and avoid copying them into constants. Inside a bar widget, `bar.foreground` and `bar.fontFamily` are also available (nightlight uses them). `Ui/TextField.qml`, `Button`, `Toggle` already follow the theme.

### 4. Desktop notification with a click action that opens the pop-out

Omarchy's shell is the notification daemon (`$OMARCHY/shell/plugins/notifications/Service.qml`, a `service` plugin). It speaks `org.freedesktop.Notifications`. It supports two click mechanisms:

- Omarchy hints. `$OMARCHY/bin/omarchy-notification-send` calls `Notify` over `busctl` with a hint `omarchy-exec-argv` (a JSON argv). A click on the card runs it detached (`Service.qml` `activate()`, lines 877-895). With `--action`, the hint `omarchy-actions` adds up to 3 buttons.
- Standard actions. A sender's `default` action is invoked on card click (`invokeSenderAction(key, "default")`). Other actions become buttons (`actionsOf`, lines 702-720).

Recommended: one `Process` in the Service that runs

```
omarchy-notification-send --app-name whisker -r <id> "Alice" "Hello" \
  --exec omarchy-shell pauwelz.whisker openChat 1234@s.whatsapp.net
```

The plugin declares in its widget: `IpcHandler { target: "pauwelz.whisker"; function openChat(jid: string): void { ... } }` (needs `manageIpc: false`). That handler stores the chat id, then calls `open()`.

- `omarchy-shell <target> <method> [args...]` is the CLI for IPC (`$OMARCHY/bin/omarchy-shell`). It is the same path that the nightlight `IpcHandler` uses.
- `--exec` takes separate words. A one-word string with spaces is refused. The first word must not start with `-` (`Security.js parseExecArgv`). Untrusted values (JID) are safe as separate argv words.
- `-r <id>` replaces a toast. `-p` prints the new id. `-u` sets urgency, `-g` a glyph, `--image` a path.
- Alternative: `notify-send -A open=Open --wait`. It keeps a process alive until the click. It dies with a shell restart. I do not recommend it. Omarchy's own sender explains why it avoids `notify-send` for untrusted text (comment in `omarchy-notification-send`).
- Another bar widget can show several instances (one per monitor). The `IpcHandler` target is single. The widget base class has `broadcast(method)` for this (`Ui/BarWidget.qml`). Check which monitor's pop-out opens. Put the "pending chat" state in the Service.
- Attended Chat suppression is whisker's own logic. Omarchy has no API for it.

### 5. Quickshell.Io `Process`

Source: `src/io/process.hpp` and `process.cpp` in quickshell-mirror/quickshell (master), plus `/usr/lib/qt6/qml/Quickshell/Io/quickshell-io.qmltypes` for the installed build.

```qml
import Quickshell.Io
Process {
  id: daemon
  command: [Qt.resolvedUrl("bin/whisker-daemon").toString(), "--profile", dir]
  stdinEnabled: true                       // before start; false closes stdin
  stdout: SplitParser {                    // default splitMarker is "\n"
    onRead: function(line) { handle(line) }
  }
  stderr: SplitParser { onRead: function(line) { log(line) } }
  onExited: function(exitCode, exitStatus) { ... }
  running: true
}
daemon.write(JSON.stringify(req) + "\n")   // write(QString): UTF-8, no newline added
```

- Properties: `running`, `processId`, `command`, `workingDirectory`, `environment`, `clearEnvironment`, `stdout`, `stderr`, `stdinEnabled`. Methods: `exec()`, `signal(int)`, `write(string)`, `startDetached()`. Signals: `started`, `exited(exitCode, exitStatus)` (qmltypes lines 50-120).
- stdout and stderr: `SplitParser { onRead }` emits once per delimited chunk. The tail without a delimiter stays in a buffer (`datastream.cpp`). A null parser closes that channel. Omarchy uses this in `$OMARCHY/shell/Commons/InputDevices.qml` lines 54-70 (`stdout: SplitParser`, `onExited`). `StdioCollector` collects all output and suits one-shot commands such as `fetch-daemon.sh` (read `.text` in `onStreamFinished`, exit code in `onExited`).
- `stdinEnabled` defaults to `false`. In that state the write channel closes and `write()` does nothing, even if you set it back to `true` (`process.hpp`, `process.cpp` `startProcessIfReady`). Set it before `running: true`.
- Lines are not capped on the QML side, which matches section 7 of the architecture ("The Plugin reads with no cap"). I did not verify that from the parser source for very large chunks.
- Command: no shell. One string per argument. A first element starting with `file://` is stripped. `root://` resolves against the Quickshell config root (the Omarchy shell folder, not the plugin folder), so it does not suit a plugin. Use `Qt.resolvedUrl(...)`.
- If the binary is missing, `onErrorOccurred(FailedToStart)` logs a warning and emits `runningChanged`. `exited` does not fire in that case (`process.cpp`). Watch `running` as well as `exited`.
- Kill on exit or reload: `~Process()` calls `QProcess::kill()` (SIGKILL) when the QML object is destroyed, as at a config reload (`process.cpp` lines 31-39). The header says "The process will be killed when quickshell dies" and "See startDetached() to prevent the process from being killed by Quickshell if Quickshell is killed or the configuration is reloaded" (`process.hpp` lines 41-55). I did not run a kill test here. I did not verify the hard-crash case. The Daemon's exit on stdin EOF (architecture section 3) covers it.
- Clean stop: set `stdinEnabled = false` (closes stdin, EOF), then wait for `exited`. `running = false` sends SIGTERM only. For SIGKILL use `signal(9)`.
- Because the Service is `keepLoaded`, it is not destroyed on plugin hot-reload (see section 1). A shell restart does destroy it. A new shell can start before the old Daemon exits. The Profile lock in the architecture covers that race.

### 6. Tests

- Quickshell and Omarchy ship no test convention for third-party plugins. I found no plugin test guidance in `$OMARCHY/default/agents/skills/omarchy/plugins.md`.
- Installed plugins use two styles:
  - `node --test` on QML `.pragma library` JS files, loaded with `vm.runInContext` after stripping the pragma line: `jankeesvw.notification-center/tests/webapps.test.js` (first lines). Also `crmne.hyprmoncfg/.github/workflows/validate.yml` runs `node --test tests/*.test.js`.
  - `QtTest` through `/usr/lib/qt6/bin/qmltestrunner -platform offscreen -input tests/qml`: `evindor.keystroke/tests/tst_hotkeys.qml`, `crmne.hyprmoncfg/.github/workflows/validate.yml` line 28, `io.github.elevate08.qs-bitwarden-cli/docs/development.md` line 103. The latter notes that a bare `qmltestrunner` on Arch is the Qt5 one, so use the Qt6 path. QtTest is installed (`/usr/lib/qt6/qml/QtTest`). `qmllint` is at `/usr/lib/qt6/bin/qmllint`. `evindor.keystroke/tests/lint.sh` shows how to make the `qs` module importable with a symlink to `$OMARCHY/shell`.
- Lightest option for whisker: put the protocol logic in `.pragma library` JS files (no QML imports). Test them with `node --test`, using `fs.readFileSync` on `contract/fixtures/*.jsonl` and the `vm` loader. `node` is installed on this machine, through mise. It is not part of Omarchy, so it is a dev and CI dependency only.
- QML-side fixture reading: `qmltestrunner` can read local files with `XMLHttpRequest` only when `QML_XHR_ALLOW_FILE_READ=1` is set (Qt documentation, not run here). That makes `node --test` the simpler choice.

### 7. File picker

Installed Qt QML modules (`/usr/lib/qt6/qml`): `QtQuick`, `QtQuick/Controls`, `QtQuick/Dialogs`, `QtQuick/Layouts`, `QtQuick/Shapes`, `QtQuick/Effects`, `Qt/labs/{platform,folderlistmodel,settings,...}`, `QtTest`, `QtNetwork`, `QtQml`, `QtWayland`, `QtWebEngine`, `QtWebChannel`, `QtPositioning`. There is no `QtMultimedia`. `pacman -Q qt6-multimedia` reports it is not installed.

So `QtQuick.Dialogs` is installed here (`QtQuick/Dialogs/qmldir`, `plugins.qmltypes` lists `FileDialog`). It is also on the ship list of qt6-declarative. Do not assume other users have it, because it is an optional plugin (`optional plugin qtquickdialogsplugin`).

- Check run here: `import QtQuick.Dialogs` and `FileDialog { fileMode: FileDialog.OpenFile }` load without error in `/usr/lib/qt6/bin/qml` with `QT_QPA_PLATFORM=offscreen`. An unknown type fails. I did not open the dialog under Quickshell. I did not check that it appears over a layer-shell pop-out.
- The environment sets `QT_QPA_PLATFORMTHEME=gtk3` (`$OMARCHY/default/hypr/envs.lua` line 14). Qt then uses the GTK3 file dialog, or the QML fallback in `QtQuick/Dialogs/quickimpl`. `/usr/lib/qt6/plugins/platformthemes` has `libqgtk3.so` and `libqxdgdesktopportal.so`.
- Portals installed: `xdg-desktop-portal`, `xdg-desktop-portal-gtk`, `xdg-desktop-portal-hyprland`. `/usr/share/xdg-desktop-portal/hyprland-portals.conf` prefers `hyprland;gtk`, so the GTK portal serves FileChooser.
- No first-party plugin or installed plugin uses a file dialog. I grepped for `FileDialog`, `QtQuick.Dialogs`, `Qt.labs.platform`, `zenity`, `kdialog` and the portal name. No match.
- No `zenity`, `kdialog` or `yad` is installed.
- Fallbacks if `QtQuick.Dialogs` is missing, all unverified:
  1. Call the portal `org.freedesktop.portal.FileChooser.OpenFile` over D-Bus from the Daemon (Go). The Daemon owns files anyway. The Plugin sends `pickFile` and the Daemon returns a path.
  2. `Qt.labs.folderlistmodel` (installed) for a small in-pop-out browser.
  3. A path text field plus paste or drag-drop.
- Suggested order for v1: try `QtQuick.Dialogs.FileDialog` with a `Loader` guard (an optional import fails in a `Loader`, not at shell start). Test it once in the real shell. Fall back to the path field.

## Sources

Local, Omarchy shell (`/usr/share/omarchy`):
- `shell/README.md`, `shell/plugins/README.md`
- `shell/services/PluginRegistry.qml`, `bin/omarchy-plugin-validate`, `bin/omarchy-plugin-add`
- `shell/shell.qml` (`serviceFor`, `serviceKeepLoaded`, `unloadPluginServices`)
- `shell/Ui/{BarWidget,Panel,PanelController,KeyboardPanel,PanelKeyCatcher,TextField,FilterInput,BarIconButton,WidgetButton}.qml`
- `shell/Commons/{Color,Style,ShellIpc,qmldir}`
- `shell/plugins/panels/network/{Panel.qml,manifest.json}`
- `shell/plugins/notifications/{Service.qml,Security.js,manifest.json}`
- `shell/Commons/InputDevices.qml`
- `bin/omarchy-notification-send`, `bin/omarchy-shell`
- `default/agents/skills/omarchy/plugins.md`
- `default/hypr/envs.lua`

Local, installed plugins (`~/.config/omarchy/plugins`):
- `kenny.nightlight/{manifest.json,BarWidget.qml,Panel.qml,Service.qml}`
- `jankeesvw.notification-center/{manifest.json,tests/webapps.test.js}`
- `evindor.keystroke/tests/{lint.sh,tst_hotkeys.qml}`
- `crmne.hyprmoncfg/.github/workflows/validate.yml`
- `io.github.elevate08.qs-bitwarden-cli/docs/development.md`

Local, Quickshell and Qt:
- `/usr/lib/qt6/qml/Quickshell/Io/quickshell-io.qmltypes`
- `/usr/lib/qt6/qml/QtQuick/Dialogs/{qmldir,plugins.qmltypes}`
- `/usr/lib/qt6/qml/{QtTest,Qt/labs}`, `/usr/lib/qt6/plugins/platformthemes`
- `/usr/lib/qt6/bin/{qmltestrunner,qmllint,qml}`
- `/usr/share/xdg-desktop-portal/hyprland-portals.conf`

Remote (fetched 2026-10-24):
- https://raw.githubusercontent.com/quickshell-mirror/quickshell/master/src/io/process.hpp
- https://raw.githubusercontent.com/quickshell-mirror/quickshell/master/src/io/process.cpp
- https://raw.githubusercontent.com/quickshell-mirror/quickshell/master/src/io/datastream.hpp
- https://raw.githubusercontent.com/quickshell-mirror/quickshell/master/src/io/datastream.cpp
- Upstream master may differ from the installed build 0.3.2.r7. The qmltypes of the installed build match the API used above.

Not checked: github.com/basecamp/omarchy (its `docs/omarchy-shell.md`), quickshell.org docs pages, and any behavior of the dialog or notification click in a live session.
