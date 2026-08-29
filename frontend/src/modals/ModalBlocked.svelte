<script>
  // The block list. The copy here has to say two things the old wording left
  // out, because both of them surprise people:
  //
  //   1. What blocking DOES. "They can't add you to DMs or guilds" described
  //      one of its jobs and skipped the one you actually pressed the button
  //      for — everything of theirs disappearing.
  //   2. WHERE the list lives. It is a plain local table: no sync record, no
  //      digest entry, not in the passphrase backup. Your phone does not know
  //      about it, and neither does a restored account. A privacy setting that
  //      silently fails to travel is worse than one that says it doesn't.
  import SettingsShell from "./SettingsShell.svelte";
  import Avatar from "../Avatar.svelte";
  import Icon from "../Icon.svelte";
  import { S, unblockUser, nameFor } from "../lib/state.svelte.js";

  let { onClose } = $props();
</script>

<SettingsShell title="Blocked users" {onClose}>
  {#if S.blocked.length === 0}
    <p class="muted empty">
      You haven't blocked anyone. Blocking hides everything the person posts —
      messages, forum threads, reactions on yours, their typing line, their
      moments, their name in a call — and stops them adding you to a DM or a
      guild. Open their profile and choose “Block”.
    </p>
  {:else}
    <p class="muted tiny intro">
      You don't see anything these people post, and they can't add you to a DM
      or a guild. Nothing is deleted: they stay in the guilds you share,
      everyone else still sees them, and unblocking brings it all back.
    </p>
  {/if}
  <p class="muted tiny scope">
    <Icon name="lock" size={12} />
    <span
      >This list is kept on this device only. Your linked devices each have
      their own, and it isn't included in your backup.</span
    >
  </p>
  {#if S.blocked.length > 0}
    <div class="list">
      {#each S.blocked as fpr (fpr)}
        <div class="row">
          <Avatar
            name={nameFor(fpr)}
            image={S.contacts.find((c) => c.fingerprint === fpr)?.avatar || ""}
            size={30}
          />
          <span class="who">
            <strong>{nameFor(fpr)}</strong>
            <span class="tiny muted mono">{fpr.slice(0, 12)}…</span>
          </span>
          <button class="unblock" onclick={() => unblockUser(fpr, nameFor(fpr))}>Unblock</button>
        </div>
      {/each}
    </div>
  {/if}
</SettingsShell>

<style>
  .empty {
    font-size: var(--fs-ui);
    line-height: 1.5;
    margin: 0;
  }
  .intro {
    font-size: var(--fs-small);
    margin: 0 0 10px;
    line-height: 1.5;
  }
  /* The scope line is the one sentence people get wrong, so it is set apart
     from the prose above it rather than added to the end of it. */
  .scope {
    display: flex;
    gap: 7px;
    align-items: flex-start;
    font-size: var(--fs-small);
    line-height: 1.5;
    margin: var(--sp-3) 0 0;
    padding: var(--sp-2) 10px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .scope :global(svg) {
    flex-shrink: 0;
    margin-top: 2px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: var(--sp-3);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
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
  .mono {
    font-family: var(--mono, monospace);
  }
  .tiny {
    font-size: var(--fs-small);
  }
  .unblock {
    flex-shrink: 0;
    padding: 6px 12px;
    background: var(--bg-3);
    color: var(--text);
    border-radius: var(--radius-sm);
    font-size: var(--fs-ui);
  }
  .unblock:hover,
  .unblock:active {
    background: var(--accent);
    color: var(--accent-fg);
  }
</style>
