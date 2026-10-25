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
      if (line.dir === "plugin" && line.lookup) state = State.lookupStarted(state, line.lookup);
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

test("a merged Chat reloads the open Chat only when it is one of the two", () => {
  const frame = { method: "chatMerged", params: { from: "1@lid", to: "2@s.whatsapp.net" } };
  const open = (jid) => State.openChat(State.initialState(), { jid, name: "A", kind: "direct" });
  for (const jid of ["1@lid", "2@s.whatsapp.net"]) {
    const after = State.applyFrame(open(jid), frame);
    assert.equal(after.openChat.jid, "2@s.whatsapp.net");
    assert.equal(State.reloadsOpenChat(after, frame), true);
  }
  const other = State.applyFrame(open("3@s.whatsapp.net"), frame);
  assert.equal(other.openChat.jid, "3@s.whatsapp.net");
  assert.equal(State.reloadsOpenChat(other, frame), false);
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

test("the Chat list request carries archived only when it is true", () => {
  assert.deepEqual(JSON.parse(State.chatListRequest(8, 0, "", true)).params, { offset: 0, archived: true });
  assert.deepEqual(JSON.parse(State.chatListRequest(9, 0, "", false)).params, { offset: 0 });
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
  assert.equal(State.composerAction(State.KEY_RETURN, false, "", true), "send");
  assert.equal(State.composerAction(State.KEY_RETURN, false, " ", false), "swallow");
});

test("the send button works when Enter would send", () => {
  assert.equal(State.canSend("hi", false), true);
  assert.equal(State.canSend(" \n ", false), false);
  assert.equal(State.canSend("", true), true);
});

test("a line shows the sender name of others and You for the user", () => {
  assert.equal(State.senderLabel(theirs), "Bob");
  assert.equal(State.senderLabel(mine), "You");
  assert.equal(State.senderLabel({ fromMe: false, senderName: "Ann" }), "Ann");
});

test("Failed has Retry, other states do not", () => {
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

test("a mention is bold, other text is escaped", () => {
  const url = "https://www.openstreetmap.org/?mlat=51.500000&mlon=-0.120000#map=17/51.500000/-0.120000";
  assert.equal(State.messageHtml(sentMessage({ text: "@Zed <b>look</b>", mentions: ["@Zed"] })), "<b>@Zed</b> &lt;b&gt;look&lt;/b&gt;");
  assert.equal(State.messageHtml(sentMessage({ text: "Location: Cafe\n" + url })), `Location: Cafe<br>${url.replace(/&/g, "&amp;")}`);
  assert.equal(State.messageHtml(sentMessage({ text: "", revoked: true, mentions: ["@Zed"] })), "This message was deleted");
});

// The map link opens from a button: a link inside the Message text did not take clicks in the pop-out.
test("a location has a map link for its Open map button", () => {
  const url = "https://www.openstreetmap.org/?mlat=51.500000&mlon=-0.120000#map=17/51.500000/-0.120000";
  assert.equal(State.mapLink(sentMessage({ text: "Location: Cafe\n" + url })), url);
  assert.equal(State.mapLink(sentMessage({ text: "see https://evil.example/" })), "");
  assert.equal(State.mapLink(sentMessage({ text: "Location: Cafe\n" + url, revoked: true })), "");
});

test("only a map link opens", () => {
  const url = "https://www.openstreetmap.org/?mlat=51.5&mlon=-0.12#map=17/51.5/-0.12";
  assert.equal(JSON.stringify(State.linkCommand(url)), JSON.stringify(["xdg-open", url])); // xdg-open rejects "--"
  assert.equal(State.linkCommand("https://evil.example/").length, 0);
  assert.equal(State.linkCommand(url + " ; rm").length, 0);
});

test("a Tombstone shows a fixed text, an edited Message shows (edited)", () => {
  assert.equal(State.messageBody(sentMessage({ revoked: true, text: "" })), "This message was deleted");
  assert.equal(State.messageBody(sentMessage()), "hi");
  assert.equal(State.editedLabel(sentMessage({ edited: true })), "(edited)");
  assert.equal(State.editedLabel(sentMessage()), "");
  assert.equal(State.editedLabel(sentMessage({ edited: true, revoked: true })), "");
});

test("Edit and Delete show for sent Messages of the user; Edit ends after 15 minutes", () => {
  assert.equal(State.canEditMessage(sentMessage(), 1000 + 900), true);
  assert.equal(State.canEditMessage(sentMessage(), 1000 + 901), false);
  assert.equal(State.canRevokeMessage(sentMessage(), 1000 + 99999), true);
  for (const state of ["pending", "failed", ""]) assert.equal(State.canRevokeMessage(sentMessage({ state })), false, state);
  assert.equal(State.canRevokeMessage(sentMessage({ fromMe: false })), false);
  assert.equal(State.canRevokeMessage(sentMessage({ revoked: true })), false);
});

// The constants below have a Go twin. Read the Go source so that a change on one side fails here.
test("EDIT_WINDOW_SECONDS agrees with EditWindow in Go", () => {
  const go = fs.readFileSync(path.join(root, "daemon", "internal", "contract", "edit.go"), "utf8");
  const minutes = Number(/EditWindow = (\d+) \* time\.Minute/.exec(go)[1]);
  assert.equal(State.EDIT_WINDOW_SECONDS, minutes * 60);
});

const goStrings = (file, name) => {
  const go = fs.readFileSync(path.join(root, "daemon", "internal", ...file), "utf8");
  return Array.from(go.matchAll(new RegExp(`\\b${name}\\w* += "(\\w+)"`, "g")), (m) => m[1]).sort();
};

test("MEDIA_STATE agrees with the Media states in Go", () => {
  assert.deepEqual(Object.values(State.MEDIA_STATE).sort(), goStrings(["chatstore", "messages.go"], "Media"));
});

test("READ_RECEIPT_MODES agrees with the Read Receipt modes in Go", () => {
  assert.deepEqual(Array.from(State.READ_RECEIPT_MODES, (m) => m.value).sort(), goStrings(["settings", "settings.go"], "ReadReceipts"));
});

test("every notification with an effect is documented in docs/contract.md", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  for (const method of Object.keys(State.NOTIFICATIONS)) assert.ok(doc.includes("`" + method + "`"), method);
});

test("REACTION_EMOJI agrees with ReactionEmoji in Go", () => {
  const go = fs.readFileSync(path.join(root, "daemon", "internal", "contract", "react.go"), "utf8");
  const list = /ReactionEmoji = \[\]string\{([^}]*)\}/.exec(go)[1];
  assert.deepEqual(Array.from(State.REACTION_EMOJI), list.split(",").map((e) => JSON.parse(e.trim())));
});

test("the composer sends an edit when a Message is under edit, otherwise a new Message", () => {
  assert.deepEqual(JSON.parse(State.composerRequest(4, direct.jid, "m1", "fixed")), { jsonrpc: "2.0", id: 4, method: "chat.edit", params: { chat: direct.jid, id: "m1", text: "fixed" } });
  assert.deepEqual(JSON.parse(State.composerRequest(5, direct.jid, "", "hi")), { jsonrpc: "2.0", id: 5, method: "chat.send", params: { chat: direct.jid, text: "hi" } });
  assert.deepEqual(JSON.parse(State.revokeRequest(6, direct.jid, "m1")), { jsonrpc: "2.0", id: 6, method: "chat.revoke", params: { chat: direct.jid, id: "m1" } });
  assert.deepEqual(JSON.parse(State.deleteForMeRequest(7, direct.jid, "m1")), { jsonrpc: "2.0", id: 7, method: "chat.deleteForMe", params: { chat: direct.jid, id: "m1" } });
});

test("filePath turns a file URL into a path, and sendMediaRequest sends it", () => {
  assert.equal(State.filePath("file:///home/me/My%20Doc%C3%A9.pdf"), "/home/me/My Docé.pdf");
  assert.deepEqual(JSON.parse(State.sendMediaRequest(7, direct.jid, "/home/me/100%.pdf", "hi")), { jsonrpc: "2.0", id: 7, method: "chat.sendMedia", params: { chat: direct.jid, path: "/home/me/100%.pdf", caption: "hi" } });
});

test("Reactions: the user picks, replaces or removes one; chips count the emoji", () => {
  const msg = { id: "m1", revoked: false, fromMe: false, reactions: [{ emoji: "👍", fromMe: false }, { emoji: "🙏", fromMe: true }, { emoji: "👍", fromMe: false }] };
  assert.equal(State.myReaction(msg), "🙏");
  assert.equal(State.reactionChoice(msg, "🙏"), "");
  assert.equal(State.reactionChoice(msg, "😂"), "😂");
  assert.equal(State.reactionChoice({ reactions: [] }, "😂"), "😂");
  assert.deepEqual(JSON.parse(JSON.stringify(State.reactionChips(msg))), [{ emoji: "👍", count: 2, mine: false, names: ", " }, { emoji: "🙏", count: 1, mine: true, names: "You" }]);
  assert.deepEqual(JSON.parse(JSON.stringify(State.reactionChips({ id: "m2" }))), []);
  assert.deepEqual(JSON.parse(JSON.stringify(State.reactionChips({ ...msg, revoked: true }))), []);
  assert.deepEqual(Array.from(State.REACTION_EMOJI), ["👍", "❤️", "😂", "😮", "😢", "🙏"]);
  assert.deepEqual(JSON.parse(State.reactRequest(3, "c@s.whatsapp.net", "m1", "")), { jsonrpc: "2.0", id: 3, method: "chat.react", params: { chat: "c@s.whatsapp.net", id: "m1", emoji: "" } });
  assert.equal(State.canReactToMessage({ fromMe: true, state: "pending" }), false);
  assert.equal(State.canReactToMessage({ fromMe: true, state: "sent" }), true);
  assert.equal(State.canReactToMessage({ fromMe: false, state: "" }), true);
  assert.equal(State.canReactToMessage({ fromMe: false, revoked: true }), false);
});

test("Quotes: the preview names the author and is cut to one line; the composer sends the Quote", () => {
  assert.equal(State.quoteView({ text: "no quote" }), null);
  assert.deepEqual(JSON.parse(JSON.stringify(State.quoteView({ quote: { fromMe: true, senderName: "", text: "a\nb" } }))), { sender: "You", text: "a b" });
  const long = State.quoteView({ quote: { fromMe: false, senderName: "Ann", text: "x".repeat(200) } });
  assert.equal(long.sender, "Ann");
  assert.equal(long.text.length, 80);
  assert.ok(long.text.endsWith("…"));
  assert.equal(State.canQuoteMessage({ revoked: true }), false);
  assert.equal(State.canQuoteMessage({ revoked: false }), true);
  const jid = "c@s.whatsapp.net";
  assert.deepEqual(JSON.parse(State.composerRequest(1, jid, "", "re", "m1")).params, { chat: jid, text: "re", quote: "m1" });
  assert.deepEqual(JSON.parse(State.composerRequest(2, jid, "", "re", "")).params, { chat: jid, text: "re" });
  assert.equal(JSON.parse(State.composerRequest(3, jid, "m1", "fix", "m2")).method, "chat.edit");
});

test("presenceLabel shows Online and Last Seen in a Direct Chat only", () => {
  const now = Math.floor(new Date(2026, 9, 24, 15, 0).getTime() / 1000);
  const seen = Math.floor(new Date(2026, 9, 24, 14, 5).getTime() / 1000);
  const old = Math.floor(new Date(2026, 9, 20, 14, 5).getTime() / 1000);
  const open = (kind) => State.openChat(State.initialState(), { jid: "1@s.whatsapp.net", name: "Ann", kind });
  const seen_ = (state, p) => State.applyFrame(state, { method: "presenceChanged", params: { chat: "1@s.whatsapp.net", ...p } });
  assert.equal(State.presenceLabel(open("direct"), now), "");
  assert.equal(State.presenceLabel(seen_(open("direct"), { online: true, lastSeen: 0 }), now), "Online");
  assert.equal(State.presenceLabel(seen_(open("direct"), { online: false, lastSeen: seen }), now), "last seen at 14:05");
  assert.equal(State.presenceLabel(seen_(open("direct"), { online: false, lastSeen: old }), now), "last seen 2026-10-20");
  assert.equal(State.presenceLabel(seen_(open("direct"), { online: false, lastSeen: 0 }), now), "");
  assert.equal(State.presenceLabel(seen_(open("group"), { online: true, lastSeen: 0 }), now), "");
  const hidden = (p) => ({ ...seen_(open("direct"), p), settings: { sendOnline: false } });
  assert.equal(State.presenceLabel(hidden({ online: true, lastSeen: 0 }), now), "");
  assert.equal(State.presenceLabel(hidden({ online: false, lastSeen: seen }), now), "");
});

test("ownOnline is true only when connected with send online on", () => {
  assert.equal(State.ownOnline({ connection: "online", settings: { sendOnline: true } }), true);
  assert.equal(State.ownOnline({ connection: "online", settings: { sendOnline: false } }), false);
  assert.equal(State.ownOnline({ connection: "offline", settings: { sendOnline: true } }), false);
  assert.equal(State.ownOnline({ connection: "online" }), false);
});

test("activityLabel names the Participant in a Group Chat and drops it on a connection loss", () => {
  const act = (state, chat, participant, name, s) =>
    State.applyFrame(state, { method: "activityChanged", params: { chat, participant, name, state: s } });
  let direct = State.openChat(State.initialState(), { jid: "1@s.whatsapp.net", name: "Ann", kind: "direct" });
  direct = act(direct, "1@s.whatsapp.net", "1@s.whatsapp.net", "Ann", "composing");
  assert.equal(State.activityLabel(direct), "typing…");
  direct = act(direct, "1@s.whatsapp.net", "1@s.whatsapp.net", "Ann", "recording");
  assert.equal(State.activityLabel(direct), "recording…");
  assert.equal(State.activityLabel(State.applyFrame(direct, { method: "disconnected" })), "");
  let group = State.openChat(State.initialState(), { jid: "g@g.us", name: "Team", kind: "group" });
  group = act(act(group, "g@g.us", "2@s", "Bob", "composing"), "g@g.us", "3@s", "Cy", "recording");
  assert.equal(State.activityLabel(group), "Bob is typing…, Cy is recording…");
});

test("typingAction sends Composing on the first text and Paused when the text empties", () => {
  assert.equal(State.typingAction(false, "h"), "start");
  assert.equal(State.typingAction(true, "hi"), "");
  assert.equal(State.typingAction(true, "  "), "stop");
  assert.equal(State.typingAction(false, ""), "");
  assert.deepEqual(JSON.parse(State.typingRequest(3, "1@s", true)), { jsonrpc: "2.0", id: 3, method: "chat.typing", params: { chat: "1@s", typing: true } });
});

test("chatStatusLabel prefers the activity over Presence", () => {
  let s = State.openChat(State.initialState(), { jid: "1@s", name: "Ann", kind: "direct" });
  s = State.applyFrame(s, { method: "presenceChanged", params: { chat: "1@s", online: true, lastSeen: 0 } });
  assert.equal(State.chatStatusLabel(s, 0), "Online");
  s = State.applyFrame(s, { method: "activityChanged", params: { chat: "1@s", participant: "1@s", name: "Ann", state: "composing" } });
  assert.equal(State.chatStatusLabel(s, 0), "typing…");
});

test("fixed shortcuts: Escape goes back one level, Ctrl+F searches the Chat list", () => {
  const list = { screen: "linked" };
  const none = { showSettings: false };
  assert.equal(State.shortcutAction("Escape", { ...list, openChat: { jid: "1" } }, none), "closeChat");
  assert.equal(State.shortcutAction("Escape", list, { showSettings: true }), "closeSettings");
  assert.equal(State.shortcutAction("Escape", list, none), "closePopout");
  assert.equal(State.shortcutAction("Ctrl+F", list, none), "focusSearch");
  assert.equal(State.shortcutAction("Ctrl+F", { ...list, openChat: { jid: "1" } }, none), "none");
  assert.equal(State.shortcutAction("Ctrl+F", list, { showSettings: true }), "none");
  assert.equal(State.shortcutAction("Ctrl+F", { screen: "linking" }, none), "none");
  assert.equal(State.shortcutAction("F5", list, none), "none");
});

const plain = (value) => JSON.parse(JSON.stringify(value));
const noLocal = { pairMode: false, showSettings: false };

test("panelParts: each part shows on its screen only", () => {
  const linked = { screen: State.SCREEN.LINKED, connection: "offline", settings: { sendOnline: true }, moreChats: true };
  assert.deepEqual(plain(State.panelParts(linked, noLocal)), {
    error: false, banner: true, qr: false, pairForm: false, pairToggle: false,
    chatList: true, loadMoreChats: true, newChat: false, newChatButton: true, emptyChat: false, settingsButton: true, settings: false, chat: false,
  });
  assert.equal(State.panelParts(linked, { ...noLocal, showSettings: true }).settings, true);
  assert.equal(State.panelParts(linked, { ...noLocal, showSettings: true }).chatList, false);
  assert.equal(State.panelParts(linked, { ...noLocal, showSettings: true }).loadMoreChats, false);
  const inChat = { ...linked, openChat: { jid: "1@s.whatsapp.net" } };
  const chat = State.panelParts(inChat, noLocal);
  assert.deepEqual([chat.chat, chat.chatList, chat.settingsButton, chat.loadMoreChats], [true, false, false, false]);
  assert.equal(State.panelParts({ ...linked, settings: undefined }, { ...noLocal, showSettings: true }).settings, false);
});

test("panelParts: linking shows the QR code or the Pair Code form, and an error shows as error", () => {
  const linking = { screen: State.SCREEN.LINKING, qr: "data:image/png;base64,AA" };
  assert.equal(State.panelParts(linking, noLocal).qr, true);
  assert.equal(State.panelParts(linking, { ...noLocal, pairMode: true }).qr, false);
  assert.equal(State.panelParts(linking, { ...noLocal, pairMode: true }).pairForm, true);
  assert.equal(State.panelParts(linking, noLocal).pairToggle, true);
  assert.equal(State.panelParts({ screen: State.SCREEN.ERROR }, noLocal).error, true);
  assert.equal(State.panelParts({ screen: State.SCREEN.STARTING }, noLocal).pairToggle, false);
});

test("readReceiptDescription follows the Read Receipts mode that is set", () => {
  const mode = State.READ_RECEIPT_MODES[1];
  assert.equal(State.readReceiptDescription({ readReceipts: mode.value }), mode.description);
  assert.equal(State.readReceiptDescription({ readReceipts: "other" }), "");
  assert.equal(State.readReceiptDescription(null), "");
});

test("SETTING_TOGGLES names the on/off settings in Go", () => {
  const src = fs.readFileSync(path.join(__dirname, "..", "daemon", "internal", "settings", "settings.go"), "utf8");
  const body = src.match(/type Settings struct \{([\s\S]*?)\}/)[1];
  const boolTags = [...body.matchAll(/bool\s+`json:"(\w+)"`/g)].map((m) => m[1]).sort();
  assert.deepEqual(Object.keys(State.SETTING_TOGGLES).sort(), boolTags);
});

test("a failed Media download shows Retry download", () => {
  const msg = (state) => ({ media: { state } });
  assert.equal(State.mediaFailed(msg(State.MEDIA_STATE.FAILED)), true);
  assert.equal(State.downloadLabel(msg(State.MEDIA_STATE.FAILED)), "Retry download");
  assert.equal(State.downloadLabel(msg(State.MEDIA_STATE.NONE)), "Download");
  assert.equal(State.mediaFailed({}), false);
});

test("composerQuoteView previews the Message that the next send quotes", () => {
  assert.equal(State.composerQuoteView(null), null);
  assert.deepEqual(plain(State.composerQuoteView({ fromMe: true, senderName: "", text: "hello", revoked: false })), { sender: "You", text: "hello" });
  assert.deepEqual(plain(State.composerQuoteView({ fromMe: false, senderName: "Ann", text: "hi" })), { sender: "Ann", text: "hi" });
});

test("openMediaStep: stop the playing note, launch a known path, or ask for the path", () => {
  const voice = { chat: "c", id: "m1", media: { kind: "voice" } };
  const key = State.mediaKey("c", "m1");
  assert.equal(State.openMediaStep({}, key, voice).action, "stop");
  assert.deepEqual(plain(State.openMediaStep({ mediaPaths: { [key]: "/run/x.ogg" } }, "", voice)), { action: "launch", path: "/run/x.ogg" });
  assert.equal(State.openMediaStep({}, "", voice).action, "request");
  assert.equal(State.openMediaStep({ mediaPaths: { [key]: "" } }, "", voice).action, "request"); // asked, no path yet
});

test("mediaFrameStep launches Media the user clicked once its path arrives, and asks for new image paths once", () => {
  const doc = { chat: "c", id: "d1", media: { kind: "document", state: "ready" } };
  const pending = State.rememberOpen({}, doc);
  assert.deepEqual(Object.keys(pending), [State.mediaKey("c", "d1")]);
  const other = State.mediaFrameStep({}, pending, { result: { chat: "c", id: "zz", path: "/p" } });
  assert.equal(other.launch, null);
  assert.deepEqual(Object.keys(other.pending), [State.mediaKey("c", "d1")]);
  const hit = State.mediaFrameStep({}, pending, { result: { chat: "c", id: "d1", path: "/run/d.pdf" } });
  assert.deepEqual(plain(hit.launch), { message: doc, path: "/run/d.pdf" });
  assert.deepEqual(plain(hit.pending), {});

  const image = { chat: "c", id: "i1", media: { kind: "image", state: "ready" } };
  const view = { openChat: { jid: "c", messages: [image] } };
  const first = State.mediaFrameStep(view, {}, { method: "connected" });
  assert.deepEqual(plain(first.load), [image]);
  const second = State.mediaFrameStep(first.state, {}, { method: "connected" });
  assert.deepEqual(plain(second.load), []);
});

test("the wheel scrolls a fixed step per notch, a touchpad by its pixels, inside the list", () => {
  // contentY, angleDelta.y, pixelDelta.y, top, bottom
  assert.equal(State.wheelScrollTarget(500, 120, 0, 0, 1000), 500 - State.WHEEL_STEP);
  assert.equal(State.wheelScrollTarget(500, -240, 0, 0, 1000), 500 + 2 * State.WHEEL_STEP);
  assert.equal(State.wheelScrollTarget(500, 120, 7, 0, 1000), 493);
  assert.equal(State.wheelScrollTarget(10, 120, 0, 0, 1000), 0);
  assert.equal(State.wheelScrollTarget(990, -120, 0, 0, 1000), 1000);
  assert.equal(State.wheelScrollTarget(0, -120, 0, 0, -50), 0); // content shorter than the view: stay at the top
});

test("a chat.picture request names the Chat", () => {
  assert.deepEqual(JSON.parse(State.chatPictureRequest(3, "1@s.whatsapp.net")), { jsonrpc: "2.0", id: 3, method: "chat.picture", params: { chat: "1@s.whatsapp.net" } });
});

test("a chat.picture result is folded into the pictures, and pictureChanged removes it", () => {
  const jid = "1@s.whatsapp.net";
  let state = State.openChat(State.initialState(), { jid, name: "A", kind: "direct" });
  assert.equal(State.pictureToRequest(state), jid);
  assert.equal(State.chatPictureSource(state, jid), "");
  state = State.applyFrame(state, { jsonrpc: "2.0", id: 1, result: { chat: jid, data: "QUJD" } });
  assert.equal(State.chatPictureSource(state, jid), "data:image/jpeg;base64,QUJD");
  assert.equal(State.pictureToRequest(state), "");
  state = State.applyFrame(state, { jsonrpc: "2.0", method: "pictureChanged", params: { chat: jid } });
  assert.equal(state.pictures[jid], undefined);
  assert.equal(State.pictureToRequest(state), jid);
});

test("a Chat with no picture is not asked again, and shows no source", () => {
  const jid = "2@s.whatsapp.net";
  let state = State.openChat(State.initialState(), { jid, name: "B", kind: "group" });
  state = State.applyFrame(state, { jsonrpc: "2.0", id: 1, result: { chat: jid, data: "" } });
  assert.equal(State.chatPictureSource(state, jid), "");
  assert.equal(State.pictureToRequest(state), "");
});

test("pictureChanged of another Chat asks for nothing", () => {
  const jid = "1@s.whatsapp.net";
  let state = State.openChat(State.initialState(), { jid, name: "A", kind: "direct" });
  state = State.applyFrame(state, { jsonrpc: "2.0", id: 1, result: { chat: jid, data: "QUJD" } });
  state = State.applyFrame(state, { jsonrpc: "2.0", method: "pictureChanged", params: { chat: "9@s.whatsapp.net" } });
  assert.equal(State.pictureToRequest(state), "");
});
