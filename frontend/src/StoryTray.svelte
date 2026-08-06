<script>
  // The Moments tray: a horizontal row of author avatars above the member
  // list. Stories are guild-scoped and text-on-banner only (see
  // internal/app/story.go), so a tile is just its author wearing a ring — no
  // thumbnails to fetch, no blob machinery. The ring is a plain 2px arc of
  // its own: NOT AvatarRing, which is the cosmetic frame system a member
  // chooses for themselves — unseen/seen state must never fight with (or be
  // mistaken for) someone's decorative ring.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { S, activeGuild, memberByFpr } from "./lib/state.svelte.js";
  import { api, on } from "./lib/api.js";

  let stories = $state([]);

  async function load() {
    // Which guild this fetch is FOR, captured before the await — the same
    // staleness guard as refreshRightPanel: two loads overlap on a fast guild
    // switch and the slower one must not paint the guild you just left.
    const gid = S.activeGuildId;
    const g = activeGuild();
    if (!gid || !g || g.kind === "dm") {
      stories = [];
      return;
    }
    try {
      const list = (await api.guildStories(gid)) || [];
      if (S.activeGuildId !== gid) return;
      // Dedupe by id: an author posting to several guilds signs ONE story per
      // guild, but a re-fetch racing an update can momentarily hand back the
      // same record twice — a tray shows a story once, full stop.
      const seen = new Set();
      stories = list.filter((s) => !seen.has(s.id) && seen.add(s.id));
    } catch {
      /* locked or transport down — an empty tray, not an error state */
    }
  }

  // Reload on guild switch (the $effect tracks S.activeGuildId)…
  $effect(() => {
    S.activeGuildId;
    load();
  });

  // …and on change signals. "story" is the backend's stories-changed event
  // (guildId "" = the expiry sweep: recheck whatever guild is showing). The
  // window event is the LOCAL echo — your own post, the viewer marking things
  // seen — because markStorySeen never leaves this device, so no backend
  // event will ever repaint the rings for it.
  $effect(() => {
    const offStory = on("story", (u) => {
      if (!u?.guildId || u.guildId === S.activeGuildId) load();
    });
    const local = () => load();
    window.addEventListener("concord:stories-changed", local);
    return () => {
      offStory?.();
      window.removeEventListener("concord:stories-changed", local);
    };
  });

  // One tile per author (list arrives newest-first, so the first story we see
  // carries the author's freshest accent pair for the ring).
  const authors = $derived.by(() => {
    const by = new Map();
    for (const s of stories) {
      let a = by.get(s.author);
      if (!a) {
        a = { fpr: s.author, name: s.authorName, color1: s.color1, color2: s.color2, stories: [], unseen: false };
        by.set(s.author, a);
      }
      a.stories.push(s);
      if (!s.seen) a.unseen = true;
    }
    return [...by.values()];
  });

  function openViewer(fpr) {
    // The viewer plays each author's stories oldest-first (a story is a
    // moment in time; playback reads forward), continuing into the next
    // author's — tap an author, land on their first unseen story.
    const list = [];
    for (const a of authors) list.push(...[...a.stories].reverse());
    let start = list.findIndex((s) => s.author === fpr && !s.seen);
    if (start < 0) start = list.findIndex((s) => s.author === fpr);
    S.modal = { kind: "storyViewer", stories: list, start: Math.max(0, start) };
  }

  const g = $derived(activeGuild());
</script>

{#if g && g.kind !== "dm"}
  <div class="moments" role="group" aria-label="Moments">
    <!-- The panel's own idiom for a section: with no stories the whole thing
         collapses to this label plus the quiet + tile — a corner of the
         roster, not a floating Share blob. -->
    <div class="section-head">Moments</div>
    <div class="tray">
    <button
      class="tile"
      title="Share a moment"
      onclick={() => (S.modal = { kind: "storyCompose" })}
    >
      <span class="ring add"><span class="hole plus"><Icon name="plus" size={14} /></span></span>
      <span class="tlabel dim">Share</span>
    </button>
    {#each authors as a (a.fpr)}
      {@const mem = memberByFpr(a.fpr)}
      <button
        class="tile"
        title="{a.name || a.fpr.slice(0, 9)} — {a.stories.length} moment{a.stories.length === 1 ? '' : 's'}"
        onclick={() => openViewer(a.fpr)}
      >
        <!-- Colors are backend-gated #hex (story.go's color gate), safe for
             inline CSS — the same posture as profile colors everywhere else. -->
        <span
          class="ring"
          class:unseen={a.unseen}
          style={a.unseen
            ? `background:linear-gradient(135deg, ${a.color1 || "var(--accent)"}, ${a.color2 || a.color1 || "var(--accent-hover)"})`
            : ""}
        >
          <span class="hole">
            <Avatar
              name={a.name || a.fpr}
              emoji={mem?.emoji || ""}
              color={mem?.color || a.color1}
              image={mem?.avatar || ""}
              size={32}
            />
          </span>
        </span>
        <span class="tlabel" class:dim={!a.unseen}>{a.name || a.fpr.slice(0, 9)}</span>
      </button>
    {/each}
    </div>
  </div>
{/if}

<style>
  .moments {
    border-bottom: 1px solid var(--border);
    flex: none;
  }
  /* MemberPanel's .section-head, replicated (not shared — this component must
     stay droppable into the panel without reaching into its styles). Keep the
     two in step. */
  .section-head {
    text-transform: uppercase;
    font-size: var(--fs-tiny);
    letter-spacing: 0.07em;
    font-weight: 700;
    color: var(--text-muted);
    margin: 10px 8px 0;
  }
  .tray {
    display: flex;
    gap: 4px;
    padding: 2px 4px 6px;
    margin: 0 4px;
    overflow-x: auto;
    /* A fling that runs out of tray must not drag the member list sideways. */
    overscroll-behavior-x: contain;
    scrollbar-width: none;
  }
  .tray::-webkit-scrollbar {
    display: none;
  }
  .tile {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 3px;
    background: transparent;
    padding: 4px 2px;
    flex: none;
    width: 52px;
  }
  .tile:hover,
  .tile:active {
    background: transparent;
  }
  /* The 2px arc: gradient padding around a bg-colored hole. Seen stories fall
     back to the quiet --border grey — present, already read. Both boxes follow
     --avatar-radius so square-avatar theme packs get a square ring around
     their square face — the +4/+2 keeps the corners concentric through the
     padding, and 50% packs just clamp back to a circle. */
  .ring {
    border-radius: calc(var(--avatar-radius, 50%) + 4px);
    padding: 2px;
    background: var(--border);
    display: inline-grid;
    place-items: center;
  }
  .hole {
    border-radius: calc(var(--avatar-radius, 50%) + 2px);
    padding: 2px;
    background: var(--bg-1);
    display: inline-grid;
    place-items: center;
  }
  /* The + tile is an invitation, not a story: a ghost dashed outline in the
     avatar's own shape, no filled ring pretending to be unseen. */
  .hole.plus {
    /* 32px avatar + the hole's own 2px padding — the ghost tile must sit on
       exactly the story tiles' baseline. */
    width: 36px;
    height: 36px;
    color: var(--text-faint);
    background: transparent;
    border: 1px dashed var(--border);
    border-radius: var(--avatar-radius, 50%);
  }
  .tile:hover .hole.plus {
    color: var(--text);
    border-color: var(--text-faint);
  }
  .ring.add {
    background: transparent;
  }
  .tlabel {
    font-size: var(--fs-micro);
    color: var(--text);
    max-width: 50px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tlabel.dim {
    color: var(--text-muted);
  }
  /* Finger-sized tiles in the mobile members drawer. */
  @media (pointer: coarse) {
    .tile {
      width: 58px;
    }
  }
</style>
