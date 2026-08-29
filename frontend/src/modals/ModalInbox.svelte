<script>
  // The activity inbox: everything that concerns you, in one list.
  //
  // Three kinds of thing land here — someone named you, someone replied to
  // something you wrote, and someone said one of your own alert words — and the
  // row says which, because they are different claims about intent. Grouped by
  // conversation, newest first, and clicking one lands on the message.
  //
  // The whole list is a query over messages already on this device. Nothing was
  // recorded when they arrived, nothing is transmitted to build it, and the only
  // thing written down is one "I have looked at this" timestamp, here.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import EmptyState from "../EmptyState.svelte";
  import {
    S,
    refreshInbox,
    markInboxRead,
    jumpToInboxEntry,
    faceFor,
    clockOpts,
    selectGuild,
  } from "../lib/state.svelte.js";
  import { plural } from "../lib/plural.js";
  import { tooltip } from "../lib/tooltip.js";
  import { shortWhen } from "../lib/timestamp.js";
  import { govSentenceText } from "../lib/govlog.js";
  import { api } from "../lib/api.js";
  import { PERM, has } from "../lib/perms.js";

  let { onClose } = $props();

  let filter = $state("all");

  // Everything an organizer can act on already exists as a signed op — the
  // moderation log holds it. It was reachable only by remembering to open that
  // log, so "somebody was kicked", "a role changed", "a channel was deleted"
  // never reached the one place the app promises will tell you what concerns
  // you. Events made the cut and governance did not.
  //
  // Offered only to somebody who could have done it themselves: to everybody
  // else it is a list of decisions they cannot question and cannot undo, and
  // the log is already gated the same way.
  const canModerate = (g) =>
    !!g &&
    g.kind !== "dm" &&
    (g.isOwner ||
      has(g.myPerms || 0, PERM.MANAGE_MEMBERS) ||
      has(g.myPerms || 0, PERM.MANAGE_ROLES) ||
      has(g.myPerms || 0, PERM.MANAGE_CHANNELS) ||
      has(g.myPerms || 0, PERM.MUTE_MEMBERS));
  const modGuilds = $derived(S.guilds.filter(canModerate));

  const FILTERS = $derived([
    { id: "all", label: "Everything" },
    { id: "mention", label: "Mentions" },
    { id: "reply", label: "Replies" },
    { id: "keyword", label: "Alert words" },
    { id: "event", label: "Events" },
    ...(modGuilds.length ? [{ id: "moderation", label: "Moderation" }] : []),
  ]);

  // The governance pass is its own query over its own store, so it runs only
  // when the filter asks for it — and bounded on both axes, because it is one
  // round trip per guild.
  const MOD_GUILDS = 8;
  const MOD_PER_GUILD = 25;
  let modEntries = $state([]);
  let modLoading = $state(false);
  $effect(() => {
    if (filter !== "moderation") return;
    const guilds = modGuilds.slice(0, MOD_GUILDS);
    let live = true;
    modLoading = true;
    Promise.all(
      guilds.map(async (g) => {
        try {
          const page = await api.governanceLog(g.id, 0, MOD_PER_GUILD);
          return (page?.entries || []).map((e) => ({ ...e, guildId: g.id, guildName: g.name }));
        } catch {
          // One guild that will not answer must not empty the whole filter.
          return [];
        }
      }),
    ).then((pages) => {
      if (!live) return;
      modEntries = pages
        .flat()
        .sort((a, b) => (b.at || 0) - (a.at || 0))
        .slice(0, 50);
      modLoading = false;
    });
    return () => {
      live = false;
    };
  });

  const REASON = {
    mention: { icon: "megaphone", label: "Mentioned you" },
    reply: { icon: "reply", label: "Replied to you" },
    keyword: { icon: "bell", label: "Your alert word" },
    event: { icon: "calendar", label: "New event" },
  };

  const entries = $derived(
    filter === "moderation"
      ? []
      : filter === "all"
        ? S.inbox.entries
        : S.inbox.entries.filter((e) => e.reason === filter),
  );

  // One header per RUN of the same conversation, not per conversation. The
  // difference matters: collecting every entry of a channel into one group
  // would lift a Tuesday message above a Wednesday one from somewhere else, and
  // the whole list claims to be newest-first. A run breaks when the channel
  // changes, so the order is untouched and the header only repeats when the
  // conversation genuinely came back round.
  const groups = $derived.by(() => {
    const out = [];
    for (const e of entries) {
      const last = out[out.length - 1];
      if (last && last.channelId === e.channelId) {
        last.entries.push(e);
        continue;
      }
      out.push({ key: e.messageId, channelId: e.channelId, title: placeOf(e), entries: [e] });
    }
    return out;
  });

  // A DM's header has to name the person. The backend calls every DM guild
  // "Direct message" — that is the stored name, and it is the same for all of
  // them — so three different conversations printed three identical headers and
  // the grouping looked broken when it was not. The resolved name is already in
  // S.guilds (the sidebar shows it), so take it from there.
  function placeOf(e) {
    if (e.isDm) {
      const g = S.guilds.find((x) => x.id === e.guildId);
      const who = dmName(g?.name) || dmName(e.guildName) || e.senderName || e.sender.slice(0, 12);
      return who ? `Direct message · ${who}` : "Direct message";
    }
    if (e.channelName && e.guildName) return `#${e.channelName} · ${e.guildName}`;
    return e.channelName ? `#${e.channelName}` : e.guildName || "A conversation";
  }

  // The stored defaults are not names of anybody.
  function dmName(n) {
    return n && n !== "Direct message" && n !== "Group message" && n !== "New conversation" ? n : "";
  }

  const fmtTime = (ms) => shortWhen(ms, clockOpts());

  // A governance row lands where it happened: the guild, and its own log, which
  // is where the signature and the verdict are.
  function openModEntry(e) {
    if (e.guildId) selectGuild(e.guildId);
    S.modal = { kind: "modLog" };
  }
</script>

<Modal title="Inbox" {onClose} wide>
  <div class="head" role="group" aria-label="Filter the inbox">
    {#each FILTERS as f (f.id)}
      <button
        class="quiet chip"
        class:on={filter === f.id}
        aria-pressed={filter === f.id}
        onclick={() => (filter = f.id)}
      >
        {f.label}
      </button>
    {/each}
  </div>

  {#if filter === "moderation"}
    {#if modLoading && !modEntries.length}
      <p class="note">Reading the signed record…</p>
    {:else if !modEntries.length}
      <p class="note">Nothing has been moderated in the guilds you can act in.</p>
    {:else}
      <div class="groups scroll-fade">
        <section>
          <h3 class="place">Moderation</h3>
          {#each modEntries as e, i (e.guildId + ":" + i)}
            {@const face = faceFor(e.signer, e.signerName)}
            <button class="entry" onclick={() => openModEntry(e)}>
              <Avatar name={face.name} emoji={face.emoji} color={face.color} image={face.image} size={30} />
              <span class="col">
                <span class="top">
                  <strong>{face.name}</strong>
                  <span class="why moderation" use:tooltip={"From the guild's signed moderation log"}>
                    <Icon name="lock" size={10} />
                    {e.guildName || "Moderation"}
                  </span>
                  <span class="when">{fmtTime(e.at)}</span>
                </span>
                <span class="text">{govSentenceText(e)}</span>
              </span>
            </button>
          {/each}
        </section>
      </div>
    {/if}
  {:else if S.inbox.loading && !S.inbox.entries.length}
    <p class="note">Looking through what you missed…</p>
  {:else if !S.inbox.entries.length}
    <EmptyState
      icon="bell"
      headline="Nothing is waiting for you"
      sub="When someone names you, replies to something you wrote, or says one of your alert words, it lands here — across every guild and DM."
      {actions}
    />
  {:else if !entries.length}
    <p class="note">Nothing of that kind. Try another filter.</p>
  {:else}
    <div class="groups scroll-fade">
      {#each groups as g (g.key)}
        <section>
          <h3 class="place">{g.title}</h3>
          {#each g.entries as e (e.messageId)}
            {@const face = faceFor(e.sender, e.senderName)}
            {@const r = REASON[e.reason] || REASON.mention}
            <button class="entry" class:unread={e.unread} onclick={() => jumpToInboxEntry(e)}>
              <!-- faceFor, not memberByFpr: this dialog is cross-guild by
                   definition and the roster it was reaching for is the one guild
                   you happen to be standing in, so half the people in it drew as
                   grey initials with their real avatar on screen behind the
                   dialog. -->
              <Avatar
                name={face.name}
                emoji={face.emoji}
                color={face.color}
                image={face.image}
                size={30}
              />
              <span class="col">
                <span class="top">
                  {#if e.unread}<span class="dot" aria-hidden="true"></span>{/if}
                  <strong>{face.name}</strong>
                  <span class="why {e.reason}" use:tooltip={r.label}>
                    <Icon name={r.icon} size={10} />
                    {e.reason === "keyword" && e.term ? e.term : r.label}
                  </span>
                  <span class="when">{fmtTime(e.at)}</span>
                </span>
                <span class="text">{e.snippet}</span>
              </span>
            </button>
          {/each}
        </section>
      {/each}
    </div>
    <div class="foot">
      <span>{plural(entries.length, "item")}{S.inbox.unread ? ` · ${S.inbox.unread} unread` : ""}</span>
      <!-- Both actions live down here. They used to share the top row with the
           four filter chips, in a dialog narrow enough that the chips wrapped
           to a second line and "Alert words" was orphaned on it. Present
           whether or not there is anything to clear, and disabled when there is
           not: an action that appears and disappears as the count crosses zero
           moves the control beside it under the cursor. -->
      <button class="quiet act" disabled={!S.inbox.unread} onclick={markInboxRead}>
        <Icon name="check" size={13} />
        Mark all read
      </button>
      <!-- "Check again" reads like polling a server, in an app whose whole
           pitch is that there is not one. This list is a query over messages
           already on this device. -->
      <button
        class="quiet act"
        disabled={S.inbox.loading}
        onclick={() => refreshInbox({ soon: true })}
      >
        {S.inbox.loading ? "Refreshing…" : "Refresh"}
      </button>
    </div>
  {/if}
</Modal>

{#snippet actions()}
  <button onclick={() => (S.modal = { kind: "notifications" })}>Set up alert words</button>
{/snippet}

<style>
  /* The head is the four filters and nothing else, so they fit on one line at
     the dialog's real width. Anything that ACTS rather than filters lives in
     the footer with the count — that is where a reader looks when they have
     finished reading the list, which is when both of those buttons are for. */
  .head {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
    min-width: 0;
    margin-bottom: var(--sp-3);
  }
  /* `quiet` (app.css) carries the fill AND the ink. The ink is the point: the
     global button rule paints --accent-fg, which is chosen to survive on the
     accent and is near-black in the dark theme, so a chip that repainted only
     its background wrote black on charcoal. */
  .chip {
    padding: 5px 11px;
    min-width: 0;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    border-radius: 999px;
  }
  .chip.on {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .act {
    padding: 5px 11px;
    min-width: 0;
    flex: none;
    font-size: var(--fs-compact);
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  /* Both states restated, because app.css's `button.quiet:hover` is specific
     enough to outrank a bare `.chip.on` and would grey out the selected one. */
  @media (pointer: fine) {
    .chip:not(.on):hover {
      background: var(--border);
      color: var(--text);
    }
    .chip.on:hover {
      background: var(--accent-hover);
      color: var(--accent-fg);
    }
  }
  .note {
    margin: 0;
    padding: var(--sp-3) var(--sp-1);
    font-size: var(--fs-ui);
    color: var(--text-muted);
  }
  .groups {
    max-height: calc(58 * var(--vh));
    overflow-y: auto;
  }
  /* One scroller per sheet: a list that scrolls inside a sheet that scrolls
     makes the sheet feel arbitrarily sticky under a thumb. */
  @media (pointer: coarse), (max-width: 768px) {
    .groups {
      max-height: none;
      overflow-y: visible;
    }
  }
  .place {
    position: sticky;
    top: 0;
    z-index: 1;
    margin: 0;
    padding: var(--sp-2) var(--sp-1) 5px;
    background: var(--bg-elevated);
    font-size: var(--fs-tiny);
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-muted);
    unicode-bidi: plaintext;
  }
  /* ONE row, whose emphasis changes — not two components. Read rows used to
     lose the card entirely and sit naked on the dialog ground beside unread
     rows drawn as tinted plates with a rule down the side, so a list of nine
     things looked like two lists. The card, the rule and the padding are the
     row; unread turns the rule on and warms the ground. */
  .entry {
    display: flex;
    width: 100%;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: var(--sp-2) var(--sp-1);
    padding-left: calc(var(--sp-1) - 2px);
    border-left: 2px solid transparent;
    min-width: 0;
    text-align: left;
    background: color-mix(in srgb, var(--bg-3) 40%, transparent);
    color: var(--text);
    border-radius: var(--radius-sm);
  }
  .entry:hover {
    background: var(--bg-3);
  }
  /* An unread item wears the same gutter rule the channel list uses for an
     unread channel, so the vocabulary is one thing app-wide — plus a ground
     it can be picked out by at a glance and a dot beside the name, because the
     rail promises a number ("Inbox — 4 unread items") and a list where every
     row is drawn identically cannot say which four. */
  .entry.unread {
    border-left-color: var(--accent);
    background: var(--accent-soft);
  }
  .entry.unread:hover {
    background: color-mix(in srgb, var(--accent) 22%, transparent);
  }
  .entry.unread strong {
    font-weight: 700;
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    flex: none;
  }
  .col {
    min-width: 0;
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .top {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    font-size: var(--fs-compact);
  }
  .top strong {
    color: var(--text);
    unicode-bidi: plaintext;
  }
  .why {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 1px 7px;
    border-radius: 999px;
    font-size: var(--fs-tiny);
    background: var(--bg-3);
    color: var(--text-muted);
    /* The alert word a keyword entry names is the user's own text and can be
       any script, so the pill isolates it like every other name in the app. */
    unicode-bidi: plaintext;
  }
  /* Three reasons, three tints, matching the row highlights in the feed: amber
     for "someone meant you", accent for "you asked to be told". */
  .why.event {
    color: var(--accent-hover);
  }
  .why.mention,
  .why.reply {
    background: var(--warn-soft);
    color: var(--warn-text);
  }
  .why.keyword {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  /* Governance is neither: it is the record, not somebody addressing you. It
     names the guild rather than a reason, in the panel's quietest ink. */
  .why.moderation {
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .when {
    margin-left: auto;
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    white-space: nowrap;
  }
  /* The preview is one message's worth of somebody else's text, so it is
     clamped from three directions: two lines, hidden overflow, and a break
     rule that applies to an unbroken run with no spaces in it. A raw
     attachment token used to run straight past the card's right border. */
  .text {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    line-height: 1.45;
    min-width: 0;
    overflow-wrap: anywhere;
    overflow: hidden;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    unicode-bidi: plaintext;
  }
  .foot {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin-top: var(--sp-3);
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }
  /* The count claims the slack so both buttons sit together at the right. */
  .foot > span {
    flex: 1;
    min-width: 0;
  }
</style>
