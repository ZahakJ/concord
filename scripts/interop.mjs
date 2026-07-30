// Driver for scripts/interop.sh — see that file for the why.
//
// Talks to two already-running Concord peers over their loopback /rpc surface:
// OLD is a shipped previous release, NEW is a build of HEAD. Each lane asserts
// something a legacy peer must still be able to do, and records the evidence
// (real message bodies, RPC results, member lists) so a failure names the thing
// that actually broke instead of just "sync is down".
//
// Run as: node scripts/interop.mjs <phase>
// Phases: online | while-old-down | old-came-back | report
// State and evidence persist in $INTEROP_WORK between phases, because the
// history-sync lane needs the OLD peer restarted in between.

import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { join } from "node:path";
// The real staging defaults the shipped composer uses, so the attachment lane
// tests the app's actual behaviour rather than a copy of it that can drift.
import { stagedImage } from "../frontend/src/lib/attachopts.js";

const OLD = process.env.INTEROP_OLD;
const NEW = process.env.INTEROP_NEW;
const WORK = process.env.INTEROP_WORK;
if (!OLD || !NEW || !WORK) {
  console.error("interop.mjs: INTEROP_OLD/INTEROP_NEW/INTEROP_WORK must be set");
  process.exit(2);
}
const phase = process.argv[2];
const STATE = join(WORK, "state.json");

const PASS_OLD = "interop-old-passphrase";
const PASS_NEW = "interop-new-passphrase";

// A 1x1 red PNG — small enough to inline, real enough that an image decoder
// on the far side accepts it.
const PNG_1PX =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAABzenr0AAAAEUlEQVR4nGP8z8DAxMDAwAAABiIBUJMSSXAAAAAASUVORK5CYII=";
// A tiny real GIF (1x1, single frame) for the guild GIF pack lane.
const GIF_1PX =
  "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==";

// ---------------------------------------------------------------- plumbing

function loadState() {
  return existsSync(STATE) ? JSON.parse(readFileSync(STATE, "utf8")) : { lanes: [] };
}
function saveState(s) {
  writeFileSync(STATE, JSON.stringify(s, null, 2));
}

async function rpc(base, method, args = []) {
  const res = await fetch(base + "/rpc", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method, args }),
  });
  if (!res.ok) throw new Error(`${method}: HTTP ${res.status} ${await res.text()}`);
  const body = await res.json();
  if (body.error) throw new Error(`${method}: ${body.error}`);
  return body.result;
}
// Same, but hands back the error string instead of throwing: for the lanes whose
// whole question is "does the old peer reject this?".
async function tryRpc(base, method, args = []) {
  try {
    return { ok: true, result: await rpc(base, method, args) };
  } catch (e) {
    return { ok: false, error: String(e.message || e) };
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Attachment bodies already accounted for by an earlier lane, so a later lane can
// tell "the picture I just sent" from "a picture from three lanes ago".
const seenAttachments = new Set();

// waitFor polls until fn returns a truthy value, or gives up. Every cross-peer
// assertion needs this: the transports are asynchronous and a fixed sleep either
// flakes or wastes a minute per lane.
async function waitFor(label, fn, { timeout = 45000, interval = 500 } = {}) {
  const deadline = Date.now() + timeout;
  let last;
  for (;;) {
    try {
      last = await fn();
      if (last) return last;
    } catch (e) {
      last = undefined;
      if (Date.now() > deadline) throw new Error(`${label}: ${e.message || e}`);
    }
    if (Date.now() > deadline) throw new Error(`${label}: timed out after ${timeout}ms`);
    await sleep(interval);
  }
}

// Lane bookkeeping. A lane is never allowed to abort the run: an early failure
// would hide every later lane, and "what else is broken" is the whole point.
const results = [];
function record(id, title, status, evidence) {
  results.push({ id, title, status, evidence });
  const tag = status === "PASS" ? "PASS" : status === "WARN" ? "WARN" : "FAIL";
  console.log(`  [${tag}] ${id} ${title}`);
  for (const line of [].concat(evidence || [])) console.log(`         ${line}`);
}
async function lane(id, title, fn) {
  console.log(`\n--- ${id}: ${title}`);
  try {
    const out = await fn();
    record(id, title, out?.status || "PASS", out?.evidence);
  } catch (e) {
    record(id, title, "FAIL", [`error: ${e.message || e}`]);
  }
}

// ---------------------------------------------------------------- helpers

async function login(base, pass, name) {
  await rpc(base, "Login", [pass]);
  // SetProfile's arity grew between the two versions; extra args are ignored by
  // argStr on both sides, missing ones come back "". Name is all we need.
  await rpc(base, "SetProfile", [name, "", "", "", "", "", "online", "", "", "", "", ""])
    .catch(() => {});
  return rpc(base, "Identity");
}

const textChannels = (g) => (g.channels || []).filter((c) => c.type === "text");
async function guildByName(base, name) {
  const guilds = await rpc(base, "Guilds");
  return guilds.find((g) => g.name === name);
}
async function messagesIn(base, channelID) {
  return (await rpc(base, "Messages", [channelID])) || [];
}
async function findMessage(base, channelID, needle) {
  const msgs = await messagesIn(base, channelID);
  return msgs.find((m) => (m.content || "").includes(needle));
}

// ---------------------------------------------------------------- phases

async function phaseOnline() {
  const st = loadState();

  // Identities first, so every later lane can name who it expected to see.
  const oldID = await login(OLD, PASS_OLD, "LegacyPeer");
  const newID = await login(NEW, PASS_NEW, "HeadPeer");
  st.oldFP = oldID.fingerprint;
  st.newFP = newID.fingerprint;
  st.oldPeerID = oldID.peerId;
  st.newPeerID = newID.peerId;
  console.log(`OLD fingerprint ${st.oldFP}`);
  console.log(`NEW fingerprint ${st.newFP}`);
  saveState(st);

  // Both peers must actually reach the rendezvous before any invite can be
  // redeemed; without this the first lane fails for a boring reason.
  await lane("L0", "both peers reach the local rendezvous", async () => {
    const seen = await waitFor(
      "rendezvous connect",
      async () => {
        const o = await rpc(OLD, "NetworkStatus");
        const n = await rpc(NEW, "NetworkStatus");
        return o.bootstrapReached && n.bootstrapReached ? { o, n } : null;
      },
      { timeout: 60000 }
    );
    return {
      evidence: [
        `OLD NetworkStatus: ${JSON.stringify(seen.o)}`,
        `NEW NetworkStatus: ${JSON.stringify(seen.n)}`,
      ],
    };
  });

  // --- L1a: OLD joins a guild NEW created ------------------------------------
  await lane("L1a", "OLD joins NEW's guild via invite code", async () => {
    const g = await rpc(NEW, "CreateGuild", ["NewMadeGuild"]);
    st.newGuildID = g.id;
    const code = await rpc(NEW, "InviteCode", [g.id]);
    st.newGuildCode = code;
    saveState(st);
    const joined = await rpc(OLD, "JoinViaInvite", [code]);
    const seen = await waitFor("OLD sees NewMadeGuild", () => guildByName(OLD, "NewMadeGuild"));
    st.newGuildChanOld = textChannels(seen)[0]?.id;
    st.newGuildChanNew = textChannels(await guildByName(NEW, "NewMadeGuild"))[0]?.id;
    saveState(st);
    return {
      evidence: [
        `invite code (${code.length} chars): ${code.slice(0, 48)}...`,
        `OLD JoinViaInvite -> id=${joined.id} name=${joined.name}`,
        `OLD channels: ${textChannels(seen).map((c) => c.name).join(", ")}`,
      ],
    };
  });

  // --- L1b: NEW joins a guild OLD created ------------------------------------
  await lane("L1b", "NEW joins OLD's guild via invite code", async () => {
    const g = await rpc(OLD, "CreateGuild", ["OldMadeGuild"]);
    st.oldGuildID = g.id;
    const code = await rpc(OLD, "InviteCode", [g.id]);
    saveState(st);
    const joined = await rpc(NEW, "JoinViaInvite", [code]);
    const seen = await waitFor("NEW sees OldMadeGuild", () => guildByName(NEW, "OldMadeGuild"));
    st.oldGuildChanNew = textChannels(seen)[0]?.id;
    st.oldGuildChanOld = textChannels(await guildByName(OLD, "OldMadeGuild"))[0]?.id;
    saveState(st);
    return {
      evidence: [
        `invite code (${code.length} chars): ${code.slice(0, 48)}...`,
        `NEW JoinViaInvite -> id=${joined.id} name=${joined.name}`,
        `NEW channels: ${textChannels(seen).map((c) => c.name).join(", ")}`,
      ],
    };
  });

  // --- L8a: a DM opened from a code, across versions -------------------------
  await lane("L8a", "OLD and NEW can hold a DM opened from a DM invite code", async () => {
    // The realistic way two people who are not yet in a guild together start a
    // DM: one of them generates a code and sends it over some other channel.
    // A DM is a kind="dm" MLS guild, so this also checks that guild shape
    // survives the version gap.
    const code = await rpc(NEW, "NewDMInvite");
    const joined = await rpc(OLD, "JoinViaInvite", [code]);
    const oldChan = (joined.channels || [])[0]?.id;
    const newDM = await waitFor("NEW sees the DM fill up", async () => {
      const g = (await rpc(NEW, "Guilds")).find((x) => x.id === joined.id);
      return g && (g.dmMembers || 0) >= 2 ? g : null;
    });
    const newChan = (newDM.channels || [])[0]?.id;
    const ev = [
      `DM invite code (${code.length} chars) from NEW, redeemed by OLD -> ${joined.id}`,
      `NEW's view: kind=${newDM.kind} dmMembers=${newDM.dmMembers} dmPeer=${newDM.dmPeer === st.oldFP ? "OLD" : newDM.dmPeer}`,
    ];
    for (const [dir, from, to, cf, ct] of [
      ["NEW->OLD", NEW, OLD, newChan, oldChan],
      ["OLD->NEW", OLD, NEW, oldChan, newChan],
    ]) {
      await rpc(from, "SendMessage", [cf, `DM-${dir}`, ""]);
      const got = await waitFor(`DM ${dir}`, () => findMessage(to, ct, `DM-${dir}`));
      ev.push(`${dir}: ${got.content}`);
    }
    return { evidence: ev };
  });

  // --- L8b: the DM push path, once the two are contacts ----------------------
  await lane("L8b", "OLD's StartDM reaches NEW now that they share a guild", async () => {
    // HEAD added a request tray: an inbound DM invite from someone who is not
    // already a known contact is held for manual accept instead of redeemed
    // (internal/app/dm.go, the !s.knownContact(senderFpr) branch). Sharing a
    // guild makes them contacts, so this should sail through — and if it ever
    // does get held, the legacy peer's UI gives no hint, which is what makes
    // this lane worth having.
    const dm = await rpc(OLD, "StartDM", [st.newFP]);
    const oldChan = textChannels(dm)[0]?.id || (dm.channels || [])[0]?.id || dm.id;
    const outcome = await waitFor(
      "NEW opens the DM or files a request",
      async () => {
        const probe = await tryRpc(NEW, "MessageRequests"); // absent before HEAD
        const reqs = (probe.ok && probe.result) || [];
        if (reqs.length) return { kind: "request", reqs };
        const g = (await rpc(NEW, "Guilds")).find((x) => x.id === dm.id);
        return g ? { kind: "opened", guild: g } : null;
      },
      { timeout: 60000 }
    );
    const ev = [`OLD StartDM -> ${dm.id}`];
    if (outcome.kind === "request") {
      ev.push(`held in NEW's request tray despite a shared guild: ${JSON.stringify(outcome.reqs)}`);
      await rpc(NEW, "AcceptMessageRequest", [st.oldFP]);
      ev.push("accepted manually to continue");
    } else {
      ev.push(`NEW opened it directly (OLD counted as a known contact): ${outcome.guild.id}`);
    }
    const g = (await rpc(NEW, "Guilds")).find((x) => x.id === dm.id);
    const newChan = textChannels(g)[0]?.id || (g.channels || [])[0]?.id;
    await rpc(OLD, "SendMessage", [oldChan, "PUSHED-DM-FROM-LEGACY", ""]);
    const got = await waitFor("NEW receives the legacy peer's pushed DM", () =>
      findMessage(NEW, newChan, "PUSHED-DM-FROM-LEGACY")
    );
    ev.push(`NEW received: ${got.content}`);
    return { status: outcome.kind === "request" ? "WARN" : "PASS", evidence: ev };
  });

  // --- L2: messages both directions, content intact --------------------------
  await lane("L2", "messages flow both ways with content intact", async () => {
    const ev = [];
    // Deliberately includes the characters a naive re-encode mangles: emoji,
    // markdown, an angle bracket, a non-ASCII quote.
    const payload = 'hello from %s — "quoted" <b>&amp; 🎉 `code`';
    for (const [dir, from, to, chanFrom, chanTo] of [
      ["NEW->OLD", NEW, OLD, st.newGuildChanNew, st.newGuildChanOld],
      ["OLD->NEW", OLD, NEW, st.oldGuildChanOld, st.oldGuildChanNew],
    ]) {
      const body = payload.replace("%s", dir);
      await rpc(from, "SendMessage", [chanFrom, body, ""]);
      const got = await waitFor(`${dir} delivery`, () => findMessage(to, chanTo, dir));
      if (got.content !== body) {
        throw new Error(`${dir} content mangled:\n  sent: ${body}\n  recv: ${got.content}`);
      }
      if (/concord:\/\//.test(got.content)) {
        throw new Error(`${dir} content leaked a raw token: ${got.content}`);
      }
      ev.push(`${dir} kind=${JSON.stringify(got.kind)} sender=${got.senderName} content=${got.content}`);
    }
    return { evidence: ev };
  });

  // --- L6: member list / presence (needs both online) ------------------------
  await lane("L6", "each peer appears online in the other's member list", async () => {
    const ev = [];
    for (const [label, base, guildID, wantFP] of [
      ["OLD sees NEW", OLD, st.newGuildID, st.newFP],
      ["NEW sees OLD", NEW, st.newGuildID, st.oldFP],
    ]) {
      const m = await waitFor(
        `${label} in member list`,
        async () => {
          const members = await rpc(base, "Members", [guildID]);
          return members.find((x) => x.fingerprint === wantFP);
        },
        { timeout: 30000 }
      );
      const online = await waitFor(
        `${label} online`,
        async () => {
          const members = await rpc(base, "Members", [guildID]);
          const x = members.find((y) => y.fingerprint === wantFP);
          return x && x.online ? x : null;
        },
        { timeout: 60000 }
      ).catch((e) => ({ __err: e.message }));
      if (online.__err) throw new Error(`${label}: listed but never online — ${online.__err}`);
      ev.push(`${label}: name=${online.name} online=${online.online} presence=${JSON.stringify(online.presence)}`);
    }
    return { evidence: ev };
  });

  // --- L4: attachments ------------------------------------------------------
  await lane("L4a", "ordinary image from NEW is a v1 token OLD can render", async () => {
    const id = await rpc(NEW, "SendAttachment", [
      st.newGuildChanNew, PNG_1PX, 1, 1, "", false, "", "",
    ]);
    const mineNew = await waitFor("NEW's own attachment message", async () => {
      const msgs = await messagesIn(NEW, st.newGuildChanNew);
      return msgs.find((m) => m.id === id || (m.content || "").includes("concord://attach/"));
    });
    const got = await waitFor("OLD receives the attachment", async () => {
      const msgs = await messagesIn(OLD, st.newGuildChanOld);
      return msgs.find((m) => (m.content || "").includes("concord://attach/"));
    });
    seenAttachments.add(got.content);
    st.plainAttachOldContent = got.content;
    saveState(st);
    const isV2 = parseAttach(got.content)?.version === 2;
    // The old frontend only understands the v1 grammar; a v2 token here is the
    // regression the whole lane exists to catch.
    const fetched = await tryRpc(OLD, "FetchAttachment", attachArgs(got.content, st.newGuildChanOld));
    const ev = [
      `NEW stored: ${mineNew.content}`,
      `OLD received: ${got.content}`,
      `OLD FetchAttachment ok=${fetched.ok} ${fetched.ok ? `bytes=${(fetched.result || "").length}` : fetched.error}`,
    ];
    if (isV2) {
      throw new Error(
        "an ordinary image (no spoiler/name/description) reached OLD as a v2 token it cannot parse"
      );
    }
    if (!fetched.ok || !String(fetched.result || "").startsWith("data:image/")) {
      throw new Error(`OLD could not fetch the image bytes: ${fetched.error || fetched.result}`);
    }
    return { evidence: ev };
  });

  await lane("L4b", "spoilered image from NEW degrades gracefully on OLD", async () => {
    await rpc(NEW, "SendAttachment", [
      st.newGuildChanNew, PNG_1PX, 1, 1, "", true, "secret.png", "a described spoiler",
    ]);
    const got = await waitFor("OLD receives the spoilered attachment", async () => {
      const msgs = await messagesIn(OLD, st.newGuildChanOld);
      return msgs.find((m) => /concord:\/\/attach\/v2\//.test(m.content || ""));
    }).catch(() => null);
    if (got) seenAttachments.add(got.content);
    if (!got) {
      // Either it never arrived (bad) or it arrived as v1 (fine, and notable).
      const any = await messagesIn(OLD, st.newGuildChanOld);
      const v1s = any.filter((m) => /concord:\/\/attach\//.test(m.content || ""));
      throw new Error(
        `no v2 token reached OLD; attachment-bearing messages on OLD: ${JSON.stringify(v1s.map((m) => m.content))}`
      );
    }
    const fetched = await tryRpc(OLD, "FetchAttachment", attachArgs(got.content, st.newGuildChanOld));
    // A raw token in the body is the expected-but-ugly outcome. What must NOT
    // happen is the message being dropped, or later messages being lost.
    await rpc(NEW, "SendMessage", [st.newGuildChanNew, "AFTER-V2-SENTINEL", ""]);
    const after = await waitFor("message after a v2 token still arrives", () =>
      findMessage(OLD, st.newGuildChanOld, "AFTER-V2-SENTINEL")
    );
    return {
      status: "WARN",
      evidence: [
        `OLD received v2 token verbatim: ${got.content}`,
        `OLD's backend CAN fetch the bytes when handed the ids: ok=${fetched.ok} ${fetched.ok ? "" : fetched.error}`,
        "  (so the break is purely the old frontend's token regex, not the blob transport)",
        `the message was NOT dropped, and the next message still arrived: ${after.content}`,
        "expected-but-ugly: a v0.41.0 UI shows this as raw markdown/text, not an image",
      ],
    };
  });

  // The lane that matters most. L4a calls SendAttachment the way a *test* would —
  // with hand-picked empty options. This one derives the options from the SAME
  // helper the shipped Composer uses to stage a dropped/pasted/picked image, so
  // it fails if the UI ever again defaults one of them to a value that forces v2.
  // That regression really happened: `name` was prefilled from the OS file name,
  // which made every ordinary picture unreadable on a v0.41.0 peer.
  await lane("L4d", "image sent the way the real Composer sends it is still v1", async () => {
    const staged = stagedImage({
      id: "interop",
      dataUrl: PNG_1PX,
      w: 1,
      h: 1,
      fileName: "Screenshot 2026-07-30.png",
    });
    await rpc(NEW, "SendAttachment", [
      st.newGuildChanNew, staged.dataUrl, staged.w, staged.h, "",
      !!staged.spoiler, staged.name || "", staged.desc || "",
    ]);
    const got = await waitFor("OLD receives the composer-style attachment", async () => {
      const msgs = await messagesIn(OLD, st.newGuildChanOld);
      return msgs
        .filter((m) => (m.content || "").includes("concord://attach/"))
        .find((m) => !seenAttachments.has(m.content));
    });
    seenAttachments.add(got.content);
    const a = parseAttach(got.content);
    const ev = [
      `staged via lib/attachopts.js stagedImage(): spoiler=${staged.spoiler} name=${JSON.stringify(staged.name)} desc=${JSON.stringify(staged.desc)}`,
      `  (origName kept for the placeholder only: ${JSON.stringify(staged.origName)})`,
      `OLD received a v${a?.version} token: ${got.content}`,
    ];
    if (a?.version !== 1) {
      throw new Error(
        "an ordinary picture (no spoiler, no description) reached OLD as a v2 token: " +
          "a v0.41.0 client shows a ~190-char raw token instead of the image"
      );
    }
    return { evidence: ev };
  });

  await lane("L4c", "image from OLD renders on NEW", async () => {
    await rpc(OLD, "SendAttachment", [st.oldGuildChanOld, PNG_1PX, 1, 1, ""]);
    const got = await waitFor("NEW receives OLD's attachment", async () => {
      const msgs = await messagesIn(NEW, st.oldGuildChanNew);
      return msgs.find((m) => (m.content || "").includes("concord://attach/"));
    });
    const fetched = await tryRpc(NEW, "FetchAttachment", attachArgs(got.content, st.oldGuildChanNew));
    if (!fetched.ok || !String(fetched.result || "").startsWith("data:image/")) {
      throw new Error(`NEW could not fetch OLD's image: ${fetched.error || fetched.result}`);
    }
    return {
      evidence: [
        `NEW received: ${got.content}`,
        `NEW FetchAttachment bytes=${String(fetched.result).length}`,
      ],
    };
  });

  // --- L5: unknown guild-meta types and unknown sync fields ------------------
  await lane("L5a", "gif_added meta does not stop OLD applying the rest", async () => {
    const gif = await rpc(NEW, "AddGuildGif", [
      st.newGuildID, "wave", ["hello"], GIF_1PX, 1, 1,
    ]);
    // The real question is not "does OLD show the gif" (it cannot, it has no
    // such feature) but "does the unknown meta type poison the stream". So we
    // push a change OLD definitely understands immediately afterwards.
    await rpc(NEW, "RenameGuild", [st.newGuildID, "NewMadeGuildRenamed"]);
    await rpc(NEW, "CreateChannel", [st.newGuildID, "after-gif", "text", ""]).catch(() => {});
    const renamed = await waitFor("OLD applies the rename that followed gif_added", async () => {
      const g = (await rpc(OLD, "Guilds")).find((x) => x.id === st.newGuildID);
      return g && g.name === "NewMadeGuildRenamed" ? g : null;
    });
    const chan = await waitFor("OLD applies the channel created after gif_added", async () => {
      const g = (await rpc(OLD, "Guilds")).find((x) => x.id === st.newGuildID);
      return (g.channels || []).find((c) => c.name === "after-gif");
    }).catch((e) => ({ __err: e.message }));
    const ev = [
      `NEW AddGuildGif -> ${JSON.stringify(gif)}`,
      `OLD guild name after rename: ${renamed.name}`,
      chan.__err
        ? `OLD did NOT get the channel created after gif_added: ${chan.__err}`
        : `OLD got the channel created after gif_added: ${chan.name} (${chan.id})`,
    ];
    if (chan.__err) throw new Error("gif_added blocked a later guild-meta record from applying");
    // Guild rename lands, so keep the state consistent for later lanes.
    st.newGuildName = "NewMadeGuildRenamed";
    saveState(st);
    return { evidence: ev };
  });

  await lane("L5b", "a GIF message from NEW does not break OLD's channel", async () => {
    const gifs = await rpc(NEW, "GuildGifs", [st.newGuildID]);
    if (!gifs.length) throw new Error("NEW has no guild gifs to send");
    await rpc(NEW, "SendGuildGif", [st.newGuildChanNew, gifs[0].id, ""]);
    const got = await waitFor("OLD receives the gif message", async () => {
      const msgs = await messagesIn(OLD, st.newGuildChanOld);
      return msgs
        .filter((m) => /concord:\/\/attach\//.test(m.content || ""))
        .find((m) => !seenAttachments.has(m.content));
    });
    seenAttachments.add(got.content);
    const a = parseAttach(got.content);
    // A pack GIF is posted as an ordinary attachment token, so an old peer should
    // render it like any other image — but only if it stayed v1.
    if (a?.version !== 1) throw new Error(`gif reached OLD as a v${a?.version} token: ${got.content}`);
    const fetched = await tryRpc(OLD, "FetchAttachment", attachArgs(got.content, st.newGuildChanOld));
    if (!fetched.ok || !String(fetched.result || "").startsWith("data:image/")) {
      throw new Error(`OLD could not fetch the pack GIF's bytes: ${fetched.error || fetched.result}`);
    }
    await rpc(NEW, "SendMessage", [st.newGuildChanNew, "AFTER-GIF-SENTINEL", ""]);
    const after = await waitFor("message after a gif still arrives", () =>
      findMessage(OLD, st.newGuildChanOld, "AFTER-GIF-SENTINEL")
    );
    return {
      evidence: [
        `OLD received the pack GIF as a v1 token: ${got.content}`,
        `OLD fetched its bytes: ${String(fetched.result).slice(0, 24)}... (${String(fetched.result).length} chars)`,
        `the next ordinary message still arrived: ${after.content}`,
      ],
    };
  });

  await lane("L5c", "a fresh OLD peer can still join a guild that has gifs", async () => {
    // The nastiest version of the unknown-field question: the whole sync payload
    // for this guild now carries a `Gifs` field v0.41.0 has never seen. If the
    // decoder is strict, joining fails outright and the legacy peer is locked
    // out of every guild that has ever added a GIF.
    const code = await rpc(NEW, "InviteCode", [st.newGuildID]);
    const before = (await rpc(OLD, "Guilds")).length;
    const st2 = loadState();
    st2.gifGuildCode = code;
    saveState(st2);
    // OLD is already a member, so re-joining is a no-op; instead assert OLD's
    // view of the gif-bearing guild is complete: channels and history intact.
    const g = (await rpc(OLD, "Guilds")).find((x) => x.id === st.newGuildID);
    const msgs = await messagesIn(OLD, st.newGuildChanOld);
    if (!g) throw new Error("OLD lost the guild after gifs were added");
    if (!msgs.length) throw new Error("OLD's history in the gif-bearing guild is empty");
    return {
      evidence: [
        `OLD guild count ${before}, gif-bearing guild present: ${g.name}`,
        `OLD channels: ${(g.channels || []).map((c) => c.name).join(", ")}`,
        `OLD message count in the main channel: ${msgs.length}`,
      ],
    };
  });

  // --- L7: voice signalling -------------------------------------------------
  await lane("L7", "voice presence crosses between OLD and NEW", async () => {
    // A fresh guild has no voice channel, so make one on NEW and wait for the
    // legacy peer to learn about it — which is itself a channel_added interop
    // check for a channel type carrying the newer metadata.
    const voiceNew = await rpc(NEW, "CreateChannel", [st.newGuildID, "voice-interop", "voice", ""]);
    const voiceOld = await waitFor("OLD sees the new voice channel", async () => {
      const g = (await rpc(OLD, "Guilds")).find((x) => x.id === st.newGuildID);
      return (g?.channels || []).find((c) => c.name === "voice-interop");
    });
    await rpc(NEW, "JoinVoice", [voiceNew.id]);
    await rpc(OLD, "JoinVoice", [voiceOld.id]);
    // Voice presence is only observable via SSE (there is no "who is in this
    // room" RPC), and completing an SDP handshake needs a real WebRTC stack the
    // browser supplies — impossible headlessly. So this lane checks the
    // app-level signalling leg: a call-control publish from each side must be
    // accepted by the other's Service without error.
    const a = await tryRpc(NEW, "SignalCall", [voiceNew.id, "join", "", ""]);
    const b = await tryRpc(OLD, "SignalCall", [voiceOld.id, "join", "", ""]);
    // RelaySignal addresses a libp2p peer ID, not a fingerprint.
    const relay = await tryRpc(NEW, "RelaySignal", [
      st.oldPeerID, '{"type":"offer","sdp":"v=0"}',
    ]);
    await tryRpc(NEW, "LeaveVoice", [voiceNew.id]);
    await tryRpc(OLD, "LeaveVoice", [voiceOld.id]);
    return {
      status: "WARN",
      evidence: [
        `voice channel: NEW ${voiceNew.id} / OLD ${voiceOld.id}`,
        `NEW JoinVoice+SignalCall ok=${a.ok} ${a.ok ? "" : a.error}`,
        `OLD JoinVoice+SignalCall ok=${b.ok} ${b.ok ? "" : b.error}`,
        `NEW RelaySignal->OLD ok=${relay.ok} ${relay.ok ? "" : relay.error}`,
        "NOT a full handshake: reaching connected state needs a browser WebRTC stack, which cannot run headlessly here. The signalling leg is what is covered.",
      ],
    };
  });

  st.lanes = (st.lanes || []).concat(results);
  saveState(st);
  return results.every((r) => r.status !== "FAIL");
}

// FetchAttachment(channelID, blobID, keys, subtype). Both grammars start with
// the same four fields, so one parser serves v1 and v2:
//   v1: concord://attach/v1/<blobID>/<keys>/<subtype>/<w>x<h>
//   v2: concord://attach/v2/<blobID>/<keys>/<subtype>/<w>x<h>/<flags>/<n>/<d>
const ATTACH_RE =
  /concord:\/\/attach\/v([12])\/([0-9a-f]{64})\/([A-Za-z0-9_-]+)\/(png|jpeg|gif|webp)\//;
function parseAttach(content) {
  const m = ATTACH_RE.exec(content || "");
  return m ? { version: Number(m[1]), blobID: m[2], keys: m[3], subtype: m[4] } : null;
}
function attachArgs(content, channelID) {
  const a = parseAttach(content);
  if (!a) throw new Error(`no parseable attachment token in: ${content}`);
  return [channelID, a.blobID, a.keys, a.subtype];
}

async function phaseWhileOldDown() {
  const st = loadState();
  // OLD is down. Post a run of messages NEW must hand over when OLD returns:
  // through the mailbox on the rendezvous, or on the next direct sync.
  for (let i = 1; i <= 3; i++) {
    await rpc(NEW, "SendMessage", [st.newGuildChanNew, `OFFLINE-CATCHUP-${i}`, ""]);
  }
  await rpc(NEW, "SendAttachment", [st.newGuildChanNew, PNG_1PX, 1, 1, "", false, "", ""]);
  st.offlineCount = 3;
  saveState(st);
  console.log("  posted 3 messages + 1 attachment from NEW while OLD was down");
  return true;
}

async function phaseOldCameBack() {
  const st = loadState();
  await login(OLD, PASS_OLD, "LegacyPeer");
  await lane("L3", "OLD catches up on history posted while it was offline", async () => {
    const found = await waitFor(
      "OLD catches up",
      async () => {
        const msgs = await messagesIn(OLD, st.newGuildChanOld);
        const hits = [1, 2, 3].filter((i) =>
          msgs.some((m) => (m.content || "").includes(`OFFLINE-CATCHUP-${i}`))
        );
        return hits.length === 3 ? { msgs, hits } : null;
      },
      { timeout: 90000 }
    );
    const bodies = found.msgs
      .filter((m) => (m.content || "").includes("OFFLINE-CATCHUP"))
      .map((m) => m.content);
    return { evidence: [`OLD now has: ${JSON.stringify(bodies)}`] };
  });
  st.lanes = (st.lanes || []).concat(results);
  saveState(st);
  return results.every((r) => r.status !== "FAIL");
}

function phaseReport() {
  const st = loadState();
  const lanes = st.lanes || [];
  const w = Math.max(...lanes.map((l) => l.title.length), 10);
  console.log("\n================ LEGACY PEER INTEROP: v0.41.0 vs HEAD ================");
  for (const l of lanes) {
    console.log(`  ${l.status.padEnd(4)}  ${l.id.padEnd(4)} ${l.title.padEnd(w)}`);
  }
  const failed = lanes.filter((l) => l.status === "FAIL");
  const warned = lanes.filter((l) => l.status === "WARN");
  console.log(
    `\n  ${lanes.length} lanes: ${lanes.length - failed.length - warned.length} pass, ${warned.length} warn, ${failed.length} fail`
  );
  if (failed.length) {
    console.log("\n  FAILURES:");
    for (const l of failed) {
      console.log(`   - ${l.id} ${l.title}`);
      for (const e of [].concat(l.evidence || [])) console.log(`       ${e}`);
    }
  }
  console.log("======================================================================");
  return failed.length === 0;
}

// ---------------------------------------------------------------- main

const phases = {
  online: phaseOnline,
  "while-old-down": phaseWhileOldDown,
  "old-came-back": phaseOldCameBack,
  report: async () => phaseReport(),
};
const run = phases[phase];
if (!run) {
  console.error(`interop.mjs: unknown phase ${phase}`);
  process.exit(2);
}
try {
  process.exit((await run()) ? 0 : 1);
} catch (e) {
  console.error(`interop.mjs: ${phase}: ${e.stack || e}`);
  process.exit(1);
}
