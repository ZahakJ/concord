<script>
  // The guild's message-retention policy: how long members keep history before
  // their own copy prunes itself.
  //
  // NOT the same thing as ModalDisappear, and the copy here works hard to keep
  // them apart. That one is a timer an author puts on new messages they send in
  // one conversation, and it erases on every side because the expiry rides
  // inside the message. This is a standing rule for a whole guild that applies
  // to messages already sent, it is set by an admin rather than an author, and
  // every client enforces it on its own copy because there is no server to make
  // anyone do anything.
  import RailShell from "./RailShell.svelte";
  import Icon from "../Icon.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import { S, activeGuild, flash, refreshGuilds } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { RETAIN_OPTIONS } from "../lib/retention.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());

  // The floor matches the clamp in the governance replay (govstate.go): an
  // hour. A policy shorter than that is a foot-gun rather than a feature, and
  // per-message disappearing already covers wanting something gone quickly.
  const OPTIONS = RETAIN_OPTIONS;

  let current = $state(0);
  let busy = $state(false);
  let pending = $state(null); // option awaiting confirmation

  (async () => {
    try {
      current = (await api.guildRetention(S.activeGuildId)) || 0;
    } catch {
      /* older backend: no policy support, so "keep everything" is the truth */
    }
  })();

  function choose(o) {
    if (o.secs === current) return;
    // Turning a policy ON (or shortening one) destroys history on every honest
    // member's device and cannot be undone by setting it back. Turning it off
    // destroys nothing, so it needs no ceremony.
    if (o.secs !== 0 && (current === 0 || o.secs < current)) {
      pending = o;
      return;
    }
    apply(o);
  }

  async function apply(o) {
    pending = null;
    busy = true;
    try {
      await api.setRetention(S.activeGuildId, "", o.secs);
      current = o.secs;
      await refreshGuilds();
      flash(o.secs ? `History is now kept for ${o.label.toLowerCase()}` : "History is kept indefinitely", "success");
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }
</script>

<RailShell title="Message history" {onClose}>
  <p class="intro muted">
    How long this guild keeps messages. Older ones are removed from each
    member's device as it comes around to checking — including the bodies of
    deleted messages that moderators can otherwise still reveal.
  </p>

  <div class="opts" role="radiogroup" aria-label="How long to keep messages">
    {#each OPTIONS as o (o.secs)}
      <button
        class="opt"
        class:sel={current === o.secs}
        role="radio"
        aria-checked={current === o.secs}
        disabled={busy || !g?.canManage}
        onclick={() => choose(o)}
      >
        <span class="lbl">
          {o.label}
          {#if o.sub}<span class="sub">{o.sub}</span>{/if}
        </span>
        {#if current === o.secs}<Icon name="check" size={15} />{/if}
      </button>
    {/each}
  </div>

  <div class="notes">
    <p><Icon name="lock" size={13} /> <b>Pinned and saved messages are never removed.</b> A retention
      policy is housekeeping for the messages nobody marked; anything you or the guild deliberately
      kept stays.</p>
    <p><Icon name="members" size={13} /> <b>This is an agreement, not enforcement.</b> There is no
      server to make anyone forget. Every honest client prunes its own copy, so members who have
      been offline catch up when they next open the app — and somebody running a modified client
      need not forget at all. It protects you against a member's device being lost or seized later,
      not against the members themselves.</p>
  </div>

  {#if !g?.canManage}
    <p class="muted tiny">Only someone who can manage this guild may change it.</p>
  {/if}
</RailShell>

{#if pending}
  <ConfirmDialog
    title="Shorten how long history is kept?"
    body={`Messages older than ${pending.label.toLowerCase()} will be removed from every member's device, including yours. Pinned and saved messages stay. This cannot be undone by setting a longer time afterwards — what is gone is gone.`}
    confirmLabel="Set to {pending.label.toLowerCase()}"
    danger
    onConfirm={() => apply(pending)}
    onClose={() => (pending = null)}
  />
{/if}

<style>
  .intro {
    font-size: var(--fs-ui);
    line-height: 1.5;
    margin: 0 0 12px;
  }
  .opts {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .opt {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 11px 14px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .opt:hover:not(:disabled) {
    border-color: var(--accent);
  }
  .opt:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .opt.sel {
    border-color: var(--accent);
  }
  .lbl {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .sub {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  .notes {
    margin-top: 14px;
    display: flex;
    flex-direction: column;
    gap: 9px;
  }
  .notes p {
    margin: 0;
    font-size: var(--fs-tiny);
    line-height: 1.55;
    color: var(--text-muted);
  }
  .notes b {
    color: var(--text);
    font-weight: 600;
  }
  .tiny {
    font-size: var(--fs-tiny);
    margin: 12px 0 0;
  }
</style>
