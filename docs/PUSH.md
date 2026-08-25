# Push notifications

Concord delivers messages over live connections. When the app is open, or
backgrounded with "Stay connected" on, nothing here is needed — the node holds
its connections and gossip arrives instantly. When the app has been closed, or
Android has reclaimed the process, a message deposited for you sits in a
[rendezvous mailbox](RENDEZVOUS.md) until you next open Concord.

Push closes that gap. A rendezvous node that holds credentials sends a
**contentless wake** when a deposit lands: no sender, no text, nothing but "you
have mail". The app wakes, drains the mailbox and decrypts locally, exactly as
it would have on reopening.

Everything in this document is **optional**. A rendezvous without credentials
and a build without a Firebase configuration both work; the mailbox behaves the
same, minus the wake.

## What already exists, and what does not

All the code is written and shipped. What is missing is a Firebase project,
which needs the account of whoever publishes the app.

| Piece | Where | State |
|---|---|---|
| Contentless wake on deposit | [`internal/mailbox/push.go`](../internal/mailbox/push.go) | Done |
| Persistent token store | `PushStore`, same file | Done |
| Node wiring | [`cmd/rendezvous/main.go`](../cmd/rendezvous/main.go) | Done |
| Token registration from the client | `Service.RegisterPush`, [`internal/app/mailbox.go`](../internal/app/mailbox.go) | Done |
| RPC surface | `RegisterPush` in [`internal/bridge/bridge.go`](../internal/bridge/bridge.go) | Done |
| Client registration + wake handling | `registerPushToken` in [`frontend/src/App.svelte`](../frontend/src/App.svelte) | Done, gated |
| Capacitor plugin, Gradle plugin | `apps/mobile/android/app/build.gradle` | Done |
| **`google-services.json`** | `apps/mobile/android/app/` | **Missing — the only step** |
| Firebase service account for the node | `FCM_SERVICE_ACCOUNT_JSON` | Missing (same project) |

The gate is automatic. `ConcordCorePlugin.pushAvailable()` reports whether the
build can reach Firebase at all, by checking for the `google_app_id` string
resource that the google-services Gradle plugin generates from
`google-services.json` — the same resource Firebase's own initialisation reads.
The frontend asks before registering. With no configuration the answer is
false, nothing registers, and the app behaves exactly as it does today.

That check exists because getting it wrong is fatal rather than untidy:
`PushNotifications.register()` reaches `FirebaseMessaging`, which throws on a
handler thread where no JavaScript `try/catch` can reach it, and the app dies.

## Client side: switching Android on

1. **Create a Firebase project.** <https://console.firebase.google.com>. Any
   name. Google Analytics is not used and can be declined.

2. **Add an Android app to it.** The package name must be exactly:

   ```
   app.concord.mobile
   ```

   (this is `applicationId` in `apps/mobile/android/app/build.gradle`; if you
   ship under your own id, use that instead, in both places). A signing
   certificate SHA-1 is not needed — FCM messaging does not use it.

3. **Download `google-services.json`** and put it at:

   ```
   apps/mobile/android/app/google-services.json
   ```

   Nothing else moves. `app/build.gradle` already applies the google-services
   plugin when — and only when — that file is present.

4. **Rebuild the app.**

   ```sh
   make android-app
   ```

5. **Confirm.** With the app running, `ConcordCore.pushAvailable()` returns
   `{available: true}`, the app asks for the notifications permission on first
   login, and `RegisterPush` calls appear against every connected rendezvous.

`google-services.json` is gitignored, and should stay that way: it identifies
your Firebase project and belongs with your signing keys, not in a public
repository.

### iOS

iOS is not wired to a runtime probe, because an APNs entitlement offers nothing
equivalent to read. Set `window.__CONCORD_PUSH = true` at build time once the
entitlement is in place; the same `registerPushToken` path then runs and
registers an `apns` token. Everything below about the node applies unchanged.

## Node side: giving a rendezvous credentials

The node needs credentials for the platforms it will wake. It reads them from
the environment at startup and disables itself if none are present — see
`NewNotifier` in [`internal/mailbox/push.go`](../internal/mailbox/push.go).

### FCM (Android)

| Variable | Value |
|---|---|
| `FCM_SERVICE_ACCOUNT_JSON` | The **contents** of a Firebase service-account JSON key, not a path |

Get it from the same Firebase project: Project settings → Service accounts →
Generate new private key. The node uses it to mint an OAuth2 token for the FCM
HTTP v1 API; it needs `client_email`, `private_key` and `project_id`.

### APNs (iOS)

| Variable | Value |
|---|---|
| `APNS_AUTH_KEY_P8` | PEM contents of the APNs `.p8` signing key |
| `APNS_KEY_ID` | That key's Key ID |
| `APNS_TEAM_ID` | Apple developer Team ID |
| `APNS_TOPIC` | The app bundle id, e.g. `app.concord.mobile` |
| `APNS_PRODUCTION` | `1` for the production host; anything else uses sandbox |

### Token storage

| Variable | Default | Meaning |
|---|---|---|
| `CONCORD_PUSH_TOKENS` | `push-tokens.json` | Where device tokens are persisted |

This one file must survive restarts. Envelopes are in-memory and cheap to lose
— they are re-sent — but a lost token is a device that silently stops being
woken until its owner next logs in. On a container, put it on a mounted volume.

On startup a node with credentials prints:

```
Push wake bridge enabled (tokens: push-tokens.json).
```

and a node whose credentials fail to parse prints `push notifier disabled:`
with the reason, then carries on without push.

### What the node learns

Nothing it did not already hold. Tokens are keyed by the opaque 16-byte mailbox
tag, never by identity, so the token store says "this device wants waking for
this mailbox" and cannot be turned into a member list. The wake itself carries
no content — the node has never had plaintext to put in it. Wakes are collapsed
to at most one per mailbox per 30 seconds, and a token not refreshed in 60 days
is forgotten (clients re-register on every login).

## Intended follow-on: "Stay connected" once push works

Android's "Stay connected" preference runs a foreground service so the process
survives backgrounding. It exists precisely because there is no push: without
it, a closed app receives nothing until reopened. It also costs a permanent
notification in the tray and keeps a libp2p node running on the user's battery.

Once push is configured, that trade changes — a wake can start the process on
demand — and the preference should default **off** for builds where
`pushAvailable()` is true, while staying on where it is false.

This is deliberately **not** implemented. The branch cannot be exercised by
anybody today, so it would ship as untested code guarding a default that no
build reaches; and it changes a visible preference, which wants to be done
alongside the settings copy explaining why the tray notification disappeared.
It belongs in the same change as the first build that actually has
`google-services.json`.

## Troubleshooting

**`pushAvailable()` says false after adding the file.** The Gradle plugin only
runs on a clean configure. Rebuild via `make android-app`, and check the build
log does not contain `google-services.json not found`.

**Registered, but no wakes arrive.** Confirm the *node* has credentials: its
startup line is the fastest check. A client will register a token happily
against a rendezvous that can do nothing with it.

**Wakes arrive but nothing appears.** The wake is contentless by design. What
posts a notification is the app draining the mailbox and decrypting; if the
mailbox is empty by then — another device drained it first — silence is
correct.
