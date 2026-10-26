const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const load = require("./load");

const ChatList = load("state/ChatList.js");
const Common = load("state/Common.js");
const Daemon = load("state/Daemon.js");
const Draft = load("state/Draft.js");
const Media = load("state/Media.js");
const Message = load("state/Message.js");
const NewChat = load("state/NewChat.js");
const Timeline = load("state/Timeline.js");
const root = path.join(__dirname, "..");
const fixtureDir = path.join(root, "contract", "fixtures");

// Seam B: fold the Daemon frames of each golden fixture, and check the view state at each "view" line.
for (const name of fs.readdirSync(fixtureDir).filter((f) => f.endsWith(".jsonl"))) {
  test(`fixture ${name}`, () => {
    let state = Daemon.initialState();
    const lines = fs.readFileSync(path.join(fixtureDir, name), "utf8").split("\n").filter(Boolean);
    for (const text of lines) {
      const line = JSON.parse(text);
      if (line.dir === "out") state = Daemon.applyFrame(state, line.frame);
      if (line.dir === "plugin" && line.openChat) state = Timeline.openChat(state, line.openChat);
      if (line.dir === "plugin" && line.requestHistory) state = Timeline.historyRequested(state);
      if (line.dir === "plugin" && line.lookup) state = NewChat.lookupStarted(state, line.lookup);
      if (line.dir === "view") {
        // The JSON round trip drops the prototypes of the vm context, so deep equality works.
        for (const [key, value] of Object.entries(line.expect)) assert.deepEqual(JSON.parse(JSON.stringify(state[key] ?? null)), value, key);
      }
    }
  });
}

test("the Plugin starts on the starting screen", () => {
  assert.equal(Daemon.initialState().screen, "starting");
});

test("a version mismatch shows an error screen", () => {
  const state = Daemon.checkVersion("1.2.3", "1.2.4\n");
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "version-mismatch");
});

test("a matching version allows the start", () => {
  assert.equal(Daemon.checkVersion("1.2.3", "1.2.3\n"), null);
});

test("a binary that does not run shows an error screen", () => {
  assert.equal(Daemon.checkVersion("1.2.3", "").reason, "binary-missing");
});

test("an oversized line exit shows its own error screen", () => {
  const state = Daemon.daemonExited(Daemon.initialState(), 10);
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "oversized-line");
});

test("any other Daemon exit shows an error screen", () => {
  const state = Daemon.daemonExited(Daemon.applyFrame(Daemon.initialState(), { method: "ready" }), 1);
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "daemon-exited");
});

test("a Profile in use exit shows its own error screen", () => {
  const state = Daemon.daemonExited(Daemon.initialState(), 11);
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "profile-in-use");
});

test("a clean Daemon exit shows the stopped screen", () => {
  assert.equal(Daemon.daemonExited(Daemon.initialState(), 0).reason, "daemon-exited");
});

test("the contract doc lists the Profile in use code", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  assert.ok(doc.includes(`| ${Daemon.EXIT_PROFILE_IN_USE} |`));
});

test("parseLine ignores a line that is not JSON", () => {
  assert.equal(Daemon.parseLine("log noise"), null);
  assert.equal(Daemon.parseLine('{"method":"ready"}').method, "ready");
});

test("a matching version starts the Daemon", () => {
  assert.equal(Daemon.versionStep("1.2.3", "1.2.3\n", false).action, "start");
});

test("a missing or wrong-version binary is downloaded once", () => {
  for (const output of ["", "1.2.4\n"]) {
    const step = Daemon.versionStep("1.2.3", output, false);
    assert.equal(step.action, "download");
    assert.equal(step.view.screen, "starting");
  }
});

test("a binary that is still wrong after the download shows an error screen", () => {
  const step = Daemon.versionStep("1.2.3", "1.2.4\n", true);
  assert.equal(step.action, "fail");
  assert.equal(step.view.reason, "version-mismatch");
});

test("a failed download shows the Retry error screen", () => {
  for (const code of [20, 21, 22, 23, 99]) {
    const state = Daemon.downloadFailed(code);
    assert.equal(state.screen, "error");
    assert.equal(state.reason, "download-failed");
    assert.equal(state.canRetry, true);
    assert.ok(state.message.startsWith("Daemon download failed"));
  }
  assert.ok(Daemon.downloadFailed(23).message.includes("checksum"));
});

test("other error screens offer no Retry", () => {
  assert.notEqual(Daemon.checkVersion("1.2.3", "").canRetry, true);
});

test("docs/releasing.md lists the fetch script exit codes", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "releasing.md"), "utf8");
  for (const code of Object.keys(Daemon.FETCH_EXIT_MESSAGES)) assert.ok(doc.includes(`| ${code} |`), code);
});

test("the contract doc lists the exit codes the Plugin maps", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  assert.ok(doc.includes(`| ${Daemon.EXIT_OVERSIZED_LINE} |`));
  assert.ok(doc.includes(`| ${Daemon.EXIT_PROFILE_IN_USE} |`));
});

test("the Plugin exit codes equal the Go constants of the contract package", () => {
  const go = fs.readFileSync(path.join(root, "daemon", "internal", "contract", "contract.go"), "utf8");
  const goCode = (name) => Number(new RegExp(`\\b${name}\\s*=\\s*(\\d+)`).exec(go)[1]);
  assert.equal(Daemon.EXIT_OVERSIZED_LINE, goCode("ExitOversizedLine"));
  assert.equal(Daemon.EXIT_PROFILE_IN_USE, goCode("ExitProfileInUse"));
});

test("the contract doc lists every error code that the Plugin maps", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  for (const code of [...Object.keys(Daemon.PAIR_CODE_ERRORS), ...Object.keys(Daemon.ERROR_NOTICES)]) {
    assert.ok(doc.includes(`| ${code} |`), code);
  }
});

test("an unavailable keyring shows a Retry action", () => {
  const state = Daemon.applyFrame(Daemon.initialState(), { jsonrpc: "2.0", method: "keyringUnavailable", params: {} });
  assert.equal(state.screen, "keyring-unavailable");
  assert.equal(state.canRetry, true);
});

test("the Retry request is a keyring.retry line", () => {
  const line = Daemon.keyringRetryRequest(7);
  assert.ok(line.endsWith("\n"));
  assert.deepEqual(JSON.parse(line), { jsonrpc: "2.0", id: 7, method: "keyring.retry" });
});

test("Retry asks the Daemon for a keyring problem and fetches otherwise", () => {
  assert.equal(Daemon.retryKind({ screen: "keyring-unavailable" }), "keyring");
  assert.equal(Daemon.retryKind(Daemon.downloadFailed(22)), "download");
});

test("a QR refresh replaces the code and keeps the Pair Code", () => {
  let state = Daemon.applyFrame(Daemon.initialState(), { method: "linkQr", params: { qr: "data:a" } });
  state = Daemon.applyFrame(state, { id: 1, result: { code: "ABCD-1234" } });
  state = Daemon.applyFrame(state, { method: "linkQr", params: { qr: "data:b" } });
  assert.equal(state.qr, "data:b");
  assert.equal(state.pairCode, "ABCD-1234");
});

test("every link failure reason has its own message and a Retry", () => {
  for (const reason of Object.keys(Daemon.LINK_FAILURES)) {
    const state = Daemon.applyFrame(Daemon.initialState(), { method: "linkFailed", params: { reason } });
    assert.equal(state.message, Daemon.LINK_FAILURES[reason]);
    assert.equal(state.canRetry, true);
  }
  assert.equal(Daemon.applyFrame(Daemon.initialState(), { method: "linkFailed", params: { reason: "new" } }).message, Daemon.LINK_FAILURES.unexpected);
});

test("the contract doc lists every link failure reason", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  for (const reason of Object.keys(Daemon.LINK_FAILURES)) assert.ok(doc.includes(`| \`${reason}\` |`), reason);
});

test("the Pair Code request keeps only the digits of the phone number", () => {
  const line = Daemon.pairCodeRequest(3, "+31 6 1234-5678");
  assert.ok(line.endsWith("\n"));
  assert.deepEqual(JSON.parse(line), { jsonrpc: "2.0", id: 3, method: "link.pairCode", params: { phone: "31612345678" } });
});

test("a merged Chat reloads the open Chat only when it is one of the two", () => {
  const frame = { method: "chatMerged", params: { from: "1@lid", to: "2@s.whatsapp.net" } };
  const open = (jid) => Timeline.openChat(Daemon.initialState(), { jid, name: "A", kind: "direct" });
  for (const jid of ["1@lid", "2@s.whatsapp.net"]) {
    const after = Daemon.applyFrame(open(jid), frame);
    assert.equal(after.openChat.jid, "2@s.whatsapp.net");
    assert.equal(Timeline.reloadsOpenChat(after, frame), true);
  }
  const other = Daemon.applyFrame(open("3@s.whatsapp.net"), frame);
  assert.equal(other.openChat.jid, "3@s.whatsapp.net");
  assert.equal(Timeline.reloadsOpenChat(other, frame), false);
});

test("the Plugin asks for the Chat list after linked and after chatsChanged only", () => {
  assert.equal(Daemon.refreshesChats({ method: "linked" }), true);
  assert.equal(Daemon.refreshesChats({ method: "chatsChanged" }), true);
  assert.equal(Daemon.refreshesChats({ method: "ready" }), false);
  assert.equal(Daemon.refreshesChats({ id: 1, result: {} }), false);
});

test("the Chat list request carries the offset", () => {
  const line = ChatList.chatListRequest(5, 50);
  assert.ok(line.endsWith("\n"));
  assert.deepEqual(JSON.parse(line), { jsonrpc: "2.0", id: 5, method: "chats.list", params: { offset: 50 } });
});

test("the Chat list request carries the search query only when it is set", () => {
  assert.deepEqual(JSON.parse(ChatList.chatListRequest(6, 0, "ann")).params, { offset: 0, query: "ann" });
  assert.deepEqual(JSON.parse(ChatList.chatListRequest(7, 0, "")).params, { offset: 0 });
});

test("the Chat list request carries archived only when it is true", () => {
  assert.deepEqual(JSON.parse(ChatList.chatListRequest(8, 0, "", true)).params, { offset: 0, archived: true });
  assert.deepEqual(JSON.parse(ChatList.chatListRequest(9, 0, "", false)).params, { offset: 0 });
});

test("the Chat list survives a chatsChanged frame and goes away on unlinked", () => {
  let state = Daemon.applyFrame(Daemon.initialState(), { method: "linked" });
  state = Daemon.applyFrame(state, { id: 1, result: { chats: [{ jid: "a" }], more: false, offset: 0 } });
  assert.equal(Daemon.applyFrame(state, { method: "chatsChanged" }).chats.length, 1);
  assert.equal(Daemon.applyFrame(state, { method: "unlinked" }).chats, undefined);
});

test("Retry on the linking screen restarts linking", () => {
  const failed = Daemon.applyFrame(Daemon.initialState(), { method: "linkFailed", params: { reason: "timeout" } });
  assert.equal(Daemon.retryKind(failed), "link");
  assert.deepEqual(JSON.parse(Daemon.linkRetryRequest(4)), { jsonrpc: "2.0", id: 4, method: "link.retry" });
});

test("the Profile Settings survive the frames that reset the view", () => {
  const settings = { readReceipts: "off", sendOnline: true, sendTyping: true, notifications: true };
  let state = Daemon.applyFrame(Daemon.initialState(), { id: 1, result: settings });
  state = Daemon.applyFrame(state, { method: "unlinked", params: {} });
  assert.deepEqual(JSON.parse(JSON.stringify(state.settings)), settings);
  assert.equal(Daemon.applyFrame(state, { method: "ready" }).settings.readReceipts, "off");
});

test("a phone unlink and a Log out show different messages", () => {
  const phone = Daemon.applyFrame(Daemon.initialState(), { method: "unlinked", params: {} });
  const user = Daemon.applyFrame(Daemon.initialState(), { method: "unlinked", params: { by: "user" } });
  assert.equal(phone.reason, "unlinked");
  assert.equal(user.reason, "logged-out");
});

test("the settings requests are JSON-RPC lines", () => {
  assert.deepEqual(JSON.parse(Daemon.settingsGetRequest(1)), { jsonrpc: "2.0", id: 1, method: "settings.get" });
  assert.deepEqual(JSON.parse(Daemon.settingsSetRequest(2, { sendTyping: false })), { jsonrpc: "2.0", id: 2, method: "settings.set", params: { sendTyping: false } });
  assert.deepEqual(JSON.parse(Daemon.logoutRequest(3)), { jsonrpc: "2.0", id: 3, method: "profile.logout" });
  assert.ok(Daemon.logoutRequest(3).endsWith("\n"));
});

test("the Plugin reads the Profile Settings after ready only", () => {
  assert.equal(Daemon.requestsSettings({ method: "ready" }), true);
  assert.equal(Daemon.requestsSettings({ method: "linked" }), false);
});

test("the settings view lists the Read Receipt modes of the Daemon", () => {
  assert.deepEqual(JSON.parse(JSON.stringify(Daemon.READ_RECEIPT_MODES.map((m) => m.value))), ["off", "onOpen", "onInteraction"]);
});

const direct = { jid: "1@s.whatsapp.net", name: "Ann", kind: "direct" };
const group = { jid: "g1@g.us", name: "Team", kind: "group" };
const theirs = { id: "a", chat: group.jid, sender: "2@s.whatsapp.net", senderName: "Bob", fromMe: false, time: 5, text: "hi", state: "" };
const mine = { id: "b", chat: group.jid, sender: "", senderName: "", fromMe: true, time: 6, text: "yo", state: "pending" };

test("the connection starts as connecting after linked and follows the Daemon", () => {
  let state = Daemon.applyFrame(Daemon.initialState(), { method: "linked" });
  assert.equal(state.connection, "connecting");
  assert.equal(Daemon.connectionBanner(state), "Connecting…");
  state = Daemon.applyFrame(state, { method: "connected" });
  assert.equal(Daemon.connectionBanner(state), "");
  state = Daemon.applyFrame(state, { method: "disconnected" });
  assert.equal(Daemon.connectionBanner(state), "Offline");
});

test("the bar icon dims on a screen that is not ready or linked, and while offline", () => {
  const linked = Daemon.applyFrame(Daemon.initialState(), { method: "linked" });
  assert.equal(Daemon.barDimmed(Daemon.initialState()), true);
  assert.equal(Daemon.barDimmed(linked), false);
  assert.equal(Daemon.barDimmed(Daemon.applyFrame(linked, { method: "disconnected" })), true);
  assert.equal(Daemon.barDimmed(Daemon.applyFrame(linked, { method: "connected" })), false);
});

test("Enter sends, Shift+Enter adds a line, an empty Enter does nothing", () => {
  assert.equal(Draft.composerAction(Common.KEY_RETURN, false, "hi"), "send");
  assert.equal(Draft.composerAction(Common.KEY_ENTER, false, "hi"), "send");
  assert.equal(Draft.composerAction(Common.KEY_RETURN, true, "hi"), "default");
  assert.equal(Draft.composerAction(Common.KEY_RETURN, false, " \n "), "swallow");
  assert.equal(Draft.composerAction(0x41, false, "hi"), "default");
  assert.equal(Draft.composerAction(Common.KEY_RETURN, false, "", true), "send");
  assert.equal(Draft.composerAction(Common.KEY_RETURN, false, " ", false), "swallow");
  assert.equal(Draft.composerAction(Draft.KEY_V, false, "", false, true, ""), "paste");
  assert.equal(Draft.composerAction(Draft.KEY_V, false, "", false, true, "m1"), "default"); // an Edit pastes text only
  assert.equal(Draft.composerAction(Draft.KEY_V, false, "", false, false, ""), "default");
});

test("the send button works when Enter would send", () => {
  assert.equal(Draft.canSend("hi", false), true);
  assert.equal(Draft.canSend(" \n ", false), false);
  assert.equal(Draft.canSend("", true), true);
});

test("a line shows the sender name of others and You for the user", () => {
  assert.equal(Timeline.senderLabel(theirs), "Bob");
  assert.equal(Timeline.senderLabel(mine), "You");
  assert.equal(Timeline.senderLabel({ fromMe: false, senderName: "Ann" }), "Ann");
});

test("Failed has Retry, other states do not", () => {
  assert.equal(Timeline.canRetryMessage({ fromMe: true, state: "failed" }), true);
  assert.equal(Timeline.canRetryMessage({ fromMe: true, state: "sent" }), false);
});

test("a Message of another Chat or an unopened Chat does not change the view", () => {
  const opened = Timeline.openChat(Daemon.applyFrame(Daemon.initialState(), { method: "linked" }), direct);
  const other = Daemon.applyFrame(opened, { method: "messageChanged", params: { message: theirs } });
  assert.equal(other.openChat.messages.length, 0);
  const none = Daemon.applyFrame(Daemon.applyFrame(Daemon.initialState(), { method: "linked" }), { method: "messageChanged", params: { message: theirs } });
  assert.equal(none.openChat, undefined);
});

test("Messages keep their time order and a repeat replaces the Message", () => {
  let state = Timeline.openChat(Daemon.applyFrame(Daemon.initialState(), { method: "linked" }), group);
  state = Daemon.applyFrame(state, { method: "messageChanged", params: { message: mine } });
  state = Daemon.applyFrame(state, { method: "messageChanged", params: { message: theirs } });
  assert.deepEqual([...state.openChat.messages.map((m) => m.id)], ["a", "b"]);
  state = Daemon.applyFrame(state, { method: "messageChanged", params: { message: Object.assign({}, mine, { state: "sent" }) } });
  assert.deepEqual([...state.openChat.messages.map((m) => m.state)], ["", "sent"]);
});

test("a page of another Chat is dropped", () => {
  const opened = Timeline.openChat(Daemon.applyFrame(Daemon.initialState(), { method: "linked" }), direct);
  const state = Daemon.applyFrame(opened, { id: 1, result: { chat: group.jid, messages: [theirs], more: false, offset: 0 } });
  assert.equal(state.openChat.messages.length, 0);
});

test("closing the Chat leaves the Chat list", () => {
  const opened = Timeline.openChat(Daemon.applyFrame(Daemon.initialState(), { method: "linked" }), direct);
  assert.equal(Timeline.closeChat(opened).openChat, undefined);
});

test("the Chat requests are JSON-RPC lines", () => {
  assert.deepEqual(JSON.parse(Timeline.messagesRequest(1, direct.jid, 50)), { jsonrpc: "2.0", id: 1, method: "chat.messages", params: { chat: direct.jid, offset: 50 } });
  assert.deepEqual(JSON.parse(Draft.sendRequest(2, direct.jid, "hi\nthere")), { jsonrpc: "2.0", id: 2, method: "chat.send", params: { chat: direct.jid, text: "hi\nthere" } });
  assert.deepEqual(JSON.parse(Timeline.retryMessageRequest(3, direct.jid, "m1")), { jsonrpc: "2.0", id: 3, method: "chat.retry", params: { chat: direct.jid, id: "m1" } });
  assert.ok(Draft.sendRequest(2, direct.jid, "hi").endsWith("\n"));
});

test("the contract doc lists the Chat methods and the message error code", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  for (const needle of ["`chat.messages`", "`chat.send`", "`chat.retry`", "`chat.edit`", "`chat.revoke`", "`messageChanged`", "`connected`", "`disconnected`", "| -32006 |", "| -32007 |"])
    assert.ok(doc.includes(needle), needle);
});

test("the attention request carries the pop-out state and the shown Chat", () => {
  assert.deepEqual(JSON.parse(Daemon.attentionRequest(3, true, direct.jid)), { jsonrpc: "2.0", id: 3, method: "attention.set", params: { open: true, chat: direct.jid } });
  assert.equal(JSON.parse(Daemon.attentionRequest(4, true)).params.chat, "");
  assert.equal(JSON.parse(Daemon.attentionRequest(5, false, direct.jid)).params.open, false);
});

test("a notification click asks the Daemon for the Chat of its token", () => {
  assert.deepEqual(JSON.parse(Daemon.notificationOpenRequest(7, "ab12")), { jsonrpc: "2.0", id: 7, method: "notification.open", params: { token: "ab12" } });
  assert.deepEqual(JSON.parse(JSON.stringify(Daemon.notificationOpened(7, { jsonrpc: "2.0", id: 7, result: { chat: direct.jid } }))), { action: "openChat", chat: direct.jid });
  assert.deepEqual(JSON.parse(JSON.stringify(Daemon.notificationOpened(8, { jsonrpc: "2.0", id: 8, error: { code: -32010, message: "invalid chat" } }))), { action: "showList" });
  assert.equal(Daemon.notificationOpened(7, { jsonrpc: "2.0", id: 9, result: {} }), null);
  assert.equal(Daemon.notificationOpened(0, { jsonrpc: "2.0", id: 0, result: {} }), null);
});

test("the Plugin sends the pop-out state again when the Daemon is ready", () => {
  assert.equal(Daemon.resendsAttention({ method: "ready" }), true);
  assert.equal(Daemon.resendsAttention({ method: "linked" }), false);
});

const sentMessage = (extra) => Object.assign({ id: "m1", chat: direct.jid, fromMe: true, time: 1000, text: "hi", state: "sent", edited: false, revoked: false }, extra);

test("a mention is bold, other text is escaped", () => {
  const url = "https://www.openstreetmap.org/?mlat=51.500000&mlon=-0.120000#map=17/51.500000/-0.120000";
  assert.equal(Message.messageHtml(sentMessage({ text: "@Zed <b>look</b>", mentions: ["@Zed"] })), "<b>@Zed</b> &lt;b&gt;look&lt;/b&gt;");
  assert.equal(Message.messageHtml(sentMessage({ text: "Location: Cafe\n" + url })), `Location: Cafe<br>${url.replace(/&/g, "&amp;")}`);
  assert.equal(Message.messageHtml(sentMessage({ text: "", revoked: true, mentions: ["@Zed"] })), "This message was deleted");
});

// The map link opens from a button: a link inside the Message text did not take clicks in the pop-out.
test("a location has a map link for its Open map button", () => {
  const url = "https://www.openstreetmap.org/?mlat=51.500000&mlon=-0.120000#map=17/51.500000/-0.120000";
  assert.equal(Message.mapLink(sentMessage({ text: "Location: Cafe\n" + url })), url);
  assert.equal(Message.mapLink(sentMessage({ text: "see https://evil.example/" })), "");
  assert.equal(Message.mapLink(sentMessage({ text: "Location: Cafe\n" + url, revoked: true })), "");
});

test("only a map link opens", () => {
  const url = "https://www.openstreetmap.org/?mlat=51.5&mlon=-0.12#map=17/51.5/-0.12";
  assert.equal(JSON.stringify(Message.linkCommand(url)), JSON.stringify(["xdg-open", url])); // xdg-open rejects "--"
  assert.equal(Message.linkCommand("https://evil.example/").length, 0);
  assert.equal(Message.linkCommand(url + " ; rm").length, 0);
});

test("a Tombstone shows a fixed text, an edited Message shows (edited)", () => {
  assert.equal(Message.messageBody(sentMessage({ revoked: true, text: "" })), "This message was deleted");
  assert.equal(Message.messageBody(sentMessage()), "hi");
  assert.equal(Message.editedLabel(sentMessage({ edited: true })), "(edited)");
  assert.equal(Message.editedLabel(sentMessage()), "");
  assert.equal(Message.editedLabel(sentMessage({ edited: true, revoked: true })), "");
});

test("Edit and Delete show for sent Messages of the user; Edit ends after 15 minutes", () => {
  assert.equal(Message.canEditMessage(sentMessage(), 1000 + 900), true);
  assert.equal(Message.canEditMessage(sentMessage(), 1000 + 901), false);
  assert.equal(Message.canEditMessage(sentMessage({ mentions: ["@Ann"] }), 1000 + 900), false);
  assert.equal(Message.canRevokeMessage(sentMessage(), 1000 + 99999), true);
  for (const state of ["pending", "failed", ""]) assert.equal(Message.canRevokeMessage(sentMessage({ state })), false, state);
  assert.equal(Message.canRevokeMessage(sentMessage({ fromMe: false })), false);
  assert.equal(Message.canRevokeMessage(sentMessage({ revoked: true })), false);
});

// The constants below have a Go twin. Read the Go source so that a change on one side fails here.
test("EDIT_WINDOW_SECONDS agrees with EditWindow in Go", () => {
  const go = fs.readFileSync(path.join(root, "daemon", "internal", "contract", "edit.go"), "utf8");
  const minutes = Number(/EditWindow = (\d+) \* time\.Minute/.exec(go)[1]);
  assert.equal(Message.EDIT_WINDOW_SECONDS, minutes * 60);
});

const goStrings = (file, name) => {
  const go = fs.readFileSync(path.join(root, "daemon", "internal", ...file), "utf8");
  return Array.from(go.matchAll(new RegExp(`\\b${name}\\w* += "(\\w+)"`, "g")), (m) => m[1]).sort();
};

test("MEDIA_STATE agrees with the Media states in Go", () => {
  assert.deepEqual(Object.values(Media.MEDIA_STATE).sort(), goStrings(["chatstore", "messages.go"], "Media"));
});

test("AUTO_DOWNLOAD_LIMITS agrees with the limits in Go", () => {
  assert.deepEqual(Array.from(Daemon.AUTO_DOWNLOAD_LIMITS, (m) => m.value).sort(), goStrings(["settings", "settings.go"], "AutoDownloadLimit"));
});

test("READ_RECEIPT_MODES agrees with the Read Receipt modes in Go", () => {
  assert.deepEqual(Array.from(Daemon.READ_RECEIPT_MODES, (m) => m.value).sort(), goStrings(["settings", "settings.go"], "ReadReceipts"));
});

test("every notification with an effect is documented in docs/contract.md", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  for (const method of Object.keys(Daemon.NOTIFICATIONS)) assert.ok(doc.includes("`" + method + "`"), method);
});

test("REACTION_EMOJI agrees with ReactionEmoji in Go", () => {
  const go = fs.readFileSync(path.join(root, "daemon", "internal", "whatsapp", "whatsapp.go"), "utf8");
  const list = /ReactionEmoji = \[\]string\{([^}]*)\}/.exec(go)[1];
  assert.deepEqual(Array.from(Message.REACTION_EMOJI), list.split(",").map((e) => JSON.parse(e.trim())));
});

test("the composer sends an edit when a Message is under edit, otherwise a new Message", () => {
  assert.deepEqual(JSON.parse(Draft.composerRequest(4, direct.jid, "m1", "fixed")), { jsonrpc: "2.0", id: 4, method: "chat.edit", params: { chat: direct.jid, id: "m1", text: "fixed" } });
  assert.deepEqual(JSON.parse(Draft.composerRequest(5, direct.jid, "", "hi")), { jsonrpc: "2.0", id: 5, method: "chat.send", params: { chat: direct.jid, text: "hi" } });
  assert.deepEqual(JSON.parse(Message.revokeRequest(6, direct.jid, "m1")), { jsonrpc: "2.0", id: 6, method: "chat.revoke", params: { chat: direct.jid, id: "m1" } });
  assert.deepEqual(JSON.parse(Message.deleteForMeRequest(7, direct.jid, "m1")), { jsonrpc: "2.0", id: 7, method: "chat.deleteForMe", params: { chat: direct.jid, id: "m1" } });
});

test("sendMediaRequest sends the paths as they are", () => {
  assert.deepEqual(JSON.parse(Draft.sendMediaRequest(7, direct.jid, ["/home/me/100%.pdf", "/b.png"], "hi")), { jsonrpc: "2.0", id: 7, method: "chat.sendMedia", params: { chat: direct.jid, paths: ["/home/me/100%.pdf", "/b.png"], caption: "hi" } });
});

test("Reactions: the user picks, replaces or removes one; chips count the emoji", () => {
  const msg = { id: "m1", revoked: false, fromMe: false, reactions: [{ emoji: "👍", fromMe: false }, { emoji: "🙏", fromMe: true }, { emoji: "👍", fromMe: false }] };
  assert.equal(Message.myReaction(msg), "🙏");
  assert.equal(Message.reactionChoice(msg, "🙏"), "");
  assert.equal(Message.reactionChoice(msg, "😂"), "😂");
  assert.equal(Message.reactionChoice({ reactions: [] }, "😂"), "😂");
  assert.deepEqual(JSON.parse(JSON.stringify(Message.reactionChips(msg))), [{ emoji: "👍", count: 2, mine: false, names: ", " }, { emoji: "🙏", count: 1, mine: true, names: "You" }]);
  assert.deepEqual(JSON.parse(JSON.stringify(Message.reactionChips({ id: "m2" }))), []);
  assert.deepEqual(JSON.parse(JSON.stringify(Message.reactionChips({ ...msg, revoked: true }))), []);
  assert.deepEqual(Array.from(Message.REACTION_EMOJI), ["👍", "❤️", "😂", "😮", "😢", "🙏"]);
  assert.deepEqual(JSON.parse(Message.reactRequest(3, "c@s.whatsapp.net", "m1", "")), { jsonrpc: "2.0", id: 3, method: "chat.react", params: { chat: "c@s.whatsapp.net", id: "m1", emoji: "" } });
  assert.equal(Message.canReactToMessage({ fromMe: true, state: "pending" }), false);
  assert.equal(Message.canReactToMessage({ fromMe: true, state: "sent" }), true);
  assert.equal(Message.canReactToMessage({ fromMe: false, state: "" }), true);
  assert.equal(Message.canReactToMessage({ fromMe: false, revoked: true }), false);
});

test("Quotes: only a verified Quote shows \"You\"; an unverified one is marked and names no user", () => {
  const view = (quote) => JSON.parse(JSON.stringify(Message.quoteView({ quote })));
  assert.deepEqual(view({ verified: true, fromMe: true, senderName: "", text: "hi" }), { sender: "You", text: "hi" });
  assert.deepEqual(view({ verified: true, fromMe: false, sender: "1@s.whatsapp.net", senderName: "Ann", senderPush: true, text: "hi" }), { sender: "~ Ann +1", text: "hi" });
  assert.deepEqual(view({ verified: true, fromMe: false, senderName: "Ann", revoked: true, text: "" }), { sender: "Ann", text: "This message was deleted" });
  assert.deepEqual(view({ verified: false, fromMe: true, senderName: "", text: "forged" }), { sender: "Unknown", text: "forged", unverified: true });
  assert.deepEqual(view({ verified: false, fromMe: false, sender: "1@s.whatsapp.net", senderName: "Ann", text: "forged" }), { sender: "Ann", text: "forged", unverified: true });
});

test("Quotes: the preview names the author and is cut to one line; the composer sends the Quote", () => {
  assert.equal(Message.quoteView({ text: "no quote" }), null);
  assert.deepEqual(JSON.parse(JSON.stringify(Message.quoteView({ quote: { fromMe: true, senderName: "", text: "a\nb" } }))), { sender: "You", text: "a b" });
  const long = Message.quoteView({ quote: { fromMe: false, senderName: "Ann", text: "x".repeat(200) } });
  assert.equal(long.sender, "Ann");
  assert.equal(long.text.length, 80);
  assert.ok(long.text.endsWith("…"));
  assert.equal(Message.canQuoteMessage({ revoked: true }), false);
  assert.equal(Message.canQuoteMessage({ revoked: false }), true);
  const jid = "c@s.whatsapp.net";
  assert.deepEqual(JSON.parse(Draft.composerRequest(1, jid, "", "re", "m1")).params, { chat: jid, text: "re", quote: "m1" });
  assert.deepEqual(JSON.parse(Draft.composerRequest(2, jid, "", "re", "")).params, { chat: jid, text: "re" });
  assert.equal(JSON.parse(Draft.composerRequest(3, jid, "m1", "fix", "m2")).method, "chat.edit");
});

test("presenceLabel shows Online and Last Seen in a Direct Chat only", () => {
  const now = Math.floor(new Date(2026, 9, 24, 15, 0).getTime() / 1000);
  const seen = Math.floor(new Date(2026, 9, 24, 14, 5).getTime() / 1000);
  const old = Math.floor(new Date(2026, 9, 20, 14, 5).getTime() / 1000);
  const open = (kind) => Timeline.openChat(Daemon.initialState(), { jid: "1@s.whatsapp.net", name: "Ann", kind });
  const seen_ = (state, p) => Daemon.applyFrame(state, { method: "presenceChanged", params: { chat: "1@s.whatsapp.net", ...p } });
  assert.equal(ChatList.presenceLabel(open("direct"), now), "");
  assert.equal(ChatList.presenceLabel(seen_(open("direct"), { online: true, lastSeen: 0 }), now), "Online");
  assert.equal(ChatList.presenceLabel(seen_(open("direct"), { online: false, lastSeen: seen }), now), "last seen at 14:05");
  assert.equal(ChatList.presenceLabel(seen_(open("direct"), { online: false, lastSeen: old }), now), "last seen 2026-10-20");
  assert.equal(ChatList.presenceLabel(seen_(open("direct"), { online: false, lastSeen: 0 }), now), "");
  assert.equal(ChatList.presenceLabel(seen_(open("group"), { online: true, lastSeen: 0 }), now), "");
  const hidden = (p) => ({ ...seen_(open("direct"), p), settings: { sendOnline: false } });
  assert.equal(ChatList.presenceLabel(hidden({ online: true, lastSeen: 0 }), now), "");
  assert.equal(ChatList.presenceLabel(hidden({ online: false, lastSeen: seen }), now), "");
});

test("ownOnline is true only when connected with send online on", () => {
  assert.equal(Daemon.ownOnline({ connection: "online", settings: { sendOnline: true } }), true);
  assert.equal(Daemon.ownOnline({ connection: "online", settings: { sendOnline: false } }), false);
  assert.equal(Daemon.ownOnline({ connection: "offline", settings: { sendOnline: true } }), false);
  assert.equal(Daemon.ownOnline({ connection: "online" }), false);
});

test("activityLabel names the Participant in a Group Chat and drops it on a connection loss", () => {
  const act = (state, chat, participant, name, s) =>
    Daemon.applyFrame(state, { method: "activityChanged", params: { chat, participant, name, state: s } });
  let direct = Timeline.openChat(Daemon.initialState(), { jid: "1@s.whatsapp.net", name: "Ann", kind: "direct" });
  direct = act(direct, "1@s.whatsapp.net", "1@s.whatsapp.net", "Ann", "composing");
  assert.equal(ChatList.activityLabel(direct), "typing…");
  direct = act(direct, "1@s.whatsapp.net", "1@s.whatsapp.net", "Ann", "recording");
  assert.equal(ChatList.activityLabel(direct), "recording…");
  assert.equal(ChatList.activityLabel(Daemon.applyFrame(direct, { method: "disconnected" })), "");
  let group = Timeline.openChat(Daemon.initialState(), { jid: "g@g.us", name: "Team", kind: "group" });
  group = act(act(group, "g@g.us", "2@s", "Bob", "composing"), "g@g.us", "3@s", "Cy", "recording");
  assert.equal(ChatList.activityLabel(group), "Bob is typing…, Cy is recording…");
});

test("typingAction sends Composing on the first text and Paused when the text empties", () => {
  assert.equal(Draft.typingAction(false, "h"), "start");
  assert.equal(Draft.typingAction(true, "hi"), "");
  assert.equal(Draft.typingAction(true, "  "), "stop");
  assert.equal(Draft.typingAction(false, ""), "");
  assert.deepEqual(JSON.parse(Draft.typingRequest(3, "1@s", true)), { jsonrpc: "2.0", id: 3, method: "chat.typing", params: { chat: "1@s", typing: true } });
});

test("typingAction does not send Composing for text that came from the Draft", () => {
  assert.equal(Draft.typingAction(false, "saved", "saved"), "");
  assert.equal(Draft.typingAction(false, "saved!", "saved"), "start");
  assert.equal(Draft.typingAction(true, "", ""), "stop"); // cancelling an Edit still sends Paused
});

test("chatStatusLabel prefers the activity over Presence", () => {
  let s = Timeline.openChat(Daemon.initialState(), { jid: "1@s", name: "Ann", kind: "direct" });
  s = Daemon.applyFrame(s, { method: "presenceChanged", params: { chat: "1@s", online: true, lastSeen: 0 } });
  assert.equal(ChatList.chatStatusLabel(s, 0), "Online");
  s = Daemon.applyFrame(s, { method: "activityChanged", params: { chat: "1@s", participant: "1@s", name: "Ann", state: "composing" } });
  assert.equal(ChatList.chatStatusLabel(s, 0), "typing…");
});

test("fixed shortcuts: Escape goes back one level, Ctrl+F searches the Chat list", () => {
  const list = { screen: "linked" };
  const none = { showSettings: false };
  assert.equal(Daemon.shortcutAction("Escape", { ...list, openChat: { jid: "1" } }, none), "closeChat");
  assert.equal(Daemon.shortcutAction("Escape", list, { showSettings: true }), "closeSettings");
  assert.equal(Daemon.shortcutAction("Escape", list, none), "closePopout");
  assert.equal(Daemon.shortcutAction("Ctrl+F", list, none), "focusSearch");
  assert.equal(Daemon.shortcutAction("Ctrl+F", { ...list, openChat: { jid: "1" } }, none), "none");
  assert.equal(Daemon.shortcutAction("Ctrl+F", list, { showSettings: true }), "none");
  assert.equal(Daemon.shortcutAction("Ctrl+F", { screen: "linking" }, none), "none");
  assert.equal(Daemon.shortcutAction("F5", list, none), "none");
});

const plain = (value) => JSON.parse(JSON.stringify(value));
const noLocal = { pairMode: false, showSettings: false };

test("panelParts: each part shows on its screen only", () => {
  const linked = { screen: Daemon.SCREEN.LINKED, connection: "offline", settings: { sendOnline: true }, moreChats: true };
  assert.deepEqual(plain(Daemon.panelParts(linked, noLocal)), {
    error: false, banner: true, qr: false, pairForm: false, pairCode: false, pairToggle: false,
    chatList: true, loadMoreChats: true, newChat: false, newChatButton: true, emptyChat: false, settingsButton: true, settings: false, chat: false,
  });
  assert.equal(Daemon.panelParts(linked, { ...noLocal, showSettings: true }).settings, true);
  assert.equal(Daemon.panelParts(linked, { ...noLocal, showSettings: true }).chatList, false);
  assert.equal(Daemon.panelParts(linked, { ...noLocal, showSettings: true }).loadMoreChats, false);
  const inChat = { ...linked, openChat: { jid: "1@s.whatsapp.net" } };
  const chat = Daemon.panelParts(inChat, noLocal);
  assert.deepEqual([chat.chat, chat.chatList, chat.settingsButton, chat.loadMoreChats], [true, false, false, false]);
  assert.equal(Daemon.panelParts({ ...linked, settings: undefined }, { ...noLocal, showSettings: true }).settings, false);
});

test("panelParts: linking shows the QR code or the Pair Code form, and an error shows as error", () => {
  const linking = { screen: Daemon.SCREEN.LINKING, qr: "data:image/png;base64,AA" };
  assert.equal(Daemon.panelParts(linking, noLocal).qr, true);
  assert.equal(Daemon.panelParts(linking, { ...noLocal, pairMode: true }).qr, false);
  assert.equal(Daemon.panelParts(linking, { ...noLocal, pairMode: true }).pairForm, true);
  assert.equal(Daemon.panelParts(linking, noLocal).pairToggle, true);
  assert.equal(Daemon.panelParts({ screen: Daemon.SCREEN.ERROR }, noLocal).error, true);
  assert.equal(Daemon.panelParts({ screen: Daemon.SCREEN.STARTING }, noLocal).pairToggle, false);
});

test("panelParts: the Pair Code shows only in pair mode, once the Daemon sent it", () => {
  const linking = { screen: Daemon.SCREEN.LINKING, qr: "data:image/png;base64,AA", pairCode: "" };
  assert.equal(Daemon.panelParts(linking, { ...noLocal, pairMode: true }).pairCode, false);
  assert.equal(Daemon.panelParts({ ...linking, pairCode: "ABCD-1234" }, { ...noLocal, pairMode: true }).pairCode, true);
  assert.equal(Daemon.panelParts({ ...linking, pairCode: "ABCD-1234" }, noLocal).pairCode, false);
});

test("linkMessage: the line above the QR code or the Pair Code form fits the mode", () => {
  let state = Daemon.applyFrame(Daemon.initialState(), { method: "linkQr", params: { qr: "data:a" } });
  assert.equal(Daemon.linkMessage(state, false), "Scan this QR code in WhatsApp: Linked devices, Link a device.");
  assert.equal(Daemon.linkMessage(state, true), "Type the phone number of your WhatsApp account, with the country code. Then press Enter.");
  state = Daemon.applyFrame(state, { id: 1, result: { code: "ABCD-1234" } });
  assert.equal(Daemon.linkMessage(state, true), "Enter this code in WhatsApp: Linked devices, Link a device, Link with phone number instead.");
  assert.equal(Daemon.linkMessage(state, false), "Scan this QR code in WhatsApp: Linked devices, Link a device.");
  const failed = { screen: Daemon.SCREEN.LINKING, qr: "", message: "The QR code expired. Select Retry for a new one." };
  assert.equal(Daemon.linkMessage(failed, false), failed.message);
  assert.equal(Daemon.linkMessage(failed, true), failed.message);
  assert.equal(Daemon.linkMessage({ screen: Daemon.SCREEN.ERROR, message: "boom" }, true), "boom");
});

test("readReceiptDescription follows the Read Receipts mode that is set", () => {
  const mode = Daemon.READ_RECEIPT_MODES[1];
  assert.equal(Daemon.readReceiptDescription({ readReceipts: mode.value }), mode.description);
  assert.equal(Daemon.readReceiptDescription({ readReceipts: "other" }), "");
  assert.equal(Daemon.readReceiptDescription(null), "");
});

test("SETTING_TOGGLES names the on/off settings in Go", () => {
  const src = fs.readFileSync(path.join(__dirname, "..", "daemon", "internal", "settings", "settings.go"), "utf8");
  const body = src.match(/type Settings struct \{([\s\S]*?)\}/)[1];
  const boolTags = [...body.matchAll(/bool\s+`json:"(\w+)"`/g)].map((m) => m[1]).sort();
  assert.deepEqual(Object.keys(Daemon.SETTING_TOGGLES).sort(), boolTags);
});

test("a failed Media download shows Retry download", () => {
  const msg = (state) => ({ media: { state } });
  assert.equal(Media.mediaFailed(msg(Media.MEDIA_STATE.FAILED)), true);
  assert.equal(Media.downloadLabel(msg(Media.MEDIA_STATE.FAILED)), "Retry download");
  assert.equal(Media.downloadLabel(msg(Media.MEDIA_STATE.NONE)), "Download");
  assert.equal(Media.mediaFailed({}), false);
});

test("composerQuoteView previews the Message that the next send quotes", () => {
  assert.equal(Message.composerQuoteView(null), null);
  assert.deepEqual(plain(Message.composerQuoteView({ fromMe: true, senderName: "", text: "hello", revoked: false })), { sender: "You", text: "hello" });
  assert.deepEqual(plain(Message.composerQuoteView({ fromMe: false, senderName: "Ann", text: "hi" })), { sender: "Ann", text: "hi" });
});

test("openMediaStep: stop the playing note, launch a known path, or ask for the path", () => {
  const voice = { chat: "c", id: "m1", media: { kind: "voice" } };
  const key = Media.mediaKey("c", "m1");
  assert.equal(Media.openMediaStep({}, key, voice).action, "stop");
  assert.deepEqual(plain(Media.openMediaStep({ mediaPaths: { [key]: "/run/x.ogg" } }, "", voice)), { action: "launch", path: "/run/x.ogg" });
  assert.equal(Media.openMediaStep({}, "", voice).action, "request");
  assert.equal(Media.openMediaStep({ mediaPaths: { [key]: "" } }, "", voice).action, "request"); // asked, no path yet
});

test("mediaFrameStep launches Media the user clicked once its path arrives, and asks for new image paths once", () => {
  const doc = { chat: "c", id: "d1", media: { kind: "document", state: "ready" } };
  const pending = Media.rememberOpen({}, doc);
  assert.deepEqual(Object.keys(pending), [Media.mediaKey("c", "d1")]);
  const other = Media.mediaFrameStep({}, pending, { result: { chat: "c", id: "zz", path: "/p" } });
  assert.equal(other.launch, null);
  assert.deepEqual(Object.keys(other.pending), [Media.mediaKey("c", "d1")]);
  const hit = Media.mediaFrameStep({}, pending, { result: { chat: "c", id: "d1", path: "/run/d.pdf" } });
  assert.deepEqual(plain(hit.launch), { message: doc, path: "/run/d.pdf" });
  assert.deepEqual(plain(hit.pending), {});

  const image = { chat: "c", id: "i1", media: { kind: "image", state: "ready" } };
  const view = { openChat: { jid: "c", messages: [image] } };
  const first = Media.mediaFrameStep(view, {}, { method: "connected" });
  assert.deepEqual(plain(first.load), [image]);
  const second = Media.mediaFrameStep(first.state, {}, { method: "connected" });
  assert.deepEqual(plain(second.load), []);
});

test("the wheel and the touchpad scroll by angleDelta at a fixed step per notch, inside the list", () => {
  // contentY, angleDelta.y, top, bottom
  assert.equal(Timeline.WHEEL_STEP, 90); // the Bitwarden plugin's rate
  assert.equal(Timeline.wheelScrollTarget(500, 120, 0, 1000), 500 - Timeline.WHEEL_STEP);
  assert.equal(Timeline.wheelScrollTarget(500, -240, 0, 1000), 500 + 2 * Timeline.WHEEL_STEP);
  // A touchpad event measured on Hyprland: angleDelta -72 (pixelDelta -6). Scrolling by pixelDelta was 9x too slow.
  assert.equal(Timeline.wheelScrollTarget(500, -72, 0, 1000), 500 + 72 / 120 * Timeline.WHEEL_STEP);
  assert.equal(Timeline.wheelScrollTarget(10, 120, 0, 1000), 0);
  assert.equal(Timeline.wheelScrollTarget(990, -120, 0, 1000), 1000);
  assert.equal(Timeline.wheelScrollTarget(0, -120, 0, -50), 0); // content shorter than the view: stay at the top
});

test("a chat.picture request names the Chat", () => {
  assert.deepEqual(JSON.parse(ChatList.chatPictureRequest(3, "1@s.whatsapp.net")), { jsonrpc: "2.0", id: 3, method: "chat.picture", params: { chat: "1@s.whatsapp.net" } });
});

test("a chat.picture result is folded into the pictures, and pictureChanged removes it", () => {
  const jid = "1@s.whatsapp.net";
  let state = Timeline.openChat(Daemon.initialState(), { jid, name: "A", kind: "direct" });
  assert.equal(ChatList.pictureToRequest(state), jid);
  assert.equal(ChatList.chatPictureSource(state, jid), "");
  state = Daemon.applyFrame(state, { jsonrpc: "2.0", id: 1, result: { chat: jid, data: "QUJD" } });
  assert.equal(ChatList.chatPictureSource(state, jid), "data:image/jpeg;base64,QUJD");
  assert.equal(ChatList.pictureToRequest(state), "");
  state = Daemon.applyFrame(state, { jsonrpc: "2.0", method: "pictureChanged", params: { chat: jid } });
  assert.equal(state.pictures[jid], undefined);
  assert.equal(ChatList.pictureToRequest(state), jid);
});

test("a Chat with no picture is not asked again, and shows no source", () => {
  const jid = "2@s.whatsapp.net";
  let state = Timeline.openChat(Daemon.initialState(), { jid, name: "B", kind: "group" });
  state = Daemon.applyFrame(state, { jsonrpc: "2.0", id: 1, result: { chat: jid, data: "" } });
  assert.equal(ChatList.chatPictureSource(state, jid), "");
  assert.equal(ChatList.pictureToRequest(state), "");
});

test("pictureChanged of another Chat asks for nothing", () => {
  const jid = "1@s.whatsapp.net";
  let state = Timeline.openChat(Daemon.initialState(), { jid, name: "A", kind: "direct" });
  state = Daemon.applyFrame(state, { jsonrpc: "2.0", id: 1, result: { chat: jid, data: "QUJD" } });
  state = Daemon.applyFrame(state, { jsonrpc: "2.0", method: "pictureChanged", params: { chat: "9@s.whatsapp.net" } });
  assert.equal(ChatList.pictureToRequest(state), "");
});

// On-Demand History Sync: the link at the end of the stored history.
const olderChat = (changes = {}) => {
  const open = Timeline.openChat(Daemon.initialState(), { jid: "1@s.whatsapp.net", name: "Ann", kind: "direct" });
  const page = Daemon.applyFrame(open, { result: { chat: "1@s.whatsapp.net", messages: [{ id: "m1", chat: "1@s.whatsapp.net", time: 100 }], more: false } });
  return { ...page, openChat: { ...page.openChat, ...changes } };
};
const received = (state, params) => Daemon.applyFrame(state, { jsonrpc: "2.0", method: "historyReceived", params: { chat: "1@s.whatsapp.net", ...params } });

test("the older link asks the Daemon for Messages when no stored Message is left", () => {
  assert.equal(Timeline.olderAction(olderChat({ moreMessages: true })), "messages");
  assert.equal(Timeline.olderAction(olderChat()), "history");
  assert.equal(Timeline.olderLink(olderChat()), "load older");
});

test("the older link waits while the phone answers, and ignores taps", () => {
  const state = Timeline.historyRequested(olderChat());
  assert.equal(Timeline.olderLink(state), "loading from phone…");
  assert.equal(Timeline.olderAction(state), "");
});

test("Messages from the phone reload the page after the loaded Messages", () => {
  const state = received(Timeline.historyRequested(olderChat()), { count: 3 });
  assert.equal(Timeline.olderLink(state), "load older");
  assert.equal(Timeline.reloadsHistory(state, { method: "historyReceived", params: { chat: "1@s.whatsapp.net", count: 3 } }), true);
});

test("a phone with nothing more hides the link for this Chat", () => {
  const frame = { method: "historyReceived", params: { chat: "1@s.whatsapp.net", count: 0 } };
  const state = received(Timeline.historyRequested(olderChat()), { count: 0 });
  assert.equal(Timeline.olderLink(state), "");
  assert.equal(Timeline.olderAction(state), "");
  assert.equal(Timeline.reloadsHistory(state, frame), false);
});

test("a phone that does not answer gives a notice and the link comes back", () => {
  const state = received(Timeline.historyRequested(olderChat()), { count: 0, timedOut: true });
  assert.equal(state.notice, "No older Messages came from your phone.");
  assert.equal(Timeline.olderLink(state), "load older");
});

test("no connection ends the wait with the offline notice", () => {
  const state = Daemon.applyFrame(Timeline.historyRequested(olderChat()), { jsonrpc: "2.0", id: 4, error: { code: -32007, message: "could not reach WhatsApp" } });
  assert.equal(state.notice, "Could not reach WhatsApp");
  assert.equal(Timeline.olderLink(state), "load older");
});

test("a historyReceived for a Chat that is not open changes nothing", () => {
  const state = Timeline.historyRequested(olderChat());
  assert.deepEqual(received(state, { chat: "2@s.whatsapp.net", count: 0 }), state);
});

test("the older link stays hidden until the first page of the Chat is in", () => {
  const open = Timeline.openChat(Daemon.initialState(), { jid: "1@s.whatsapp.net", name: "Ann", kind: "direct" });
  assert.equal(Timeline.olderLink(open), "");
});

test("the Daemon request for older Messages is chat.requestHistory", () => {
  assert.deepEqual(JSON.parse(Timeline.historyRequest(5, "1@s.whatsapp.net")), { jsonrpc: "2.0", id: 5, method: "chat.requestHistory", params: { chat: "1@s.whatsapp.net" } });
});

test("chatCleared takes the Messages up to `until` out of the open Chat, or all with 0", () => {
  const at = (time) => ({ id: "m" + time, chat: "1@s.whatsapp.net", time });
  let state = Timeline.openChat(Daemon.initialState(), { jid: "1@s.whatsapp.net", name: "A", kind: "direct" });
  for (const time of [10, 20, 30]) state = Timeline.upsertMessage(state, at(time));
  const ids = (s) => Array.from(s.openChat.messages, (m) => m.id);
  const frame = (chat, until) => ({ method: "chatCleared", params: { chat, until } });
  assert.deepEqual(ids(Daemon.applyFrame(state, frame("1@s.whatsapp.net", 20))), ["m30"]);
  assert.deepEqual(ids(Daemon.applyFrame(state, frame("1@s.whatsapp.net", 0))), []);
  assert.deepEqual(ids(Daemon.applyFrame(state, frame("2@s.whatsapp.net", 0))), ["m10", "m20", "m30"]);
  assert.equal(Daemon.refreshesChats(frame("1@s.whatsapp.net", 20)), true);
});

test("chatRemoved takes the Chat out of the list and closes it when it is open", () => {
  const frame = { method: "chatRemoved", params: { chat: "1@s.whatsapp.net" } };
  const list = [{ jid: "1@s.whatsapp.net" }, { jid: "2@s.whatsapp.net" }];
  const listed = Object.assign(Daemon.initialState(), { chats: list });
  const open = (jid) => Timeline.openChat(listed, { jid, name: "A", kind: "direct" });
  const shut = Daemon.applyFrame(open("1@s.whatsapp.net"), frame);
  assert.equal(Daemon.removesOpenChat(open("1@s.whatsapp.net"), frame), true);
  assert.equal(shut.openChat, undefined);
  assert.deepEqual(Array.from(shut.chats, (c) => c.jid), ["2@s.whatsapp.net"]);
  const other = open("2@s.whatsapp.net");
  assert.equal(Daemon.removesOpenChat(other, frame), false);
  assert.equal(Daemon.applyFrame(other, frame).openChat.jid, "2@s.whatsapp.net");
  assert.equal(Daemon.refreshesChats(frame), true);
});

test("the Settings notice shows for keyringPlain true only", () => {
  const ready = (params) => Daemon.applyFrame(Daemon.initialState(), { method: "ready", params });
  assert.match(Daemon.keyringNotice(ready({ keyringPlain: true })), /no password/);
  assert.equal(Daemon.keyringNotice(ready({ keyringPlain: false })), "");
  assert.equal(Daemon.keyringNotice(ready({})), "");
  assert.equal(Daemon.keyringNotice(ready(undefined)), "");
});

test("the keyring notice stays through later frames and ends with a new ready", () => {
  let state = Daemon.applyFrame(Daemon.initialState(), { method: "ready", params: { keyringPlain: true } });
  state = Daemon.applyFrame(state, { method: "linked" });
  assert.notEqual(Daemon.keyringNotice(state), "");
  state = Daemon.applyFrame(state, { method: "ready", params: { keyringPlain: false } });
  assert.equal(Daemon.keyringNotice(state), "");
});

const linked = Daemon.applyFrame(Daemon.initialState(), { method: "linked" });
test("the Offline banner shows why WhatsApp ended the session, and a connect clears it", () => {
  let state = Daemon.applyFrame(Daemon.applyFrame(linked, { method: "connected" }), { method: "disconnected", params: { reason: "stream-replaced" } })
  assert.match(Daemon.connectionBanner(state), /^Offline\n.*another device/)
  assert.equal(Daemon.connectionBanner(Daemon.applyFrame(linked, { method: "disconnected" })), "Offline")
  assert.equal(Daemon.connectionBanner(Daemon.applyFrame(state, { method: "connected" })), "")
})
