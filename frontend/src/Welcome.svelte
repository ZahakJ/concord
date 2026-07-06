<script>
  // Shown in the main area when no channel is open (typically a brand-new
  // user with no servers yet). Turns an empty screen into a warm first run.
  import Icon from "./Icon.svelte";
  import { S, selectNotes } from "./lib/state.svelte.js";
</script>

<div class="welcome">
  <div class="badge"><Icon name="concorde" size={40} /></div>
  <h1>Welcome to Concord</h1>
  <p class="muted">
    Your community, on your machines, readable by no one else. Spin up a server, join a friend's
    with their invite code, or jot something in your private Notes.
  </p>
  <div class="cards">
    <button class="card" onclick={() => (S.modal = { kind: "create" })}>
      <span class="ic"><Icon name="plus" size={18} /></span>
      <strong>Create a server</strong>
      <span class="muted sub">Start a space for your friends</span>
    </button>
    <button class="card" onclick={() => (S.modal = { kind: "join", code: "" })}>
      <span class="ic"><Icon name="download" size={18} /></span>
      <strong>Join with an invite</strong>
      <span class="muted sub">Paste a code a friend sent you</span>
    </button>
    <button class="card" onclick={selectNotes}>
      <span class="ic"><Icon name="edit" size={18} /></span>
      <strong>Open your Notes</strong>
      <span class="muted sub">A private, encrypted scratchpad</span>
    </button>
  </div>
</div>

<style>
  .welcome {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 24px;
    gap: 10px;
    overflow-y: auto;
  }
  .badge {
    width: 76px;
    height: 76px;
    border-radius: 20px;
    display: grid;
    place-items: center;
    background: linear-gradient(120deg, var(--accent), var(--accent-hover));
    color: white;
    box-shadow: var(--shadow-pop);
  }
  h1 {
    margin: 8px 0 0;
    font-size: 22px;
  }
  p {
    max-width: 440px;
    line-height: 1.55;
    font-size: 14px;
    margin: 0;
  }
  .cards {
    display: flex;
    gap: 12px;
    margin-top: 18px;
    flex-wrap: wrap;
    justify-content: center;
  }
  .card {
    width: 190px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    padding: 16px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    color: var(--text);
    text-align: left;
    transition:
      transform 0.12s ease,
      border-color 0.12s ease;
  }
  .card:hover {
    background: var(--bg-1);
    border-color: var(--accent);
    transform: translateY(-2px);
  }
  .ic {
    width: 36px;
    height: 36px;
    border-radius: var(--radius-md);
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent-hover);
    margin-bottom: 6px;
  }
  .sub {
    font-size: 12px;
  }
  @media (prefers-reduced-motion: reduce) {
    .card:hover {
      transform: none;
    }
  }
</style>
