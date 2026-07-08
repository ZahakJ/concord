<script>
  // The leftmost rail: a Concord "home" bubble (opens DMs), one bubble per DM
  // and per guild with unread badges, plus create/join.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { S, selectGuild, openDMs, guildUnread } from "./lib/state.svelte.js";

  const g = $derived(S.guilds.find((x) => x.id === S.activeGuildId) || null);
  const inDMs = $derived(g?.kind === "dm");
  const servers = $derived(S.guilds.filter((x) => x.kind !== "dm"));
  // DM bubbles surface only when they need attention: a DM with unread
  // messages. Everything else (incl. Notes) lives behind the home button.
  const dms = $derived(
    S.guilds.filter((x) => x.kind === "dm" && x.name !== "Notes" && guildUnread(x).count > 0),
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
  <div class="divider"></div>

  {#each dms as dm (dm.id)}
    {@const u = guildUnread(dm)}
    <button
      class="pill"
      class:active={dm.id === S.activeGuildId}
      title={dm.name}
      aria-label={dm.name}
      onclick={() => selectGuild(dm.id)}
    >
      <Avatar
        name={dm.name}
        image={dm.icon}
        size={42}
        online={dm.dmPeer ? !!dm.dmPeerOnline : null}
        presence={dm.dmPeerPresence || ""}
      />
      {#if dm.id !== S.activeGuildId && u.count > 0}
        <span class="badge mention">{u.count > 99 ? "99+" : u.count}</span>
      {/if}
    </button>
  {/each}

  {#if dms.length}<div class="divider"></div>{/if}

  {#each servers as sv (sv.id)}
    {@const u = guildUnread(sv)}
    <button
      class="pill"
      class:active={sv.id === S.activeGuildId}
      class:hasicon={sv.icon}
      title={sv.name}
      aria-label={sv.name}
      onclick={() => selectGuild(sv.id)}
    >
      {#if sv.icon}
        <img class="icon" src={sv.icon} alt="" />
      {:else}
        <span class="face">{initials(sv.name)}</span>
      {/if}
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
  /* The home bubble is deliberately not a server: a fixed rounded-square with a
     neutral surface, so it reads as "brand / home" rather than a guild. */
  .pill.home {
    border-radius: 15px;
    background: var(--bg-3);
    color: var(--accent);
  }
  .pill.home:hover {
    border-radius: 15px;
    color: white;
    background: var(--accent);
  }
  .pill.home.active {
    border-radius: 15px;
    background: var(--accent);
    color: white;
  }
  .pill .icon {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: inherit;
  }
  /* A pill showing a real icon keeps it edge-to-edge; drop the tinted bg. */
  .pill.hasicon {
    background: var(--bg-2);
    overflow: hidden;
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
