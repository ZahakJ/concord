# Google Play listing

Everything a Play Console submission needs that can be decided from the
repository: the copy, the answers to the two questionnaires, and the assets. The
parts that need a Google account, money or a signing key are listed at the
bottom as what remains manual — they are not things this file can do for you.

Nothing here is a substitute for the Console's own definitions. Where a Play
question turns on a definition rather than on a fact about Concord, this file
gives the fact and says which way it points, and flags the one question that
genuinely needs a human decision.

## Assets

| Asset | Where | Size |
|---|---|---|
| Feature graphic | [`media/feature-graphic.png`](media/feature-graphic.png) (source: [`media/feature-graphic.svg`](media/feature-graphic.svg)) | 1024 × 500 |
| Phone screenshots ×6 | [`media/store/`](media/store/) | 1080 × 2400 each |
| App icon | `apps/mobile/assets/icon.png` (Play wants 512 × 512; regenerate with `npx @capacitor/assets@3.0.5 generate` if the master changes) | — |

Regenerate the feature graphic after editing the SVG:

```sh
rsvg-convert -w 1024 -h 500 docs/media/feature-graphic.svg -o docs/media/feature-graphic.png
```

The screenshots are raw device captures at 1080 × 2400 from an API 35 emulator —
no frames, no marketing overlays, no captions burned in. They are the app.
Descriptions for the Console, in upload order:

1. `phone-1-conversation.png` — a channel conversation between three members.
2. `phone-2-channels.png` — the guild drawer: guilds, channels, unread state.
3. `phone-3-voice.png` — a three-person voice call in a voice channel.
4. `phone-4-profile.png` — a profile card with its scene, ring and decoration.
5. `phone-5-privacy.png` — Privacy & safety, every switch and its default.
6. `phone-6-light.png` — the same conversation in the light theme.

**A note on aspect ratio.** These are 1080 × 2400 (a 20:9 phone). Play's phone
screenshot rules require each side between 320 px and 3840 px, and some Play
surfaces state a 16:9 / 9:16 ratio. If the Console rejects a 20:9 upload, the fix
is to recapture at 1080 × 1920 rather than to letterbox: set the emulator with
`adb shell wm size 1080x1920` before capturing. Do not pad — a padded screenshot
looks like a mistake on the listing card.

## Copy

### App name

```
Concord
```

### Short description (80 characters max)

```
Peer-to-peer encrypted chat. No company, no accounts, no server in the middle.
```

78 characters.

### Full description

```
Concord is a chat app with nothing in the middle. No company holds your
messages, because there is no company. No account database has your name in it,
because there are no accounts. Your identity is a keypair on your phone and your
history is an encrypted database beside it.

Guilds, direct messages, voice, video and screen sharing — all of it travels
directly between the people in the conversation, encrypted end to end with MLS
(RFC 9420), the IETF's group messaging standard. Only the people in the group
hold the keys. Removing someone rekeys the group.

WHAT YOU GET
• Guilds with text, voice, announcement and forum channels, roles and a signed
  governance log that decides who may change what
• Direct messages and group DMs, with a request queue that holds a stranger's
  first message unopened until you accept
• Voice and video calls with screen sharing, encrypted between participants
• Profile cards, custom emoji, polls, events, moments and a guild GIF pack
• Disappearing messages, per-channel retention, full-text search that never
  leaves the device
• Light and dark themes, and a lot of ways to make a profile yours

WHAT IT COSTS, STATED PLAINLY
Peer-to-peer is a real trade, not a slogan, so here is the other half. Peers you
connect to learn your IP address, as in any peer-to-peer system. Metadata is not
hidden: Concord authenticates identities, it does not conceal them. Encryption
protects a group from outsiders — it was never going to stop somebody you
invited from screenshotting what you sent them. And two people behind ordinary
home routers sometimes need one always-on node to introduce them; it can be a
friend's, it forwards only ciphertext, and you can run your own.

NOTHING IS COLLECTED
No analytics. No crash reporting. No advertising. No attribution. Not switched
off by default — not in the app at all. There is no server of ours for your
messages to reach. The two features that send anything to an outside search
service each have a switch in Privacy & safety, and one of them ships off.

Everything is free software under the AGPL-3.0. The source is the specification:
github.com/ZahakJ/concord
```

### Category and tags

- **Category:** Communication.
- **Tags:** messaging, chat, privacy, encryption, voice chat.
- **Contains ads:** No.
- **In-app purchases:** No.

### Contact and links

| Field | Value |
|---|---|
| Website | `https://zahakj.github.io/concord/` |
| Privacy policy | `https://zahakj.github.io/concord/privacy.html` |
| Support email | **Needs a decision — see "what remains manual".** Play requires one; the project has deliberately never published an address, and `SECURITY.md` routes vulnerability reports through GitHub's private reporting instead. |

## Data safety

Play's data safety form asks about data *collected* (transmitted off the device)
and *shared* (passed to a third party). The facts, all of which can be checked in
the source:

- **The developer receives nothing.** There is no backend belonging to this
  project, and the app ships configured to connect to no infrastructure at all.
- **There are no accounts**, so no name, email address, phone number or user ID
  is created, held or transmitted.
- **No analytics, crash-reporting, advertising or attribution library is in the
  build.** `apps/mobile/package.json` is Capacitor plus four of its plugins and a
  biometric one; `frontend/package.json` is an emoji set and two QR-code
  libraries.
- **No advertising ID or device identifier is read.**
- **Firebase Cloud Messaging ships as a dependency of the push plugin but never
  initialises**, because there is no `google-services.json` in the build and the
  init provider is disabled outright (`docs/PUSH.md`). Nothing registers a token
  and nothing is reported.
- **Message content, photos, files, and voice/video** do leave the device — to
  the other members of the conversation, end-to-end encrypted with MLS, never to
  the developer and never to a processor acting for the developer.

Suggested answers:

| Question | Answer | Why |
|---|---|---|
| Does your app collect or share any of the required user data types? | **No** | Nothing is transmitted to the developer or to a third party acting for the developer. Content that leaves the device goes only to the recipients the user chose, end-to-end encrypted. This is the question to re-read the Console's definitions on before submitting — see the note below. |
| Is all user data encrypted in transit? | **Yes** | MLS end-to-end for content; Noise/DTLS on every transport hop underneath it; the two optional search flows are HTTPS. |
| Do you provide a way for users to request that their data be deleted? | **Yes** | *Settings → Privacy & safety → Delete everything on this device* erases the identity keystore, the encrypted database, the MLS group state and the peer cache. There is no account to delete server-side because there is no server. |
| Data types collected | **None** | — |
| Data types shared | **None** | — |
| Is your app's data collection independently validated? | No | No third-party security review has been done. `SECURITY.md` says so too. |

**The one thing to check yourself.** Play defines "collection" as transmitting
data off the device, and lists exemptions — including data that is end-to-end
encrypted, and data sent to another user at the user's direction. Concord's
message traffic is both. If the Console's current wording does not exempt it,
declare **Messages → Other in-app messages**, **Photos and videos**, and **Audio
files** as *collected*, mark them **not shared**, mark every one of them
"Data is end-to-end encrypted", and set the purpose to **App functionality**.
Either answer is defensible from the facts above; what is not defensible is
declaring a data type the app does not touch. Do not tick Location, Contacts,
Financial info, Health, Calendar, or Personal info — none of them are read.

### The two off-device searches

Neither sends data to the developer, and neither is a Play data type on its own,
but both are worth knowing about when answering:

| Feature | Default | Where the query goes | What it sends |
|---|---|---|---|
| Game title search | **Off** | Valve's public storefront search | The partial title the user is typing, and the request's own IP |
| GIF search | **On** | The rendezvous node the *user* configured, which asks a GIF service on their behalf | The search terms. The GIF service sees only the node, never the user |

Both switches are enforced in the Go backend, not in the interface: switched off,
no request is made. Off-by-default for game search applies to new installs;
installs that already had a game collection when the switch arrived keep the
feature. See `internal/app/offdevice.go`.

### Permissions, and what each is for

Useful when the Console asks about sensitive permissions.

| Permission | Why |
|---|---|
| `INTERNET` | Peer connections |
| `ACCESS_NETWORK_STATE` | Wi-Fi vs cellular, so discovery slows down on a metered plan |
| `RECORD_AUDIO`, `MODIFY_AUDIO_SETTINGS` | Voice calls and voice messages |
| `CAMERA` | Video calls and scanning a device-link QR code |
| `POST_NOTIFICATIONS` | Message notifications, asked for on first use rather than at launch |
| `FOREGROUND_SERVICE` + `_DATA_SYNC` | The "Stay connected" preference: keeps the peer node alive so messages arrive while the app is backgrounded. Exists *because* there is no push |
| `FOREGROUND_SERVICE_MICROPHONE` | Android 14+ requires a microphone-type service for a call that outlives the screen |
| `WAKE_LOCK` | Proximity wake lock: screen off against the cheek during a call |
| `USE_BIOMETRIC` (and `USE_FINGERPRINT`, capped at API 27) | The optional app lock |

No `QUERY_ALL_PACKAGES`, no location, no contacts, no storage-wide access.

## Content rating (IARC questionnaire)

Concord is a communication app that carries user-generated content. The answers
follow from that:

- **Category:** Communication / social.
- **Does the app allow users to interact or exchange content?** Yes — text,
  images, files, voice and video, between users.
- **Can users share their location?** No. There is no location feature and the
  app holds no location permission.
- **Can users share personal information?** Yes, in the sense that any messenger
  lets someone type their own address into a message. The app requests none and
  stores none of its own.
- **Does the app contain violence / sexual content / drugs / gambling?** No
  content is authored or supplied by the app. What members send each other is
  theirs.
- **Is user-generated content moderated?** Partly, and honestly: there is no
  central moderation, because there is no server. Each guild's own admins govern
  it through a signed governance log; every user has blocking, which is enforced
  at the render path, and a report flow that routes to the guild's admins and
  writes a local evidence file. Say this plainly rather than claiming a
  moderation team.
- **Expected rating:** Teen / PEGI 12 or thereabouts, driven entirely by
  unmoderated user interaction rather than by any content the app ships.

The app must also be marked as **not** designed for children: it is
general-audience, there is no age gate, and it is not in the Families programme.
`PRIVACY.md` says the same thing in the same words, deliberately.

## What remains manual

Everything below needs a person, an account, or a key. Nothing in this
repository can do it.

1. **A Play Console developer account.** One-time registration fee, plus
   Google's identity verification. This is the gate on everything else.
2. **A support email address.** Play requires one on the listing. The project has
   never published an address — vulnerability reports go through GitHub's private
   reporting (`SECURITY.md`) — so this is a decision, not a lookup: register one,
   or use an address you are willing to have on a public listing forever.
3. **Signing.** `apps/mobile/android/app/build.gradle` reads
   `apps/mobile/android/keystore.properties` (gitignored) and expects the
   keystore itself outside the repository. Decide between Play App Signing
   (Google holds the app signing key, you hold an upload key) and managing the
   key yourself. Either way: **back the keystore up before the first upload.**
   Losing it after release means the sideload APK and the Play build stop being
   the same app.
4. **Build the bundle.** From a clean tree, with a JDK 17–21 on `JAVA_HOME`:

   ```sh
   make android-app VERSION=v0.57.0 MOBILE_VERSION_CODE=5700
   ```

   The `.aab` lands at
   `apps/mobile/android/app/build/outputs/bundle/release/app-release.aab` (all
   ABIs; Play splits per device). The same target also produces the arm64-only
   sideload APK the GitHub Release carries. `MOBILE_VERSION_CODE` has no useful
   default and the target refuses to run without one — Play rejects any upload
   whose code is not strictly greater than the last. The convention is
   minor × 100 + patch. CI runs the identical target
   (`.github/workflows/release.yml`), so a bundle built either way is the same
   bundle; see [RELEASING.md](RELEASING.md#android).
5. **Upload and fill in the Console**: the copy and questionnaire answers above,
   the assets from `docs/media/`, the privacy policy URL, target audience
   (13+, not designed for children), and the countries to release in.
6. **Answer the "app access" question.** Reviewers get no credentials because
   there are none: the app creates its identity on first launch and needs no
   sign-in. Say that in the app access notes, and add that to see more than an
   empty first-run screen a reviewer needs a second install to talk to, or an
   invite code — offer to supply one.

## What was fixed to get here

Recorded so nobody re-derives it. Four things blocked or embarrassed a
submission and are now done:

- The privacy policy was factually stale and not Play-shaped. It now states what
  is collected (nothing), names every outbound call, and has retention, deletion,
  children, change-notice and contact sections. It is published at
  `docs/privacy.html`.
- The two searches that left the device had no switch, so the policy had to
  describe them as unavoidable. Both are switches now, enforced in the backend.
- `usesCleartextTraffic="true"` was app-wide, to serve a listener that never
  leaves 127.0.0.1. It is scoped to loopback by a network security config.
- Deleting your data was reachable only from the login screen, behind "Forgot
  passphrase?". It is now a flow in Settings → Privacy & safety.
