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

**Participant**:
A canonical WhatsApp user identity resolved across both phone-number addressing (JID) and privacy-preserving linked identity (LID).
_Avoid_: User, contact, peer, member, sender

**Pair Code**:
An 8-character alphanumeric code linked to a phone number used as a fallback to QR scanning when authorizing a Companion Device.
_Avoid_: OTP, verification code, PIN, link token

**Media**:
An attachment payload (image, video, audio, voice note, document, or sticker) associated with a message and stored on WhatsApp's encrypted CDN until downloaded to the Profile's local cache.
_Avoid_: File, blob, upload, asset

**Message**:
A chronological entry in a Chat timeline sent by a Participant, carrying text, Media, or a system event, and open to subsequent in-place mutations (Edit, Reaction, Revoke, Delivery State).
_Avoid_: Post, item, event, packet, bubble

**History Sync**:
Batches of historical Chats and Messages pushed by the primary phone to a newly linked or reconnecting Companion Device.
_Avoid_: Backfill, archive dump, initial load

**Draft**:
Unsent message composition state (text and any pending Quote or Edit target) preserved per Chat when navigating between Chats.
_Avoid_: Unsent buffer, scratch text, composer cache

**Profile Settings**:
The local options of one Profile that control how whisker behaves, such as privacy, Media auto-download, notifications and keybindings. They never sync to the phone or to other devices, unlike App State.
_Avoid_: Config, preferences, options

**whisker-legacy**:
The .NET WhatsApp client that whisker and catty replace. It is a reference for behaviour only. No code moves over from it.
_Avoid_: old Whisker
