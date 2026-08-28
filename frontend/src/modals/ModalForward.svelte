<script>
  // Pick a destination (any text channel or DM) to forward a message to.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, forwardMessage, jumpToChannel, flash } from "../lib/state.svelte.js";
  import { previewText } from "../lib/attachments.js";

  let { message, onClose } = $props();

  let query = $state("");
  let busy = $state(false);

  // Flatten every forwardable destination: text channels in guilds + DMs.
  const destinations = $derived.by(() => {
    const out = [];
    for (const g of S.guilds) {
      if (g.kind === "dm") {
        out.push({
          id: g.channels[0]?.id,
          label: g.name,
          sub: "DM",
          dm: true,
          guild: g,
          // Same face the DM list shows, so the picker rows match it.
          avatar: g.dmPeerAvatar || g.dmFaces?.[0]?.avatar || g.icon || "",
        });
      } else {
        for (const c of g.channels) {
          if (c.type === "voice") continue;
          out.push({ id: c.id, label: `#${c.name}`, sub: g.name, dm: false });
        }
      }
    }
    const q = query.trim().toLowerCase();
    // Drop destinations with no channel id (a channel-less DM) — they can't be
    // forwarded to anyway, and duplicate `undefined` keys crash the {#each}.
    const withId = out.filter((d) => d.id);
    return q
      ? withId.filter((d) => (d.label + " " + d.sub).toLowerCase().includes(q))
      : withId;
  });

  async function send(dest) {
    if (busy || !dest.id) return;
    busy = true;
    try {
      await forwardMessage(message, dest.id);
      await jumpToChannel(dest.id);
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title="Forward message" {onClose}>
  <div class="snippet">{previewText(message.content).slice(0, 140) || "(message)"}</div>
  <input class="search" bind:value={query} placeholder="Search channels and DMs…" />
  <div class="list scroll-fade">
    {#each destinations as d (d.id)}
      <button class="dest" disabled={busy} onclick={() => send(d)}>
        {#if d.dm}
          <Avatar name={d.label} image={d.avatar || ""} size={22} />
        {:else}
          <span class="hashicon"><Icon name="hash" size={13} /></span>
        {/if}
        <span class="dest-label">{d.label}</span>
        <span class="muted dest-sub">{d.sub}</span>
      </button>
    {:else}
      <div class="muted none">No matching channel</div>
    {/each}
  </div>
</Modal>

<style>
  .snippet {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    background: var(--bg-0);
    border-left: 3px solid var(--border);
    border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
    padding: 8px 10px;
    max-height: 60px;
    overflow: hidden;
  }
  .search {
    font-size: var(--fs-ui);
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 280px;
    overflow-y: auto;
  }
  /* One scroller per sheet — see Modal.svelte. */
  @media (pointer: coarse), (max-width: 768px) {
    .list {
      max-height: none;
      overflow-y: visible;
    }
  }
  .dest {
    display: flex;
    align-items: center;
    gap: 9px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 7px 8px;
    border-radius: var(--radius-sm);
  }
  .dest:hover,
  .dest:active {
    background: var(--bg-3);
  }
  .hashicon {
    width: 22px;
    display: grid;
    place-items: center;
    color: var(--text-muted);
  }
  .dest-label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dest-sub {
    font-size: var(--fs-compact);
  }
  .none {
    padding: var(--sp-3);
    font-size: var(--fs-ui);
    text-align: center;
  }
</style>
