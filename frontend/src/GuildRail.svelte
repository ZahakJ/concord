<script>
  // The leftmost rail: one circle per guild, unread badges, create/join.
  import Icon from "./Icon.svelte";
  import { S, selectGuild, guildUnread } from "./lib/state.svelte.js";

  const initials = (name) =>
    name
      .split(/\s+/)
      .map((w) => w[0])
      .join("")
      .slice(0, 2);
</script>

<nav class="rail" aria-label="Servers">
  <div class="brand" title="Concord"><Icon name="diamond" size={20} /></div>

  {#each S.guilds as g (g.id)}
    {@const u = guildUnread(g)}
    <button
      class="pill"
      class:active={g.id === S.activeGuildId}
      title={g.name}
      aria-label={g.name}
      onclick={() => selectGuild(g.id)}
    >
      <span class="face">{initials(g.name)}</span>
      {#if g.id !== S.activeGuildId && u.count > 0}
        <span class="badge" class:mention={u.mentions > 0}>{u.count > 99 ? "99+" : u.count}</span>
      {/if}
    </button>
  {/each}

  <button class="pill add" title="Create a server" aria-label="Create a server" onclick={() => (S.modal = { kind: "create" })}>
    <Icon name="plus" />
  </button>
  <button class="pill add" title="Join with invite" aria-label="Join with invite" onclick={() => (S.modal = { kind: "join", code: "" })}>
    <Icon name="download" />
  </button>
</nav>

<style>
  .rail {
    background: var(--bg-0);
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 10px 0;
    overflow-y: auto;
    scrollbar-width: none;
  }
  .rail::-webkit-scrollbar {
    display: none;
  }
  .brand {
    color: var(--accent);
    padding: 4px 0 10px;
    border-bottom: 1px solid var(--border);
    width: 60%;
    display: grid;
    place-items: center;
    margin-bottom: 2px;
  }
  .pill {
    position: relative;
    width: 42px;
    height: 42px;
    border-radius: 50%;
    background: var(--bg-2);
    color: var(--text);
    font-weight: 600;
    font-size: 14px;
    text-transform: uppercase;
    display: grid;
    place-items: center;
    padding: 0;
    transition:
      border-radius 0.15s ease,
      background 0.15s ease;
    flex-shrink: 0;
  }
  .pill:hover {
    border-radius: 14px;
    background: var(--bg-3);
  }
  .pill.active {
    border-radius: 14px;
    background: var(--accent);
    color: white;
  }
  .pill.add {
    background: transparent;
    border: 1px dashed var(--border);
    color: var(--text-muted);
  }
  .pill.add:hover {
    color: var(--accent-hover);
    border-color: var(--accent-hover);
    background: var(--bg-2);
  }
  .badge {
    position: absolute;
    bottom: -3px;
    right: -3px;
    min-width: 17px;
    height: 17px;
    padding: 0 4px;
    border-radius: 9px;
    background: var(--text-faint);
    color: white;
    font-size: 10px;
    font-weight: 700;
    display: grid;
    place-items: center;
    border: 2px solid var(--bg-0);
  }
  .badge.mention {
    background: var(--danger);
  }
</style>
