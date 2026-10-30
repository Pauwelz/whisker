# 1. The per-file Media key leaves out the Chat

Date: 2026-10-30

Status: Accepted

Decided in #110, which came from #98. It records a known risk of `docs/architecture.md`, section "Encryption at rest", item 6 (Media at rest). It does not change that decision.

## Context

The Daemon encrypts each stored Media file in the CDN format of WhatsApp: AES-256-CBC with a truncated HMAC-SHA256. The key and the IV of a file come from `HMAC(root, "file\x00" + id)`, where `root` is `HKDF(storeKey, "whisker media files")` and `id` is the Message id.

The Chat is not part of the key. `MoveChat` moves files between Chats, for example when two Chats merge. With the Chat in the key, each move must encrypt the files again.

Senders choose Message ids. Two Chats can hold a Message with the same id. Their files then share a key and an IV.

## Decision

Keep the current key. Do not add the Chat.

## Consequences

What the shared key and IV show:

- CBC under one key and IV shows only whether two files start with the same 16-byte blocks. One file does not reveal the other.

Who can use it. An attacker needs all of these:

- A copy of the Profile folder.
- The Message id of a file in a Chat that the attacker is not in.
- A way to make the user download a Message with the same id in another Chat.

Who knows the id:

- The Profile holds no id in the clear. Both stores are encrypted under the store key, Media file names are keyed hashes, logs hold no Message ids, and whatsmeow logging is off.
- Members of the Chat know the id, but they already have the Media.
- The WhatsApp server knows every id and can send a Message with any id.

The risk we accept: a hostile WhatsApp server with a copy of the Profile folder can check whether a stored file starts with content that it guesses, for example a known image.

Larger threat: with the store key, an attacker reads every store and Media file directly. On a default Omarchy install the keyring has no password, so a backup of `~/.local/share` holds the store key (section "Threat boundary"). A keyring password protects more than a Chat in the key would.

What we avoid:

- A migration that encrypts every stored file again.
- An encryption pass on each `MoveChat`.
- The risk of lost Media when one of those steps fails.

Revisit this decision when whisker gets its own Media format, for example the second encryption layer that #98 proposed. A new format needs a migration anyway, and it can add the Chat to the key at no extra cost.
