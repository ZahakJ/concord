<script>
  // The guild's governance log, rendered as history.
  //
  // Nothing on this screen is fetched, computed by anyone else, or taken on
  // trust. Every row is one Ed25519-signed operation that already sits on this
  // device, folded in the order every peer folds it in, and the badge on the
  // right is this machine's own verdict on the signature — not a claim the
  // sender made about themselves.
  //
  // Two verdicts, not one, because they answer different questions: a signature
  // can verify perfectly and the replay can still have refused the op (a
  // moderator who had lost the role by the time it folded). A log that showed
  // only the first would print bans that never happened.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import EmptyState from "../EmptyState.svelte";
  import { S, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { tooltip } from "../lib/tooltip.js";
  import { plural } from "../lib/plural.js";
  import {
    GOV_FILTERS,
    matchesFilter,
    govSentence,
    permNames,
    verdictLabel,
    shortFingerprint,
  } from "../lib/govlog.js";

  let { onClose } = $props();

  const PAGE = 100;

  let entries = $state([]);
  let total = $state(0);
  let loading = $state(true);
  let loadingMore = $state(false);
  let filter = $state("all");

  const shown = $derived(entries.filter((e) => matchesFilter(e, filter)));
  const more = $derived(entries.length < total);

  async function load(offset = 0) {
    try {
      const page = await api.governanceLog(S.activeGuildId, offset, PAGE);
      total = page?.total || 0;
      entries = offset === 0 ? page?.entries || [] : [...entries, ...(page?.entries || [])];
    } catch (err) {
      flash(err);
    } finally {
      loading = false;
      loadingMore = false;
    }
  }

  function loadMore() {
    loadingMore = true;
    load(entries.length);
  }

  // The author's own clock, printed in the reader's locale. A governance record
  // is one of the few places in the app where the date matters as much as the
  // time, so both are always shown rather than the feed's relative wording.
  const stamp = (ms) =>
    ms
      ? new Date(ms).toLocaleString(undefined, {
          dateStyle: "medium",
          timeStyle: "short",
        })
      : "";

  load();
</script>

<Modal title="Moderation log" {onClose} wide>
  <p class="lede">
    Every role change, ban, mute and handover in this guild, signed by whoever
    made it and checked here on your own machine. Nothing was fetched to build
    this — the record is already on every member's device, which is why every
    member can read it.
  </p>

  <div class="filters" role="group" aria-label="Filter the log">
    {#each GOV_FILTERS as f (f.id)}
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

  {#if loading}
    <p class="note">Reading the log…</p>
  {:else if entries.length === 0}
    <EmptyState
      icon="list"
      headline="Nothing has been decided yet"
      sub="No roles, bans, mutes or handovers have happened in this guild. When one does, it will be signed and it will show up here."
    />
  {:else if shown.length === 0}
    <p class="note">Nothing in this part of the log yet. Try another filter.</p>
  {:else}
    <ul class="log">
      {#each shown as e (e.hash)}
        {@const v = verdictLabel(e)}
        <li class="row" class:doubt={v.id !== "ok"}>
          <Avatar name={e.signerName || e.signer} size={28} />
          <div class="body">
            <p class="say">
              {#each govSentence(e) as part, i (i)}
                {#if part.k === "person"}<span class="who">{part.v}</span
                  >{:else if part.k === "role"}<span
                    class="role"
                    style={part.color ? `color:${part.color}` : null}>{part.v}</span
                  >{:else if part.k === "channel"}<span class="chan">{part.v}</span
                  >{:else if part.k === "time"}<span class="chan">{stamp(part.v)}</span
                  >{:else}{part.v}{/if}
              {/each}
            </p>
            {#if e.type === "role_upsert" && permNames(e.perms).length}
              <p class="perms">{permNames(e.perms).join(" · ")}</p>
            {/if}
            <p class="meta">
              <span>{stamp(e.at)}</span>
              <span class="dot">·</span>
              <span
                class="verdict {v.id}"
                use:tooltip={v.id === "ok"
                  ? `Signed by ${shortFingerprint(e.signer)} and verified on this device`
                  : v.id === "refused"
                    ? "The signature is good, but the signer was not permitted to do this, so no peer applied it"
                    : "This operation's signature does not check out. No peer applied it."}
              >
                <Icon name={v.icon} size={12} />
                {v.label}
              </span>
            </p>
          </div>
        </li>
      {/each}
    </ul>

    <div class="foot">
      <span class="count">{plural(shown.length, "entry", "entries")} of {total} shown</span>
      {#if more}
        <button class="load" disabled={loadingMore} onclick={loadMore}>
          {loadingMore ? "Loading…" : "Load older"}
        </button>
      {/if}
    </div>
  {/if}
</Modal>

<style>
  .lede {
    margin: 0 0 var(--sp-3);
    font-size: var(--fs-compact);
    line-height: 1.55;
    color: var(--text-muted);
  }
  .note {
    margin: 0;
    padding: var(--sp-3) var(--sp-1);
    font-size: var(--fs-ui);
    color: var(--text-muted);
  }
  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
    margin-bottom: var(--sp-3);
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
  .log {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
    max-height: 60vh;
    overflow-y: auto;
  }
  /* One scroller per sheet: a list that scrolls inside a sheet that scrolls
     makes the sheet feel arbitrarily sticky under a thumb. */
  @media (pointer: coarse), (max-width: 768px) {
    .log {
      max-height: none;
      overflow-y: visible;
    }
  }
  .row {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: var(--sp-2) var(--sp-1);
    border-radius: var(--radius-sm);
  }
  .row:hover {
    background: var(--bg-3);
  }
  /* An entry that did not take effect is not an error to be shouted about — it
     is a fact about the record. A left rule marks it without recolouring the
     sentence, which still has to be readable. */
  .row.doubt {
    border-left: 2px solid var(--warn);
    padding-left: calc(var(--sp-1) - 2px);
  }
  .body {
    min-width: 0;
    flex: 1;
  }
  .say {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.5;
    /* A name, a role and a channel are all user-chosen text, and the record has
       to read correctly when one of them is Arabic. */
    unicode-bidi: plaintext;
  }
  .who {
    font-weight: 600;
    color: var(--text);
  }
  .role {
    font-weight: 600;
    color: var(--accent-hover);
  }
  .chan {
    color: var(--accent-hover);
  }
  .perms {
    margin: 3px 0 0;
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  .meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    margin: 3px 0 0;
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }
  .dot {
    color: var(--text-faint);
  }
  .verdict {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
  }
  .verdict.ok {
    color: var(--ok-text);
  }
  .verdict.refused {
    color: var(--warn-text);
  }
  .verdict.bad {
    color: var(--danger-text);
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
  .load {
    padding: 5px 12px;
    min-width: 0;
    font-size: var(--fs-compact);
    background: var(--bg-3);
    border-radius: var(--radius-sm);
  }
</style>
