<script>
  // The leftmost rail: a Concord "home" bubble (opens DMs), one bubble per DM
  // and per guild with unread badges, plus create/join.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { S, selectGuild, openDMs, guildUnread } from "./lib/state.svelte.js";

  const g = $derived(S.guilds.find((x) => x.id === S.activeGuildId) || null);
  const inDMs = $derived(g?.kind === "dm");
  const servers = $derived(S.guilds.filter((x) => x.kind !== "dm"));
  // DMs shown as their own bubbles, Notes first.
  const dms = $derived(
    S.guilds
      .filter((x) => x.kind === "dm")
      .sort((a, b) => (a.name === "Notes" ? -1 : b.name === "Notes" ? 1 : 0)),
  );

  const initials = (name) =>
    name
      .split(/\s+/)
      .map((w) => w[0])
      .join("")
      .slice(0, 2);
</script>

<nav class="rail" aria-label="Servers">
  <button
    class="pill home"
    class:active={inDMs}
    title="Direct messages"
    aria-label="Home / Direct messages"
    onclick={openDMs}
  >
    <Icon name="concorde" size={24} />
  </button>

  {#each dms as dm (dm.id)}
    {@const u = guildUnread(dm)}
    <button
      class="pill"
      class:active={dm.id === S.activeGuildId}
      title={dm.name === "Notes" ? "Notes" : dm.name}
      aria-label={dm.name}
      onclick={() => selectGuild(dm.id)}
    >
      {#if dm.name === "Notes"}
        <span class="face"><Icon name="edit" size={18} /></span>
      {:else}
        <Avatar name={dm.name} size={42} />
      {/if}
      {#if dm.id !== S.activeGuildId && u.count > 0}
        <span class="badge mention">{u.count > 99 ? "99+" : u.count}</span>
      {/if}
    </button>
  {/each}

  <div class="divider"></div>

  {#each servers as sv (sv.id)}
    {@const u = guildUnread(sv)}
    <button
      class="pill"
      class:active={sv.id === S.activeGuildId}
      title={sv.name}
      aria-label={sv.name}
      onclick={() => selectGuild(sv.id)}
    >
      <span class="face">{initials(sv.name)}</span>
      {#if sv.id !== S.activeGuildId && u.count > 0}
        <span class="badge" class:mention={u.mentions > 0}>{u.count > 99 ? "99+" : u.count}</span>
      {/if}
    </button>
  {/each}

  <button class="pill add" title="Create a guild" aria-label="Create a guild" onclick={() => (S.modal = { kind: "create" })}>
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
  .pill.home {
    color: var(--accent);
    margin-bottom: 2px;
  }
  .pill.home:hover {
    color: white;
    background: var(--accent);
  }
  .pill.home.active {
    background: var(--accent);
    color: white;
  }
  .divider {
    width: 28px;
    height: 2px;
    border-radius: 2px;
    background: var(--border);
    margin: 2px 0;
    flex-shrink: 0;
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
