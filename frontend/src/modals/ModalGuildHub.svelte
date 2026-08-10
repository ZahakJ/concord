<script>
  // The guild hub: everything about ONE guild behind one door, Discord-style.
  // The channel-list header, the mobile sheet and the chat header's More menu
  // all used to scatter guild management across their own piles of entries;
  // they now open this instead. It's a MENU modal — rows grouped in sections,
  // each opening the real panel via openPanel(), so the back arrow always
  // returns here and the hub reads as one place you move through.
  //
  // Rows are permission-gated exactly the way their targets gate the op (the
  // same checks ChatHeader's menu used): what you can't do, you don't see.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Banner from "../Banner.svelte";
  import {
    S,
    activeGuild,
    refreshGuilds,
    selectGuild,
    flash,
    openPanel,
    modalNav,
  } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { PERM, has } from "../lib/perms.js";
  import { guildBannerArt } from "../lib/guildbanners.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  const art = $derived(guildBannerArt(g?.banner));
  const canRoles = $derived(!!g && (g.isOwner || has(g.myPerms, PERM.MANAGE_ROLES)));

  // The invite modal needs the code as a prop, and openPanel only carries
  // {kind, from} — so push the hub onto the stack the same way openPanel does
  // and hand the code along ourselves. Back still returns here.
  // Guild-wide transcript. The backend reads the store, so this is the whole
  // history rather than the pages the reader happens to have open.
  async function exportGuild() {
    try {
      const md = await api.exportMarkdown(S.activeGuildId, "");
      const a = document.createElement("a");
      a.href = URL.createObjectURL(new Blob([md], { type: "text/markdown" }));
      a.download = `${(g?.name || "guild").replace(/[^\w.-]+/g, "-")}-history.md`;
      a.click();
      URL.revokeObjectURL(a.href);
      flash("Guild history exported", "success");
    } catch (err) {
      flash(err);
    }
  }

  async function showInvite() {
    let code;
    try {
      code = await api.inviteCode(S.activeGuildId);
    } catch (err) {
      flash(err);
      return;
    }
    modalNav.dir = 1;
    S.modalStack = [...S.modalStack, S.modal];
    S.modal = { kind: "invite", code };
  }

  // Same confirm as ChatHeader's — copied, not imported: a cross-component
  // import for one payload would couple the header to this modal's lifetime.
  function confirmLeave() {
    if (!g) return;
    const verb = g.isOwner ? "Delete" : "Leave";
    S.modal = {
      kind: "confirm",
      title: `${verb} "${g.name}"?`,
      body: "Its messages will be removed from this device.",
      confirmLabel: verb,
      onConfirm: async () => {
        S.modal = null;
        await api.leaveGuild(g.id);
        S.activeGuildId = "";
        S.activeChannelId = "";
        S.messages = [];
        await refreshGuilds();
        if (S.guilds.length) selectGuild(S.guilds[0].id);
        flash(g.isOwner ? "Guild deleted" : "Left guild");
      },
    };
  }
</script>

<Modal title="Guild settings" wide {onClose}>
  {#if g}
    <!-- Hero: the guild's name and icon over its banner, laid out like the
         channel-list header paints it — same Banner component, same scrim, so
         the hub opens on exactly the identity every member's sidebar shows.
         A guild without a banner gets Banner's default accent gradient rather
         than a blank strip; the scrim keeps the name readable over either. -->
    <div class="hero" class:ink-dark={art?.ink === "dark"}>
      <Banner banner={art ? g.banner : ""} scrim={art?.ink || "light"} class="hub-art" />
      <span class="hero-row">
        {#if g.icon}
          <img class="hero-icon" src={g.icon} alt="" />
        {/if}
        <strong>{g.name}</strong>
      </span>
    </div>

    <!-- IDENTITY -->
    <section class="grp">
      <div class="sec-label">Identity</div>
      <div class="card">
        <button class="row" onclick={() => openPanel("guildSettings")}>
          <span class="chip"><Icon name="gear" size={17} /></span>
          <span class="row-text">
            <span class="row-title">Overview</span>
            <span class="row-sub">Name, icon, banner &amp; description</span>
          </span>
          <span class="chev">›</span>
        </button>
      </div>
    </section>

    <!-- PEOPLE -->
    {#if canRoles || g.canManage}
      <section class="grp">
        <div class="sec-label">People</div>
        <div class="card">
          {#if canRoles}
            <button class="row" onclick={() => openPanel("roles")}>
              <span class="chip"><Icon name="spark" size={17} /></span>
              <span class="row-text">
                <span class="row-title">Roles</span>
                <span class="row-sub">Permissions &amp; who can do what</span>
              </span>
              <span class="chev">›</span>
            </button>
          {/if}
          {#if g.canManage}
            <button class="row" onclick={() => openPanel("bans")}>
              <span class="chip"><Icon name="door" size={17} /></span>
              <span class="row-text">
                <span class="row-title">Banned members</span>
                <span class="row-sub">Review &amp; lift bans</span>
              </span>
              <span class="chev">›</span>
            </button>
            <button class="row" onclick={showInvite}>
              <span class="chip"><Icon name="members" size={17} /></span>
              <span class="row-text">
                <span class="row-title">Invite people</span>
                <span class="row-sub">Share a code that lets people in</span>
              </span>
              <span class="chev">›</span>
            </button>
          {/if}
        </div>
      </section>
    {/if}

    <!-- EXPRESSION -->
    <section class="grp">
      <div class="sec-label">Expression</div>
      <div class="card">
        <button class="row" onclick={() => openPanel("emoji")}>
          <span class="chip"><Icon name="smile" size={17} /></span>
          <span class="row-text">
            <span class="row-title">Guild emoji</span>
            <span class="row-sub">Custom emoji everyone here can use</span>
          </span>
          <span class="chev">›</span>
        </button>
        <button class="row" onclick={() => openPanel("stats")}>
          <span class="chip"><Icon name="poll" size={17} /></span>
          <span class="row-text">
            <span class="row-title">Stats</span>
            <span class="row-sub">Activity &amp; diagnostics for this guild</span>
          </span>
          <span class="chev">›</span>
        </button>
      </div>
    </section>

    <!-- EVENTS -->
    <section class="grp">
      <div class="sec-label">Events</div>
      <div class="card">
        <button class="row" onclick={() => openPanel("events")}>
          <span class="chip"><Icon name="calendar" size={17} /></span>
          <span class="row-text">
            <span class="row-title">Events &amp; calendar</span>
            <span class="row-sub">The crew's shared board — what's coming up</span>
          </span>
          <span class="chev">›</span>
        </button>
      </div>
    </section>

    <!-- DATA -->
    <section class="grp">
        <div class="sec-label">Data</div>
        <div class="card">
          <button class="row" onclick={exportGuild}>
            <span class="chip"><Icon name="download" size={17} /></span>
            <span class="row-text">
              <span class="row-title">Export this guild</span>
              <span class="row-sub">Every channel's full history as a Markdown transcript</span>
            </span>
            <span class="chev">›</span>
          </button>
          {#if g.canManage}
          <button class="row" onclick={() => openPanel("retention")}>
            <span class="chip"><Icon name="clock" size={17} /></span>
            <span class="row-text">
              <span class="row-title">Message history</span>
              <span class="row-sub">How long members keep messages before their copy prunes itself</span>
            </span>
            <span class="chev">›</span>
          </button>
          {/if}
        </div>
      </section>

    <!-- DANGER -->
    <section class="grp">
      <div class="sec-label">Danger zone</div>
      <div class="card danger-card">
        {#if g.isOwner}
          <!-- Not a dead-end button: transfer and heir live on the member row
               (MemberPanel's right-click), so this just points there. -->
          <div class="row note">
            <span class="chip danger-chip"><Icon name="crown" size={16} /></span>
            <span class="row-text">
              <span class="row-title">Ownership &amp; heir</span>
              <span class="row-sub">Right-click a member in the member list to transfer ownership or name an heir.</span>
            </span>
          </div>
        {/if}
        <button class="row danger" onclick={confirmLeave}>
          <span class="chip danger-chip"><Icon name={g.isOwner ? "trash" : "door"} size={16} /></span>
          <span class="row-text">
            <span class="row-title">{g.isOwner ? "Delete guild" : "Leave guild"}</span>
            <span class="row-sub">Its messages will be removed from this device.</span>
          </span>
          <span class="chev">›</span>
        </button>
      </div>
    </section>
  {/if}
</Modal>

<style>
  /* Hero: bleeds to the dialog's edges so the art reads as a header, not a
     thumbnail. Name pinned to the bottom edge like the channel-list header. */
  .hero {
    position: relative;
    margin: -4px -20px 0;
    min-height: 88px;
    display: flex;
    align-items: flex-end;
    padding: 12px 20px;
    color: #fff;
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  }
  /* The pale templates (Linen Press) ask for dark ink; Banner.svelte flips its
     scrim to match, so the pair stays readable together. */
  .hero.ink-dark {
    color: #12161a;
    text-shadow: 0 1px 2px rgba(255, 255, 255, 0.65);
  }
  .hero :global(.hub-art) {
    position: absolute;
    inset: 0;
  }
  /* The row sits ABOVE the art layer: the art is absolutely positioned, and
     positioned boxes paint over in-flow ones whatever the DOM order. */
  .hero-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .hero-row strong {
    font-size: var(--fs-title);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hero-icon {
    width: 30px;
    height: 30px;
    border-radius: 8px;
    object-fit: cover;
    flex-shrink: 0;
  }

  /* Sectioned, carded rows — the same structure ModalSettings uses, so the
     guild hub and the app hub read as siblings. */
  .grp {
    display: flex;
    flex-direction: column;
    gap: 7px;
    text-align: left;
    animation: grp-in 0.3s ease both;
  }
  /* Sections cascade in — a beat apart, settled fast. (The dialog's children
     start at the pinned sheet-top, then the hero, so the first section is
     nth-child(3).) */
  .grp:nth-child(4) {
    animation-delay: 0.04s;
  }
  .grp:nth-child(5) {
    animation-delay: 0.08s;
  }
  .grp:nth-child(6) {
    animation-delay: 0.12s;
  }
  .grp:nth-child(7) {
    animation-delay: 0.16s;
  }
  @keyframes grp-in {
    from {
      opacity: 0;
      transform: translateY(6px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .grp {
      animation: none;
    }
  }
  .sec-label {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding: 0 4px;
  }
  .card {
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  /* Hairlines between rows, inset past the icon chip like iOS/Telegram. */
  .card > .row + .row {
    border-top: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 52px;
    padding: 10px 14px;
    background: transparent;
    color: var(--text);
    text-align: left;
    border-radius: 0;
    transition: background 0.14s ease;
  }
  button.row:hover,
  button.row:active {
    background: var(--bg-3);
  }
  .row-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .row-title {
    font-size: var(--fs-ui);
    font-weight: 600;
    line-height: 1.3;
  }
  .row-sub {
    font-size: var(--fs-small);
    line-height: 1.45;
    color: var(--text-muted);
  }
  .chev {
    flex-shrink: 0;
    font-size: 20px;
    line-height: 1;
    color: var(--text-faint);
    transition:
      transform 0.15s ease,
      color 0.15s ease;
  }
  /* Nav chevrons drift toward the destination on hover. */
  .row:hover .chev {
    color: var(--text-muted);
    transform: translateX(2px);
  }
  /* Icon chips: soft accent-tinted circles. */
  .chip {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    flex-shrink: 0;
    border-radius: 10px;
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent-hover);
  }

  /* Danger zone: warning-tinted card, danger row hover, quiet note row. */
  .danger-card {
    border-color: color-mix(in srgb, var(--danger) 30%, var(--border));
    background: color-mix(in srgb, var(--danger) 4%, var(--bg-0));
  }
  .danger-chip {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    color: var(--danger-text);
  }
  .row.danger .row-title {
    color: var(--danger-text);
  }
  button.row.danger:hover,
  button.row.danger:active {
    background: var(--danger-soft);
  }
  /* The heir pointer is information, not a control — no hover answer. */
  .row.note {
    cursor: default;
  }

  /* Phone: rows get a touch more height — the finger-sized floor the other
     hubs use (Modal's global --tap-min covers the buttons; this covers the
     row rhythm). */
  @media (pointer: coarse), (max-width: 768px) {
    .row {
      min-height: 56px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .chev {
      transition: none;
    }
    .row:hover .chev {
      transform: none;
    }
  }
</style>
