<script>
  // Lists a guild's banned members and lets a moderator lift a ban.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, refreshRightPanel, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

  let { onClose } = $props();

  let bans = $state([]);
  let loading = $state(true);
  let busy = $state("");
  let q = $state("");

  // Display names are self-asserted and collide, so this is the one screen
  // that cannot identify people by name alone. The fingerprint is the durable
  // identity; the date, the moderator and the reason all come out of the
  // guild's own signed log, where they already were.
  const shortFpr = (f) => (f || "").replace(/\s+/g, "").slice(0, 16).replace(/(.{4})/g, "$1 ").trim();
  const banLine = (b) =>
    ["Banned", b.at ? when(b.at) : "", b.byName ? `by ${b.byName}` : ""].filter(Boolean).join(" ");
  const when = (ms) =>
    ms ? new Date(ms).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" }) : "";

  const rows = $derived.by(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return bans;
    return bans.filter((b) =>
      `${b.name || ""} ${b.fingerprint || ""} ${b.reason || ""} ${b.byName || ""}`
        .toLowerCase()
        .includes(needle),
    );
  });

  async function load() {
    loading = true;
    try {
      bans = (await api.bans(S.activeGuildId)) || [];
    } catch (err) {
      flash(err);
    } finally {
      loading = false;
    }
  }

  // Lifting a ban lets someone back into a room they were thrown out of, and
  // the Unban button used to be a single unguarded click beside a row whose
  // only identifier was a display name.
  function confirmUnban(b) {
    const name = b.name || shortFpr(b.fingerprint);
    S.modal = {
      kind: "confirm",
      title: `Lift the ban on ${name}?`,
      body: "They'll be able to join again with an invite. Their old messages are unaffected either way.",
      confirmLabel: "Unban",
      danger: false,
      onConfirm: () => {
        S.modal = null;
        unban(b.fingerprint);
      },
    };
  }

  async function unban(fpr) {
    busy = fpr;
    try {
      await api.unbanMember(S.activeGuildId, fpr);
      await refreshRightPanel();
      bans = bans.filter((b) => b.fingerprint !== fpr);
      flash("Ban lifted", "success");
    } catch (err) {
      flash(err);
    } finally {
      busy = "";
    }
  }

  load();
</script>

<Modal title="Banned members" {onClose}>
  {#if loading}
    <p class="muted empty">Loading…</p>
  {:else if bans.length === 0}
    <p class="muted empty">No one is banned from this guild.</p>
  {:else}
    {#if bans.length > 4}
      <label class="search">
        <Icon name="search" size={14} />
        <input bind:value={q} placeholder="Search name, fingerprint or reason" aria-label="Search bans" />
      </label>
    {/if}
    {#if !rows.length}
      <p class="muted empty">Nothing here matches that.</p>
    {/if}
    <div class="list">
      {#each rows as b (b.fingerprint)}
        <div class="ban-row">
          <Avatar name={b.name || b.fingerprint} size={30} />
          <span class="who">
            <span class="ban-name">{b.name || "Unknown"}</span>
            <button
              class="fpr"
              title="Copy the full fingerprint"
              onclick={() => {
                navigator.clipboard?.writeText(b.fingerprint);
                flash("Fingerprint copied", "success");
              }}
            >
              {shortFpr(b.fingerprint)}… <Icon name="copy" size={11} />
            </button>
            <!-- Built in JS, not stitched from three template branches: the
                 newline before an {#if} is swallowed and the line read
                 "Banned Aug 28, 2026by Amina Sadiq". -->
            <span class="meta">{banLine(b)}</span>
            {#if b.reason}
              <span class="reason">&ldquo;{b.reason}&rdquo;</span>
            {/if}
          </span>
          <button class="unban" disabled={busy === b.fingerprint} onclick={() => confirmUnban(b)}>
            Unban
          </button>
        </div>
      {/each}
    </div>
  {/if}
</Modal>

<style>
  .empty {
    font-size: var(--fs-ui);
    padding: 8px 2px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 420px;
    overflow-y: auto;
  }
  /* One scroller per sheet: a list that scrolls inside a sheet that scrolls
     makes the sheet feel arbitrarily sticky under a thumb. */
  @media (pointer: coarse), (max-width: 768px) {
    .list {
      max-height: none;
      overflow-y: visible;
    }
  }
  .search {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-faint);
  }
  .search input {
    flex: 1;
    border: 0;
    background: transparent;
    padding: 7px 0;
  }
  .search input:focus {
    outline: none;
  }
  .ban-row {
    display: flex;
    align-items: flex-start;
    gap: 9px;
    padding: 9px 6px;
    border-radius: var(--radius-sm);
  }
  .who {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .fpr {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    background: none;
    border: 0;
    padding: 0;
    font-family: var(--font-mono, monospace);
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  .fpr:hover {
    color: var(--text);
  }
  .meta {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .reason {
    font-size: var(--fs-small);
    color: var(--text-muted);
    font-style: italic;
  }
  .ban-row:hover {
    background: var(--bg-3);
  }
  .ban-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-ui);
  }
  .unban {
    align-self: center;
    padding: 5px 12px;
    font-size: var(--fs-compact);
    background: var(--bg-3);
    color: var(--text);
    border-radius: var(--radius-sm);
    flex: none;
  }
  .unban:hover,
  .unban:active {
    background: var(--accent);
    color: var(--accent-fg);
  }
</style>
