<script>
  // The archive panel: what this guild's imported history is, how big it is,
  // and how much of it this particular device is holding.
  //
  // Every member sees this, not just the owner, because the questions it answers
  // are a member's questions: where did the history above my channels come from,
  // who vouched for it, how far back does it go, and is it costing me disk.
  import RailShell from "./RailShell.svelte";
  import Icon from "../Icon.svelte";
  import EmptyState from "../EmptyState.svelte";
  import { S, activeGuild, flash, openPanel, refreshChronicle } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { plural } from "../lib/plural.js";
  import { fmtBytes, fmtCount, rangeLabel, dayLabel } from "../lib/chronicle.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  const c = $derived(S.chronicle);
  let pinning = $state(false);

  // The index is refreshed on entry: cached-page counts move as people read,
  // and a panel about storage that shows last week's number is worse than one
  // that shows none.
  $effect(() => {
    refreshChronicle(S.activeGuildId);
  });

  const channels = $derived((c?.channels || []).filter((ch) => ch.messages > 0));
  const cachedPct = $derived(c?.chunks ? Math.round(((c.chunksCached || 0) / c.chunks) * 100) : 0);
  const imported = $derived(c?.created ? dayLabel(c.created * 1e9) : "");

  // The archive's overall span is the widest of its channels'. The manifest
  // records the span per channel and not once for the whole thing, so it is
  // assembled here rather than read off.
  const span = $derived.by(() => {
    let first = 0;
    let last = 0;
    for (const ch of channels) {
      if (ch.firstNano && (!first || ch.firstNano < first)) first = ch.firstNano;
      if (ch.lastNano > last) last = ch.lastNano;
    }
    return rangeLabel(first, last) || "—";
  });

  // The whole archive on this device: the pages plus the media sealed with them.
  // The pin switch is about all of it, so the number beside the switch is too.
  const fullSize = $derived((c?.bytes || 0) + (c?.attachmentBytes || 0));

  async function togglePin() {
    if (!c || pinning) return;
    const want = !c.pinned;
    pinning = true;
    try {
      await api.setChroniclePinned(S.activeGuildId, want);
      await refreshChronicle(S.activeGuildId);
      flash(
        want ? "Keeping the whole archive on this device" : "The archive may now be evicted",
        "success",
      );
    } catch (err) {
      flash(err);
    } finally {
      pinning = false;
    }
  }

  const chanIcon = (type) =>
    /voice/i.test(type || "") ? "speaker" : /forum|thread/i.test(type || "") ? "forum" : "hash";
</script>

<RailShell title="Archive" wide {onClose}>
  {#if !c}
    <!-- EmptyState centres its own contents, not the block. In the hub's
         1000×660 pane that left a 400px card stuck in the top-left of a
         mostly-empty page — the archive "window" looking off-centre. Fill
         the pane and centre the illustration in the leftover. -->
    <div class="empty-wrap">
      <EmptyState
        icon="clock"
        headline="No archive here"
        sub="An archive is a community's older history, imported once and carried by every member as a small index. This guild has never had one attached."
      />
    </div>
    {#if g?.isOwner}
      <div class="actions">
        <button class="ghost" onclick={onClose}>Close</button>
        <button onclick={() => openPanel("chronicleImport", "chronicle")}>Import a chat archive</button>
      </div>
    {:else}
      <div class="actions"><button class="ghost" onclick={onClose}>Close</button></div>
    {/if}
  {:else}
    <div class="src">
      <span class="src-ic"><Icon name="clock" size={18} /></span>
      <span class="src-text">
        <strong>{c.source || "Imported history"}</strong>
        <span class="muted small">
          {imported ? `Imported ${imported}` : "Imported"} · signed by
          {c.signer === S.identity.fingerprint ? "you" : `${c.signer.slice(0, 12)}…`}
        </span>
      </span>
    </div>
    {#if c.description}
      <p class="desc">{c.description}</p>
    {/if}

    <div class="totals">
      <div class="tot">
        <span class="tv">{fmtCount(c.messages)}</span>
        <span class="tl">messages</span>
      </div>
      <div class="tot">
        <span class="tv">{span}</span>
        <span class="tl">covering</span>
      </div>
      <div class="tot">
        <span class="tv">{fmtCount(channels.length)}</span>
        <span class="tl">{channels.length === 1 ? "channel" : "channels"}</span>
      </div>
      <div class="tot">
        <span class="tv">{fmtBytes(fullSize)}</span>
        <span class="tl">whole archive</span>
      </div>
    </div>

    <div class="sec-head"><h4>Channels</h4></div>
    <div class="tbl">
      {#each channels as ch (ch.id)}
        <div class="trow">
          <span class="c-name">
            <Icon name={chanIcon(ch.type)} size={12} />
            <span class="cn">{ch.name}</span>
          </span>
          <span class="c-num">{fmtCount(ch.messages)}</span>
          <span class="c-range">{rangeLabel(ch.firstNano, ch.lastNano) || "—"}</span>
        </div>
      {/each}
    </div>

    <div class="sec-head"><h4>On this device</h4></div>
    <div class="cache">
      <div class="cbar"><span class="cfill" style="width:{cachedPct}%"></span></div>
      <!-- Pages, explicitly: the total above counts the media too, and a bar
           that read "55 KB of 855 KB" while claiming to be full would be a
           worse lie than a longer label. -->
      <span class="clabel">
        Pages held here — {fmtBytes(c.cachedBytes)} of {fmtBytes(c.bytes)},
        {c.chunksCached || 0} of {plural(c.chunks, "page")}
      </span>
      <!-- No seeder count: nothing the backend reports says which members hold
           an archive, and a number invented from "peers we happen to be
           connected to" would be a guess dressed as a fact. -->
      <p class="hint">
        Pages you don't have are fetched from other members the moment you scroll to
        them, and kept once read.
      </p>
    </div>

    <label class="pinrow">
      <input type="checkbox" checked={c.pinned} disabled={pinning} onchange={togglePin} />
      <span class="rtext">
        <span class="rt">Keep the full archive on this device</span>
        <span class="rs">
          {fmtBytes(fullSize)} · pinned pages are never dropped to make room for newer
          attachments.
          {#if c.signer === S.identity.fingerprint}
            You imported this, so your copy is the original.
          {/if}
        </span>
      </span>
    </label>

    {#if S.isMobile}
      <p class="hint wifi">
        <Icon name="devices" size={12} /> On a metered connection the app won't fetch archive
        pages until you tell it to.
      </p>
    {/if}

    <div class="actions">
      <button class="ghost" onclick={onClose}>Close</button>
    </div>
  {/if}
</RailShell>

<style>
  .empty-wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    min-height: 220px;
  }
  .src {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-2);
  }
  .src-ic {
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    border-radius: var(--radius-md);
    background: var(--accent-soft);
    color: var(--accent-hover);
    flex-shrink: 0;
  }
  .src-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .src-text strong {
    font-size: var(--fs-ui);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .small {
    font-size: var(--fs-small);
  }
  .desc {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.5;
    color: var(--text-muted);
  }
  .sec-head h4 {
    margin: var(--sp-2) 0 0;
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .totals {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: var(--sp-2);
  }
  .tot {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--sp-2) var(--sp-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-2);
    min-width: 0;
  }
  .tv {
    font-size: var(--fs-title);
    font-weight: 700;
    line-height: 1.15;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .tl {
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .tbl {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    /* Clips the rounded corner off the first and last row's fill. */
    overflow: hidden;
    /* …and `overflow: hidden` is also what makes this shrinkable: it zeroes the
       automatic minimum size, so as a child of the dialog's own column flexbox
       the table collapsed to a 8px sliver the moment the content was taller
       than a phone sheet. The dialog is the scroller; nothing inside it should
       be squeezed to make the content fit. */
    flex: none;
    background: var(--bg-2);
  }
  .trow {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 74px 128px;
    align-items: center;
    gap: var(--sp-2);
    padding: 7px var(--sp-3);
    font-size: var(--fs-compact);
    border-bottom: 1px solid var(--hairline, var(--border));
  }
  .trow:last-child {
    border-bottom: none;
  }
  .c-name {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
  }
  .cn {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .c-num {
    text-align: right;
    font-variant-numeric: tabular-nums;
    color: var(--text-muted);
  }
  .c-range {
    color: var(--text-faint);
    font-size: var(--fs-tiny);
    text-align: right;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (max-width: 620px) {
    .trow {
      grid-template-columns: minmax(0, 1fr) 74px;
    }
    .trow .c-range {
      grid-column: 1 / -1;
      text-align: left;
    }
  }
  .cache {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .cbar {
    height: 8px;
    border-radius: 999px;
    background: var(--bg-3);
    overflow: hidden;
  }
  .cfill {
    display: block;
    height: 100%;
    border-radius: 999px;
    background: linear-gradient(90deg, var(--accent), var(--accent-hover));
    transform-origin: left center;
    animation: bar-across 0.55s var(--ease-spring) both;
  }
  @keyframes bar-across {
    from {
      transform: scaleX(0);
    }
    to {
      transform: scaleX(1);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .cfill {
      animation: none;
    }
  }
  .clabel {
    font-size: var(--fs-small);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  .hint {
    margin: 0;
    font-size: var(--fs-small);
    line-height: 1.5;
    color: var(--text-faint);
  }
  .wifi {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
  }
  .pinrow {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: var(--sp-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-2);
    cursor: pointer;
  }
  .pinrow input {
    margin-top: 2px;
    flex-shrink: 0;
  }
  .rtext {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .rt {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .rs {
    font-size: var(--fs-small);
    color: var(--text-faint);
    line-height: 1.45;
  }
</style>
