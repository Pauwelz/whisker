# whisker

whisker is a WhatsApp client for the Omarchy shell: a Quickshell Plugin and its own Daemon. It runs only on Linux.

## Language

**whisker**:
The Omarchy product: the Plugin and its own Daemon. It runs only on Linux.
_Avoid_: Omarchy product, new Whisker

**Plugin**:
The Quickshell QML part of whisker. It starts the Daemon as a child process, shows its state and sends it commands. It holds no Profile state and owns all presentation.
_Avoid_: Client, frontend, widget, UI

**Daemon**:
The Go process that the Plugin starts as a child. It is the sole owner of the Profile: the stores, the Media, the session and linking. It talks to the Plugin over its stdio and exits on stdin EOF.
_Avoid_: Service, server, backend, background process

**Companion Device**:
A linked secondary WhatsApp client instance authorized by a user's primary phone via QR code or pair code.
_Avoid_: Account, sub-account, session, web client

**Profile**:
An isolated local directory containing the state, configuration, and credentials for a single Companion Device.
_Avoid_: Workspace, tenant, user folder

**Chat**:
A conversation thread belonging to a Companion Device, categorized as a **Direct Chat** (one-to-one with another Participant), a **Group Chat** (multi-participant room), or a **Self Chat** ("Message Yourself").
_Avoid_: Channel, room, thread, dialog, conversation

**Chat Sign**:
The icon before the name of a Chat that shows its kind: Direct Chat, Group Chat or Self Chat. It is not App State.
_Avoid_: Chat icon, avatar, kind mark

**Chat Marks**:
The signs on a Chat line that show its App State and a mention: Pinned, Muted, `@` and the unread count or `●`. They sit at the end of the line.
_Avoid_: Badges, flags, status icons

**Attended Chat**:
The Chat that the Plugin shows while its pop-out is open. A Message in the Attended Chat does not notify.
_Avoid_: Focused chat, open chat, active chat

**Chat links**:
The links pin, mark unread and mute of one Chat. A Chat line shows them on hover, the Chat header after a click on ⋯. Mute shows the mute choices in their place.
_Avoid_: Chat menu, Chat options, hover buttons

**Participant**:
A canonical WhatsApp user identity resolved across both phone-number addressing (JID) and privacy-preserving linked identity (LID).
_Avoid_: User, contact, peer, member, sender

**Phone Contact**:
A Participant for whom the contacts on the user's primary phone hold a name. A Phone Contact can exist without a Chat.
_Avoid_: Contact, address book entry, friend

**Push Name**:
The name that a Participant sets for themselves in WhatsApp. whisker shows it when your phone contacts have no name for that Participant.
_Avoid_: Nickname, display name, profile name

**Pair Code**:
An 8-character alphanumeric code linked to a phone number used as a fallback to QR scanning when authorizing a Companion Device.
_Avoid_: OTP, verification code, PIN, link token

**Media**:
An attachment payload (image, video, audio, voice note, document, or sticker) associated with a message and stored on WhatsApp's encrypted CDN until downloaded to the Profile's local cache.
_Avoid_: File, blob, upload, asset

**Message**:
A chronological entry in a Chat timeline sent by a Participant, carrying text or Media, and open to subsequent in-place mutations (Edit, Reaction, Revoke, Delivery State). A System Message is the one kind that no Participant writes.
_Avoid_: Post, item, event, packet, bubble

**System Message**:
A Message that records a change to a Chat, such as a Participant who joins, a new subject or a new Disappearing timer. No Participant writes it, and nobody can react to it, quote it, edit it or revoke it.
_Avoid_: System line, group event, stub, notice

**Quote**:
A reference on a Message to an earlier Message in the same Chat. It shows the original sender and a short preview.
_Avoid_: Reply-to, parent message, thread reply, citation

**Reaction**:
One emoji that a Participant attaches to a Message. A Participant has at most one Reaction per Message.
_Avoid_: Like, upvote, emoji response

**Emoji Shortcode**:
A `:name:` that the user types in the Draft text for one emoji, such as `:thumbsup:` for 👍.
_Avoid_: Emoji code, alias, emoticon

**Tombstone**:
The placeholder that stays in the Chat timeline when a Message is revoked for everyone. It replaces the text and the Media, and it keeps the position of the Message.
_Avoid_: Soft delete, redacted message, retracted message

**Delivery State**:
The progress of an outbound Message: `Pending`, `Sent`, `Delivered`, `Read`, `Played` (voice and audio Media), or `Failed`. In a Group Chat, a state applies only when every Recipient has reached it.
_Avoid_: Message State, ack status, tick status, receipt state

**Recipient**:
A Participant of a Group Chat at the moment the user sends a Message there. The set of Recipients of a Message never changes after the send. A Recipient is a Participant, not a device: one device that reaches a state is enough. The user's own devices are never Recipients.
_Avoid_: Receiver, addressee, target, member device

**Disappearing Message**:
A Message in a Chat with a disappearing timer. Every device deletes it when the timer expires.
_Avoid_: Ephemeral Message, self-destructing message, expiring message

**History Sync**:
Batches of historical Chats and Messages that the primary phone sends to a Companion Device. The phone pushes them to a newly linked or reconnecting device, or sends them on request (On-Demand History Sync).
_Avoid_: Backfill, archive dump, initial load

**On-Demand History Sync**:
A History Sync batch that the Daemon asks the phone for: the Messages of one Chat that are older than its oldest stored Message.
_Avoid_: Backfill, fetch older, load from phone

**Draft**:
Unsent message composition state (text, and a pending Quote, Edit target or Attachments) preserved per Chat when navigating between Chats.
_Avoid_: Unsent buffer, scratch text, composer cache

**Attachment**:
The Media that waits in a Draft until the user sends it. The user adds it with a paste. A Draft holds up to 30, in order. They never combine with a Quote or an Edit.
_Avoid_: Staged file, upload, pick

**App State**:
The state that the primary phone syncs to all linked devices, such as `Pinned`, `Muted`, `Archived` and the Read Marker of a Chat. It is not the state of whisker.
_Avoid_: Chat State, user preferences, settings sync, chat flags

**Profile Settings**:
The local options of one Profile that control how whisker behaves, such as privacy, Media auto-download, notifications and keybindings. They never sync to the phone or to other devices, unlike App State.
_Avoid_: Config, preferences, options

**Read Marker**:
The App State flag that marks a Chat read or unread on all of the user's own devices. Senders never see it.
_Avoid_: Read flag, seen state, unread flag

**Read Receipt**:
The notice to the sender of a Message that the user has read it. It moves the Delivery State of that Message to `Read` for the sender.
_Avoid_: Blue ticks, seen notice, read ack

**Presence**:
Short-lived activity of a Participant: availability (`Online`, `Last Seen`) and activity in a Chat (`Composing`, `Recording`).
_Avoid_: Presence State, Chat State, status, activity status, typing state

**whisker-legacy**:
The .NET WhatsApp client that whisker and catty replace. It is a reference for behaviour only. No code moves over from it.
_Avoid_: old Whisker
