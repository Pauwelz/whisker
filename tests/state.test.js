const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const load = require("./load");

const State = load("WhiskerState.js");
const root = path.join(__dirname, "..");
const fixtureDir = path.join(root, "contract", "fixtures");

// Seam B: fold the Daemon frames of each golden fixture, and check the view state at each "view" line.
for (const name of fs.readdirSync(fixtureDir).filter((f) => f.endsWith(".jsonl"))) {
  test(`fixture ${name}`, () => {
    let state = State.initialState();
    const lines = fs.readFileSync(path.join(fixtureDir, name), "utf8").split("\n").filter(Boolean);
    for (const text of lines) {
      const line = JSON.parse(text);
      if (line.dir === "out") state = State.applyFrame(state, line.frame);
      if (line.dir === "plugin" && line.openChat) state = State.openChat(state, line.openChat);
      if (line.dir === "view") {
        // The JSON round trip drops the prototypes of the vm context, so deep equality works.
        for (const [key, value] of Object.entries(line.expect)) assert.deepEqual(JSON.parse(JSON.stringify(state[key] ?? null)), value, key);
      }
    }
  });
}

test("the Plugin starts on the starting screen", () => {
  assert.equal(State.initialState().screen, "starting");
});

test("a version mismatch shows an error screen", () => {
  const state = State.checkVersion("1.2.3", "1.2.4\n");
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "version-mismatch");
});

test("a matching version allows the start", () => {
  assert.equal(State.checkVersion("1.2.3", "1.2.3\n"), null);
});

test("a binary that does not run shows an error screen", () => {
  assert.equal(State.checkVersion("1.2.3", "").reason, "binary-missing");
});

test("an oversized line exit shows its own error screen", () => {
  const state = State.daemonExited(State.initialState(), 10);
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "oversized-line");
});

test("any other Daemon exit shows an error screen", () => {
  const state = State.daemonExited(State.applyFrame(State.initialState(), { method: "ready" }), 1);
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "daemon-exited");
});

test("a Profile in use exit shows its own error screen", () => {
  const state = State.daemonExited(State.initialState(), 11);
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "profile-in-use");
});

test("a clean Daemon exit shows the stopped screen", () => {
  assert.equal(State.daemonExited(State.initialState(), 0).reason, "daemon-exited");
});

test("the contract doc lists the Profile in use code", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  assert.ok(doc.includes(`| ${State.EXIT_PROFILE_IN_USE} |`));
});

test("parseLine ignores a line that is not JSON", () => {
  assert.equal(State.parseLine("log noise"), null);
  assert.equal(State.parseLine('{"method":"ready"}').method, "ready");
});

test("a matching version starts the Daemon", () => {
  assert.equal(State.versionStep("1.2.3", "1.2.3\n", false).action, "start");
});

test("a missing or wrong-version binary is downloaded once", () => {
  for (const output of ["", "1.2.4\n"]) {
    const step = State.versionStep("1.2.3", output, false);
    assert.equal(step.action, "download");
    assert.equal(step.view.screen, "starting");
  }
});

test("a binary that is still wrong after the download shows an error screen", () => {
  const step = State.versionStep("1.2.3", "1.2.4\n", true);
  assert.equal(step.action, "fail");
  assert.equal(step.view.reason, "version-mismatch");
});

test("a failed download shows the Retry error screen", () => {
  for (const code of [20, 21, 22, 23, 99]) {
    const state = State.downloadFailed(code);
    assert.equal(state.screen, "error");
    assert.equal(state.reason, "download-failed");
    assert.equal(state.canRetry, true);
    assert.ok(state.message.startsWith("Daemon download failed"));
  }
  assert.ok(State.downloadFailed(23).message.includes("checksum"));
});

test("other error screens offer no Retry", () => {
  assert.notEqual(State.checkVersion("1.2.3", "").canRetry, true);
});

test("docs/releasing.md lists the fetch script exit codes", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "releasing.md"), "utf8");
  for (const code of Object.keys(State.FETCH_EXIT_MESSAGES)) assert.ok(doc.includes(`| ${code} |`), code);
});

test("the contract doc lists the exit code the Plugin maps", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  assert.ok(doc.includes(`| ${State.EXIT_OVERSIZED_LINE} |`));
});

test("an unavailable keyring shows a Retry action", () => {
  const state = State.applyFrame(State.initialState(), { jsonrpc: "2.0", method: "keyringUnavailable", params: {} });
  assert.equal(state.screen, "keyring-unavailable");
  assert.equal(state.canRetry, true);
});

test("the Retry request is a keyring.retry line", () => {
  const line = State.keyringRetryRequest(7);
  assert.ok(line.endsWith("\n"));
  assert.deepEqual(JSON.parse(line), { jsonrpc: "2.0", id: 7, method: "keyring.retry" });
});

test("Retry asks the Daemon for a keyring problem and fetches otherwise", () => {
  assert.equal(State.retryKind({ screen: "keyring-unavailable" }), "keyring");
  assert.equal(State.retryKind(State.downloadFailed(22)), "download");
});

test("a QR refresh replaces the code and keeps the Pair Code", () => {
  let state = State.applyFrame(State.initialState(), { method: "linkQr", params: { qr: "data:a" } });
  state = State.applyFrame(state, { id: 1, result: { code: "ABCD-1234" } });
  state = State.applyFrame(state, { method: "linkQr", params: { qr: "data:b" } });
  assert.equal(state.qr, "data:b");
  assert.equal(state.pairCode, "ABCD-1234");
});

test("every link failure reason has its own message and a Retry", () => {
  for (const reason of Object.keys(State.LINK_FAILURES)) {
    const state = State.applyFrame(State.initialState(), { method: "linkFailed", params: { reason } });
    assert.equal(state.message, State.LINK_FAILURES[reason]);
    assert.equal(state.canRetry, true);
  }
  assert.equal(State.applyFrame(State.initialState(), { method: "linkFailed", params: { reason: "new" } }).message, State.LINK_FAILURES.unexpected);
});

test("the contract doc lists every link failure reason", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  for (const reason of Object.keys(State.LINK_FAILURES)) assert.ok(doc.includes(`| \`${reason}\` |`), reason);
});

test("the Pair Code request keeps only the digits of the phone number", () => {
  const line = State.pairCodeRequest(3, "+31 6 1234-5678");
  assert.ok(line.endsWith("\n"));
  assert.deepEqual(JSON.parse(line), { jsonrpc: "2.0", id: 3, method: "link.pairCode", params: { phone: "31612345678" } });
});

test("the Plugin asks for the Chat list after linked and after chatsChanged only", () => {
  assert.equal(State.refreshesChats({ method: "linked" }), true);
  assert.equal(State.refreshesChats({ method: "chatsChanged" }), true);
  assert.equal(State.refreshesChats({ method: "ready" }), false);
  assert.equal(State.refreshesChats({ id: 1, result: {} }), false);
});

test("the Chat list request carries the offset", () => {
  const line = State.chatListRequest(5, 50);
  assert.ok(line.endsWith("\n"));
  assert.deepEqual(JSON.parse(line), { jsonrpc: "2.0", id: 5, method: "chats.list", params: { offset: 50 } });
});

test("the Chat list request carries the search query only when it is set", () => {
  assert.deepEqual(JSON.parse(State.chatListRequest(6, 0, "ann")).params, { offset: 0, query: "ann" });
  assert.deepEqual(JSON.parse(State.chatListRequest(7, 0, "")).params, { offset: 0 });
});

test("the Chat list survives a chatsChanged frame and goes away on unlinked", () => {
  let state = State.applyFrame(State.initialState(), { method: "linked" });
  state = State.applyFrame(state, { id: 1, result: { chats: [{ jid: "a" }], more: false, offset: 0 } });
  assert.equal(State.applyFrame(state, { method: "chatsChanged" }).chats.length, 1);
  assert.equal(State.applyFrame(state, { method: "unlinked" }).chats, undefined);
});

test("Retry on the linking screen restarts linking", () => {
  const failed = State.applyFrame(State.initialState(), { method: "linkFailed", params: { reason: "timeout" } });
  assert.equal(State.retryKind(failed), "link");
  assert.deepEqual(JSON.parse(State.linkRetryRequest(4)), { jsonrpc: "2.0", id: 4, method: "link.retry" });
});

test("the Profile Settings survive the frames that reset the view", () => {
  const settings = { readReceipts: "off", sendOnline: true, sendTyping: true, notifications: true };
  let state = State.applyFrame(State.initialState(), { id: 1, result: settings });
  state = State.applyFrame(state, { method: "unlinked", params: {} });
  assert.deepEqual(JSON.parse(JSON.stringify(state.settings)), settings);
  assert.equal(State.applyFrame(state, { method: "ready" }).settings.readReceipts, "off");
});

test("a phone unlink and a Log out show different messages", () => {
  const phone = State.applyFrame(State.initialState(), { method: "unlinked", params: {} });
  const user = State.applyFrame(State.initialState(), { method: "unlinked", params: { by: "user" } });
  assert.equal(phone.reason, "unlinked");
  assert.equal(user.reason, "logged-out");
});

test("the settings requests are JSON-RPC lines", () => {
  assert.deepEqual(JSON.parse(State.settingsGetRequest(1)), { jsonrpc: "2.0", id: 1, method: "settings.get" });
  assert.deepEqual(JSON.parse(State.settingsSetRequest(2, { sendTyping: false })), { jsonrpc: "2.0", id: 2, method: "settings.set", params: { sendTyping: false } });
  assert.deepEqual(JSON.parse(State.logoutRequest(3)), { jsonrpc: "2.0", id: 3, method: "profile.logout" });
  assert.ok(State.logoutRequest(3).endsWith("\n"));
});

test("the Plugin reads the Profile Settings after ready only", () => {
  assert.equal(State.requestsSettings({ method: "ready" }), true);
  assert.equal(State.requestsSettings({ method: "linked" }), false);
});

test("the settings view lists the Read Receipt modes of the Daemon", () => {
  assert.deepEqual(JSON.parse(JSON.stringify(State.READ_RECEIPT_MODES.map((m) => m.value))), ["off", "onOpen", "onInteraction"]);
});

const direct = { jid: "1@s.whatsapp.net", name: "Ann", kind: "direct" };
const group = { jid: "g1@g.us", name: "Team", kind: "group" };
const theirs = { id: "a", chat: group.jid, sender: "2@s.whatsapp.net", senderName: "Bob", fromMe: false, time: 5, text: "hi", state: "" };
const mine = { id: "b", chat: group.jid, sender: "", senderName: "", fromMe: true, time: 6, text: "yo", state: "pending" };

test("the connection starts as connecting after linked and follows the Daemon", () => {
  let state = State.applyFrame(State.initialState(), { method: "linked" });
  assert.equal(state.connection, "connecting");
  assert.equal(State.connectionBanner(state), "Connecting…");
  state = State.applyFrame(state, { method: "connected" });
  assert.equal(State.connectionBanner(state), "");
  state = State.applyFrame(state, { method: "disconnected" });
  assert.equal(State.connectionBanner(state), "Offline");
});

test("the bar icon dims on a screen that is not ready or linked, and while offline", () => {
  const linked = State.applyFrame(State.initialState(), { method: "linked" });
  assert.equal(State.barDimmed(State.initialState()), true);
  assert.equal(State.barDimmed(linked), false);
  assert.equal(State.barDimmed(State.applyFrame(linked, { method: "disconnected" })), true);
  assert.equal(State.barDimmed(State.applyFrame(linked, { method: "connected" })), false);
});

test("Enter sends, Shift+Enter adds a line, an empty Enter does nothing", () => {
  assert.equal(State.composerAction(State.KEY_RETURN, false, "hi"), "send");
  assert.equal(State.composerAction(State.KEY_ENTER, false, "hi"), "send");
  assert.equal(State.composerAction(State.KEY_RETURN, true, "hi"), "default");
  assert.equal(State.composerAction(State.KEY_RETURN, false, " \n "), "swallow");
  assert.equal(State.composerAction(0x41, false, "hi"), "default");
});

test("a Group Chat shows the sender name on each Message of others", () => {
  assert.equal(State.senderLabel(group, theirs), "Bob");
  assert.equal(State.senderLabel(group, mine), "");
  assert.equal(State.senderLabel(direct, { fromMe: false, senderName: "Ann" }), "");
});

test("the Delivery State shows for the Messages of the user only, and Failed has Retry", () => {
  const labels = ["pending", "sent", "delivered", "read", "played", "failed"].map((state) => State.deliveryLabel({ fromMe: true, state }));
  assert.deepEqual(labels, ["Sending…", "Sent", "Delivered", "Read", "Played", "Failed"]);
  assert.equal(State.deliveryLabel(theirs), "");
  assert.equal(State.canRetryMessage({ fromMe: true, state: "failed" }), true);
  assert.equal(State.canRetryMessage({ fromMe: true, state: "sent" }), false);
});

test("a Message of another Chat or an unopened Chat does not change the view", () => {
  const opened = State.openChat(State.applyFrame(State.initialState(), { method: "linked" }), direct);
  const other = State.applyFrame(opened, { method: "messageChanged", params: { message: theirs } });
  assert.equal(other.openChat.messages.length, 0);
  const none = State.applyFrame(State.applyFrame(State.initialState(), { method: "linked" }), { method: "messageChanged", params: { message: theirs } });
  assert.equal(none.openChat, undefined);
});

test("Messages keep their time order and a repeat replaces the Message", () => {
  let state = State.openChat(State.applyFrame(State.initialState(), { method: "linked" }), group);
  state = State.applyFrame(state, { method: "messageChanged", params: { message: mine } });
  state = State.applyFrame(state, { method: "messageChanged", params: { message: theirs } });
  assert.deepEqual([...state.openChat.messages.map((m) => m.id)], ["a", "b"]);
  state = State.applyFrame(state, { method: "messageChanged", params: { message: Object.assign({}, mine, { state: "sent" }) } });
  assert.deepEqual([...state.openChat.messages.map((m) => m.state)], ["", "sent"]);
});

test("a page of another Chat is dropped", () => {
  const opened = State.openChat(State.applyFrame(State.initialState(), { method: "linked" }), direct);
  const state = State.applyFrame(opened, { id: 1, result: { chat: group.jid, messages: [theirs], more: false, offset: 0 } });
  assert.equal(state.openChat.messages.length, 0);
});

test("closing the Chat leaves the Chat list", () => {
  const opened = State.openChat(State.applyFrame(State.initialState(), { method: "linked" }), direct);
  assert.equal(State.closeChat(opened).openChat, undefined);
});

test("the Chat requests are JSON-RPC lines", () => {
  assert.deepEqual(JSON.parse(State.messagesRequest(1, direct.jid, 50)), { jsonrpc: "2.0", id: 1, method: "chat.messages", params: { chat: direct.jid, offset: 50 } });
  assert.deepEqual(JSON.parse(State.sendRequest(2, direct.jid, "hi\nthere")), { jsonrpc: "2.0", id: 2, method: "chat.send", params: { chat: direct.jid, text: "hi\nthere" } });
  assert.deepEqual(JSON.parse(State.retryMessageRequest(3, direct.jid, "m1")), { jsonrpc: "2.0", id: 3, method: "chat.retry", params: { chat: direct.jid, id: "m1" } });
  assert.ok(State.sendRequest(2, direct.jid, "hi").endsWith("\n"));
});

test("the contract doc lists the Chat methods and the message error code", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  for (const needle of ["`chat.messages`", "`chat.send`", "`chat.retry`", "`chat.edit`", "`chat.revoke`", "`messageChanged`", "`connected`", "`disconnected`", "| -32006 |", "| -32007 |"])
    assert.ok(doc.includes(needle), needle);
});

test("the attention request carries the pop-out state and the shown Chat", () => {
  assert.deepEqual(JSON.parse(State.attentionRequest(3, true, direct.jid)), { jsonrpc: "2.0", id: 3, method: "attention.set", params: { open: true, chat: direct.jid } });
  assert.equal(JSON.parse(State.attentionRequest(4, true)).params.chat, "");
  assert.equal(JSON.parse(State.attentionRequest(5, false, direct.jid)).params.open, false);
});

test("the Plugin sends the pop-out state again when the Daemon is ready", () => {
  assert.equal(State.resendsAttention({ method: "ready" }), true);
  assert.equal(State.resendsAttention({ method: "linked" }), false);
});

const sentMessage = (extra) => Object.assign({ id: "m1", chat: direct.jid, fromMe: true, time: 1000, text: "hi", state: "sent", edited: false, revoked: false }, extra);

test("a Tombstone shows a fixed text, an edited Message shows Edited", () => {
  assert.equal(State.messageBody(sentMessage({ revoked: true, text: "" })), "This message was deleted");
  assert.equal(State.messageBody(sentMessage()), "hi");
  assert.equal(State.editedLabel(sentMessage({ edited: true })), "Edited");
  assert.equal(State.editedLabel(sentMessage()), "");
  assert.equal(State.editedLabel(sentMessage({ edited: true, revoked: true })), "");
});

test("Edit and Delete show for sent Messages of the user; Edit ends after 20 minutes", () => {
  assert.equal(State.canEditMessage(sentMessage(), 1000 + 1200), true);
  assert.equal(State.canEditMessage(sentMessage(), 1000 + 1201), false);
  assert.equal(State.canRevokeMessage(sentMessage(), 1000 + 99999), true);
  for (const state of ["pending", "failed", ""]) assert.equal(State.canRevokeMessage(sentMessage({ state })), false, state);
  assert.equal(State.canRevokeMessage(sentMessage({ fromMe: false })), false);
  assert.equal(State.canRevokeMessage(sentMessage({ revoked: true })), false);
});

test("the composer sends an edit when a Message is under edit, otherwise a new Message", () => {
  assert.deepEqual(JSON.parse(State.composerRequest(4, direct.jid, "m1", "fixed")), { jsonrpc: "2.0", id: 4, method: "chat.edit", params: { chat: direct.jid, id: "m1", text: "fixed" } });
  assert.deepEqual(JSON.parse(State.composerRequest(5, direct.jid, "", "hi")), { jsonrpc: "2.0", id: 5, method: "chat.send", params: { chat: direct.jid, text: "hi" } });
  assert.deepEqual(JSON.parse(State.revokeRequest(6, direct.jid, "m1")), { jsonrpc: "2.0", id: 6, method: "chat.revoke", params: { chat: direct.jid, id: "m1" } });
});

test("sendMediaRequest turns a file URL into a path", () => {
  assert.deepEqual(JSON.parse(State.sendMediaRequest(7, direct.jid, "file:///home/me/My%20Doc%C3%A9.pdf", "hi")), { jsonrpc: "2.0", id: 7, method: "chat.sendMedia", params: { chat: direct.jid, path: "/home/me/My Docé.pdf", caption: "hi" } });
});
