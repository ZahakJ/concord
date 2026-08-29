<script>
  // "While you were away." An inline summary that sits at the unread boundary
  // when you come back to a guild after a long enough absence.
  //
  // It goes HERE, in the feed, rather than in a dialog, because it is about a
  // place in the conversation: everything below the line is what you have not
  // read, and this is the one-paragraph version of it. A dialog would have to be
  // dismissed before you could see whether it was worth reading.
  //
  // Everything on it was computed on this device from what it already had.
  import Icon from "./Icon.svelte";
  import {
    S,
    dismissCatchUp,
    selectChannel,
    jumpToInboxEntry,
    channelTypeIcon,
  } from "./lib/state.svelte.js";
  import { humanAway } from "./lib/digest.js";
  import { plural } from "./lib/plural.js";
  import { tooltip } from "./lib/tooltip.js";

  let { digest } = $props();

  // Collapsed shows the headline only. It starts open, because a summary you
  // have to open is a summary you will not read — but the state is remembered
  // for the session so somebody who does not want it can put it away.
  let open = $state(true);

  const REASON = { mention: "megaphone", reply: "reply", keyword: "bell" };

  function goto(channelId) {
    dismissCatchUp();
    selectChannel(channelId);
  }
</script>

<div class="catchup" role="region" aria-label="While you were away">
  <div class="head">
    <button
      class="toggle"
      aria-expanded={open}
      onclick={() => (open = !open)}
      use:tooltip={open ? "Collapse the summary" : "Expand the summary"}
    >
      <span class="chev" class:open><Icon name="chevron" size={12} /></span>
      <span class="title">While you were away</span>
      <span class="sub">
        {humanAway(digest.awayMs)} · {plural(digest.total, "new message")}
        {#if digest.mentions}· {plural(digest.mentions, "mention")}{/if}
      </span>
    </button>
    <button class="x" aria-label="Dismiss the summary" use:tooltip={"Dismiss"} onclick={dismissCatchUp}>
      <Icon name="close" size={10} />
    </button>
  </div>

  {#if open}
    {#if digest.highlights.length}
      <!-- What was aimed at you, first. For most people this is the whole
           reason to look, and burying it under channel counts would mean
           reading a table to find out whether anything mattered. -->
      <ul class="hits">
        {#each digest.highlights as e (e.messageId)}
          <li>
            <button onclick={() => jumpToInboxEntry(e)}>
              <span class="badge {e.reason}"><Icon name={REASON[e.reason] || "bell"} size={10} /></span>
              <strong>{e.senderName || "Someone"}</strong>
              <span class="say">{e.snippet}</span>
              <span class="where">#{e.channelName}</span>
            </button>
          </li>
        {/each}
      </ul>
      {#if digest.moreHighlights}
        <button class="more" onclick={() => (S.modal = { kind: "inbox" })}>
          {plural(digest.moreHighlights, "more item")} in your inbox
        </button>
      {/if}
    {/if}

    {#if digest.channels.length}
      <ul class="chans">
        {#each digest.channels as c (c.id)}
          <li>
            <button onclick={() => goto(c.id)} title={c.name}>
              <Icon name={channelTypeIcon(c.type)} size={11} />
              <span class="cname">{c.name}</span>
              <span class="cnum" class:mention={c.mentions > 0}>
                {c.count > 99 ? "99+" : c.count}
              </span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</div>

<style>
  .catchup {
    margin: var(--sp-3) var(--sp-edge);
    padding: var(--sp-2) var(--sp-3) var(--sp-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-1);
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .toggle {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: var(--sp-2);
    flex: 1;
    min-width: 0;
    padding: 4px 0;
    background: transparent;
    text-align: left;
  }
  .chev {
    align-self: center;
    display: inline-flex;
    color: var(--text-faint);
    /* The glyph points RIGHT at rest (Icon's chevron is a ">"), which is the
       collapsed state; open turns it down. */
    transition: transform var(--dur-standard) var(--ease-out);
  }
  .chev.open {
    transform: rotate(90deg);
  }
  .title {
    font-size: var(--fs-ui);
    font-weight: 600;
    color: var(--text);
  }
  .sub {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  .x {
    flex: none;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    min-width: 0;
    padding: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-faint);
  }
  .x:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  ul {
    list-style: none;
    margin: var(--sp-2) 0 0;
    padding: 0;
  }
  .hits button {
    display: flex;
    align-items: baseline;
    gap: 7px;
    width: 100%;
    min-width: 0;
    padding: 5px 6px;
    background: transparent;
    border-radius: var(--radius-sm);
    text-align: left;
    font-size: var(--fs-compact);
  }
  .hits button:hover {
    background: var(--bg-3);
  }
  .badge {
    align-self: center;
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    flex: none;
    border-radius: 50%;
    background: var(--warn-soft);
    color: var(--warn-text);
  }
  /* An alert word is a different event from a mention, and wears the accent
     that says so everywhere else in the app. */
  .badge.keyword {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .hits strong {
    flex: none;
    color: var(--text);
    unicode-bidi: plaintext;
  }
  .say {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-muted);
    unicode-bidi: plaintext;
  }
  .where {
    flex: none;
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    unicode-bidi: plaintext;
  }
  .more {
    margin-top: 6px;
    padding: var(--sp-1) var(--sp-2);
    min-width: 0;
    background: transparent;
    color: var(--accent-hover);
    font-size: var(--fs-tiny);
  }
  .chans {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .chans button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    padding: 4px 9px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
    font-size: var(--fs-tiny);
  }
  .chans button:hover {
    color: var(--text);
  }
  /* A count chip does not need the whole title. Without a cap one post's chip
     was eight times the width of "#general" next to it and the row wrapped to
     three ragged lines, so the actual channels — the thing the row is for —
     were the hardest part of it to pick out. */
  .cname {
    unicode-bidi: plaintext;
    max-width: 22ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .cnum {
    font-weight: 700;
    color: var(--text);
  }
  .cnum.mention {
    color: var(--danger-text);
  }
</style>
