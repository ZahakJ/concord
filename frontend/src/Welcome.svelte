<script>
  // The welcome (§8.3): what a new account, or anyone with no conversation
  // open, sees in the conversation column. The one empty-state pattern — the
  // app's own mark, a headline, a sentence, the two ways in — and below it two
  // quiet rows to the places you already have. Never shown inside a guild
  // (a guild with no channels has its own empty state, App.svelte).
  import Icon from "./Icon.svelte";
  import EmptyState from "./EmptyState.svelte";
  import { S, selectNotes } from "./lib/state.svelte.js";

  const hasGuild = $derived(S.guilds.some((g) => g.kind !== "dm"));
</script>

<div class="welcome">
  <div class="inner">
    <EmptyState
      icon="concorde"
      headline="Welcome to Concord"
      sub="A guild is a space of your own, kept on the devices of the people in it and readable by no one else. Start one, or join a friend's with the code they sent you."
    >
      {#snippet actions()}
        <button class="primary" onclick={() => (S.modal = { kind: "create" })}>Create a guild</button>
        <button class="quiet" onclick={() => (S.modal = { kind: "join", code: "" })}>Join with an invite code</button>
      {/snippet}
    </EmptyState>
    <div class="more">
      <button class="row" onclick={selectNotes}>
        <Icon name="edit" size={16} />
        <span class="lbl">Open your Notes</span>
        <span class="sub">a private scratchpad</span>
      </button>
      {#if hasGuild}
        <!-- Only once there is a guild to have plans in: a brand-new account's
             welcome should not open onto an empty calendar. -->
        <button class="row" onclick={() => (S.modal = { kind: "myCalendar" })}>
          <Icon name="calendar" size={16} />
          <span class="lbl">Your calendar</span>
          <span class="sub">what's coming up, across every guild</span>
        </button>
      {/if}
    </div>
  </div>
</div>

<style>
  .welcome {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    overflow-y: auto;
    padding: var(--sp-6) var(--sp-edge);
    background: var(--bg-2);
  }
  .inner {
    max-width: 560px;
  }
  .more {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 0 var(--sp-4);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 32px;
    padding: 0 var(--sp-2);
    margin-left: calc(-1 * var(--sp-2));
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--fs-ui);
    text-align: start;
  }
  .row :global(svg) {
    color: var(--text-muted);
    flex: none;
  }
  .sub {
    color: var(--text-muted);
    font-size: var(--fs-compact);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (pointer: fine) {
    .row:hover {
      background: var(--bg-3);
    }
  }
  .row:focus-visible {
    outline: var(--focus-ring);
    outline-offset: -2px;
  }
  @media (max-width: 768px) {
    .welcome {
      justify-content: flex-start;
      padding-top: var(--sp-5);
    }
    .row {
      min-height: 44px;
    }
  }
</style>
