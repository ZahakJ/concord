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
  import { S, refreshInbox, markInboxRead, jumpToInboxEntry, memberByFpr, clockOpts } from "../lib/state.svelte.js";
  import { plural } from "../lib/plural.js";
  import { tooltip } from "../lib/tooltip.js";

  let { onClose } = $props();

  let filter = $state("all");

  const FILTERS = [
    { id: "all", label: "Everything" },
    { id: "mention", label: "Mentions" },
    { id: "reply", label: "Replies" },
    { id: "keyword", label: "Alert words" },
  ];

  const REASON = {
    mention: { icon: "megaphone", label: "Mentioned you" },
    reply: { icon: "reply", label: "Replied to you" },
    keyword: { icon: "bell", label: "Your alert word" },
  };

  const entries = $derived(
    filter === "all" ? S.inbox.entries : S.inbox.entries.filter((e) => e.reason === filter),
  );

  // Grouped by conversation, in the order the conversations first appear — so
  // the busiest recent place is at the top without a second sort deciding
  // something the newest-first order already said.
  const groups = $derived.by(() => {
    const out = [];
    const byKey = new Map();
    for (const e of entries) {
      const key = e.channelId;
      let g = byKey.get(key);
      if (!g) {
        g = { key, title: placeOf(e), entries: [] };
        byKey.set(key, g);
        out.push(g);
      }
      g.entries.push(e);
    }
    return out;
  });

  function placeOf(e) {
    if (e.isDm) return e.guildName || "Direct message";
    if (e.channelName && e.guildName) return `#${e.channelName} · ${e.guildName}`;
    return e.channelName ? `#${e.channelName}` : e.guildName || "A conversation";
  }

  function fmtTime(ms) {
    try {
      return new Date(ms).toLocaleString([], {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        ...clockOpts(),
      });
    } catch {
      return "";
    }
  }
</script>

<Modal title="Inbox" {onClose} wide>
  <div class="head">
    <div class="filters" role="group" aria-label="Filter the inbox">
      {#each FILTERS as f (f.id)}
        <button
          class="chip"
          class:on={filter === f.id}
          aria-pressed={filter === f.id}
          onclick={() => (filter = f.id)}
        >
          {f.label}
        </button>
      {/each}
    </div>
    {#if S.inbox.unread > 0}
      <button class="clear" onclick={markInboxRead}>
        Mark all read <Icon name="check" size={12} />
      </button>
    {/if}
  </div>

  {#if S.inbox.loading && !S.inbox.entries.length}
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
    <div class="groups">
      {#each groups as g (g.key)}
        <section>
          <h3 class="place">{g.title}</h3>
          {#each g.entries as e (e.messageId)}
            {@const mem = memberByFpr(e.sender)}
            {@const r = REASON[e.reason] || REASON.mention}
            <button class="entry" class:unread={e.unread} onclick={() => jumpToInboxEntry(e)}>
              <Avatar
                name={e.senderName || e.sender}
                emoji={mem?.emoji}
                color={mem?.color}
                image={mem?.avatar}
                size={30}
              />
              <span class="col">
                <span class="top">
                  <strong>{e.senderName || e.sender.slice(0, 12)}</strong>
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
      <button class="refresh" disabled={S.inbox.loading} onclick={() => refreshInbox({ soon: true })}>
        {S.inbox.loading ? "Checking…" : "Check again"}
      </button>
    </div>
  {/if}
</Modal>

{#snippet actions()}
  <button onclick={() => (S.modal = { kind: "notifications" })}>Set up alert words</button>
{/snippet}

<style>
  /* The head wraps as one row until it cannot, and then the action drops to a
     line of its own rather than floating vertically centred against two rows of
     chips — which is what it did at the dialog's real width. */
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--sp-2);
    margin-bottom: var(--sp-3);
  }
  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
  }
  .clear {
    margin-left: auto;
  }
  .chip {
    padding: 5px 11px;
    min-width: 0;
    font-size: var(--fs-compact);
    background: var(--bg-3);
    border-radius: 999px;
  }
  .chip.on {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .clear,
  .refresh {
    padding: 5px 11px;
    min-width: 0;
    flex: none;
    font-size: var(--fs-compact);
    background: var(--bg-3);
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .note {
    margin: 0;
    padding: var(--sp-3) var(--sp-1);
    font-size: var(--fs-ui);
    color: var(--text-muted);
  }
  .groups {
    max-height: 58vh;
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
  .entry {
    display: flex;
    width: 100%;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: var(--sp-2) var(--sp-1);
    min-width: 0;
    text-align: left;
    background: transparent;
    border-radius: var(--radius-sm);
  }
  .entry:hover {
    background: var(--bg-3);
  }
  /* An unread item wears the same gutter rule the channel list uses for an
     unread channel, so the vocabulary is one thing app-wide. */
  .entry.unread {
    border-left: 2px solid var(--accent);
    padding-left: calc(var(--sp-1) - 2px);
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
  }
  /* Three reasons, three tints, matching the row highlights in the feed: amber
     for "someone meant you", accent for "you asked to be told". */
  .why.mention,
  .why.reply {
    background: var(--warn-soft);
    color: var(--warn-text);
  }
  .why.keyword {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .when {
    margin-left: auto;
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    white-space: nowrap;
  }
  .text {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    line-height: 1.45;
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
    justify-content: space-between;
    gap: var(--sp-2);
    margin-top: var(--sp-3);
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }
</style>
