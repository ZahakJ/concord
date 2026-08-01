<script>
  // Message requests: people you have no relationship with who want to DM you.
  // Nothing here has been joined — the backend is holding their invite code
  // un-redeemed (internal/app/request.go), which is why the row can show who
  // knocked but not what they said: reading it would mean joining their group,
  // and joining is what hands them your profile, presence and mailbox key.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, flash, acceptRequest, declineRequest, nameFor } from "../lib/state.svelte.js";

  let { onClose } = $props();
  let busy = $state("");

  const label = (r) => r.fromName || nameFor(r.from) || r.from.slice(0, 9);

  // A request carries only a self-asserted name — never an image (we haven't
  // joined their group, so there's nothing else to know). But if we've already
  // LEARNED this person's profile elsewhere (a shared guild, a past session),
  // show that locally-known face instead of an initials disc. Never rendered
  // from anything the request itself supplied.
  const face = (r) => S.contacts.find((c) => c.fingerprint === r.from);

  async function accept(r) {
    busy = r.from;
    try {
      await acceptRequest(r.from);
      flash(`Opened your conversation with ${label(r)}`, "success");
    } catch (err) {
      flash(err);
    } finally {
      busy = "";
    }
  }

  async function decline(r, block) {
    busy = r.from;
    try {
      await declineRequest(r.from, block);
      flash(block ? `Blocked ${label(r)}` : "Request deleted", "success");
    } catch (err) {
      flash(err);
    } finally {
      busy = "";
    }
  }
</script>

<Modal title="Message requests" {onClose}>
  {#if S.requests.length === 0}
    <p class="muted empty">Nothing waiting. Requests from people you don't know show up here.</p>
  {:else}
    <p class="muted tiny intro">
      They haven't reached you yet — Concord is holding the invitation, not the conversation. Until
      you accept, they can't see your profile, whether you're online, or anything else. Deleting is
      silent: they're never told.
    </p>
    <div class="list">
      {#each S.requests as r (r.from)}
        <div class="row">
          <Avatar
            name={label(r)}
            image={face(r)?.avatar || ""}
            emoji={face(r)?.emoji || ""}
            color={face(r)?.color || ""}
            size={32}
          />
          <span class="who">
            <strong>{label(r)}</strong>
            <span class="tiny muted mono">{r.from.slice(0, 12)}…</span>
          </span>
          <div class="acts">
            <button
              class="ghost"
              disabled={busy === r.from}
              title="Delete without telling them"
              onclick={() => decline(r, false)}>Delete</button
            >
            <button
              class="ghost danger"
              disabled={busy === r.from}
              title="Delete and stop them contacting you again"
              onclick={() => decline(r, true)}>Block</button
            >
            <button disabled={busy === r.from} onclick={() => accept(r)}>
              <Icon name="check" size={13} /> Accept
            </button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</Modal>

<style>
  .empty {
    font-size: var(--fs-ui);
    line-height: 1.5;
    margin: 0;
  }
  .intro {
    font-size: var(--fs-small);
    line-height: 1.5;
    margin: 0 0 10px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    flex-wrap: wrap;
  }
  .who {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .who strong {
    font-size: var(--fs-ui);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* The fingerprint is one unbroken token, so without this it wraps to three
     lines and squeezes the name beside it down to an ellipsis. */
  .mono {
    font-family: var(--mono, monospace);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tiny {
    font-size: var(--fs-small);
  }
  .acts {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }
  .acts button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 6px 11px;
    font-size: var(--fs-ui);
    border-radius: var(--radius-sm);
  }
  .acts .ghost {
    background: var(--bg-3);
    color: var(--text);
  }
  .acts .ghost:hover,
  .acts .ghost:active {
    background: var(--bg-2);
  }
  .acts .danger:hover,
  .acts .danger:active {
    background: var(--danger);
    color: var(--danger-fg);
  }
</style>
