<script>
  // Floating Discord-style profile card. Reads S.profilePopover ({fingerprint,
  // rect}); positions itself above the anchor (or below when there's no room),
  // clamped to the viewport. Rendered once at the app root so nothing clips it.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import GameShelf from "./GameShelf.svelte";
  import {
    S,
    activeGuild,
    memberByFpr,
    holdProfilePopover,
    scheduleCloseProfilePopover,
    closeProfilePopover,
    popoverJustOpened,
    refreshRightPanel,
    startDM,
    flash,
    roleColorFor,
    isBlocked,
    blockUser,
    unblockUser,
    openContextMenu,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PERM, PERM_ALL, has } from "./lib/perms.js";
  import { splitStatus } from "./lib/presence.js";
  import Banner from "./Banner.svelte";

  let dmText = $state("");
  let dmBusy = $state(false);

  // Nickname editing (self, in a real guild — not a DM).
  let editingNick = $state(false);
  let nickText = $state("");
  let nickBusy = $state(false);
  // You can always rename yourself in a guild. A moderator with MANAGE_MEMBERS
  // can rename anyone they outrank — never the owner (Discord's rule, and the
  // one every peer re-checks when the change arrives).
  const canNick = $derived.by(() => {
    const g = activeGuild();
    if (!mem || !S.activeGuildId || g?.kind === "dm") return false;
    if (mem.isSelf) return true;
    return !!g?.canManage && !mem.isOwner;
  });

  function startEditNick() {
    nickText = mem?.username ? mem.name : "";
    editingNick = true;
  }

  async function saveNick(e) {
    e?.preventDefault();
    if (nickBusy) return;
    nickBusy = true;
    try {
      if (mem.isSelf) await api.setNickname(S.activeGuildId, nickText.trim());
      else await api.setMemberNickname(S.activeGuildId, mem.fingerprint, nickText.trim());
      await refreshRightPanel();
      editingNick = false;
      flash(nickText.trim() ? "Nickname set" : "Nickname cleared", "success");
    } catch (err) {
      flash(err);
    } finally {
      nickBusy = false;
    }
  }

  async function sendDM(e) {
    e?.preventDefault();
    if (dmBusy) return;
    dmBusy = true;
    const text = dmText;
    dmText = "";
    try {
      await startDM(mem.fingerprint, text);
      closeProfilePopover();
    } catch (err) {
      dmText = text;
      flash(err);
    } finally {
      dmBusy = false;
    }
  }

  const mem = $derived(S.profilePopover ? memberByFpr(S.profilePopover.fingerprint) : null);
  // Clear per-person editor state when the card switches to a different person.
  $effect(() => {
    S.profilePopover?.fingerprint;
    dmText = "";
    editingNick = false;
    copied = false;
    revealFpr = false;
  });

  // Game collection lives in GameShelf; saving re-announces the profile.
  async function saveGames(next) {
    await api.setGames(next);
    S.identity = await api.identity();
    await refreshRightPanel();
  }

  let card = $state(null);
  let pos = $state(null); // {left, top} once measured

  // Measure then place: above the anchor if it fits, else below; clamp to
  // the viewport with an 8px margin. (Desktop only — the mobile presentation
  // is a bottom sheet and ignores the anchor entirely.)
  $effect(() => {
    const pop = S.profilePopover;
    if (!pop || !card || S.isMobile) {
      pos = null;
      return;
    }
    const cw = card.offsetWidth;
    const ch = card.offsetHeight;
    let left = pop.rect.x + pop.rect.w / 2 - cw / 2;
    left = Math.max(8, Math.min(left, window.innerWidth - cw - 8));
    let top = pop.rect.y - ch - 8;
    if (top < 8) top = pop.rect.y + pop.rect.h + 8;
    top = Math.min(top, window.innerHeight - ch - 8);
    pos = { left, top };
  });

  async function verify() {
    try {
      await api.verifyFingerprint(mem.fingerprint);
      await refreshRightPanel();
      flash("Member verified ✓", "success");
    } catch (err) {
      flash(err);
    }
  }

  // ---- moderation & roles ----
  // Controls show for a non-self, non-owner member in a real guild.
  const canModerate = $derived(
    !!mem && !mem.isSelf && !mem.isOwner && activeGuild()?.kind !== "dm" && !!activeGuild()?.canManage,
  );
  // Role assignment needs the Manage Roles permission (or owner).
  // Role assignment works on ANY member, including yourself: if you can manage
  // roles you can already grant them to others, so hiding your own card just
  // made it impossible to give yourself a role (e.g. the owner taking the
  // Admin badge). The guild owner keeps full authority regardless of roles.
  const canAssignRoles = $derived(
    !!mem &&
      activeGuild()?.kind !== "dm" &&
      (has(activeGuild()?.myPerms || 0, PERM.MANAGE_ROLES) || !!activeGuild()?.isOwner),
  );

  // One-click admin: Discord makes you hand-build a role first; here, "Make
  // admin" finds (or creates) an Admin role with every permission and assigns
  // it. Owner or Manage Roles only.
  const adminRole = $derived(S.roles.find((r) => r.perms === PERM_ALL));
  const isAdmin = $derived(!!adminRole && !!mem?.roleIds?.includes(adminRole.id));

  const blocked = $derived(isBlocked(mem?.fingerprint));
  function toggleBlock() {
    if (!mem) return;
    if (blocked) unblockUser(mem.fingerprint, mem.name);
    else blockUser(mem.fingerprint, mem.name);
    closeProfilePopover();
  }
  // Only offer "Make admin" to someone whose op would actually stick: the
  // guild's governance refuses a role granting MORE than the actor holds, so
  // a plain moderator clicking this would just watch it vanish. Owner, or a
  // member who already holds every permission.
  const canMakeAdmin = $derived(
    !!mem &&
      activeGuild()?.kind !== "dm" &&
      (!!activeGuild()?.isOwner || has(activeGuild()?.myPerms || 0, PERM_ALL)),
  );

  async function toggleAdmin() {
    // Capture the intent BEFORE refreshRightPanel() — that refresh updates
    // mem.roleIds, which recomputes the isAdmin $derived to the NEW state, so
    // reading isAdmin afterwards inverted the toast ("Admin removed" on grant).
    const grant = !isAdmin;
    try {
      let role = adminRole;
      if (!role) {
        await api.upsertRole(S.activeGuildId, "", "Admin", "#e0a63c", PERM_ALL, 100);
        S.roles = (await api.roles(S.activeGuildId)) || [];
        role = S.roles.find((r) => r.perms === PERM_ALL);
      }
      if (!role) throw new Error("couldn't create the Admin role");
      await api.assignRole(S.activeGuildId, mem.fingerprint, role.id, grant);
      await refreshRightPanel();
      flash(grant ? `${mem.name || "Member"} is now an admin 👑` : "Admin removed", "success");
    } catch (err) {
      flash(err);
    }
  }

  async function toggleRole(role) {
    const hasRole = mem.roleIds?.includes(role.id);
    try {
      await api.assignRole(S.activeGuildId, mem.fingerprint, role.id, !hasRole);
      await refreshRightPanel();
    } catch (err) {
      flash(err);
    }
  }

  const canMute = $derived(canModerate && has(activeGuild()?.myPerms || 0, PERM.MUTE_MEMBERS));
  const isMuted = $derived(!!mem && mem.mutedUntil > Date.now() / 1000);

  async function toggleMute() {
    try {
      if (isMuted) await api.unmuteMember(S.activeGuildId, mem.fingerprint);
      else await api.muteMember(S.activeGuildId, mem.fingerprint, 10);
      await refreshRightPanel();
      flash(isMuted ? "Unmuted" : "Muted for 10 min");
    } catch (err) {
      flash(err);
    }
  }

  function kick() {
    const fpr = mem.fingerprint;
    const name = mem.name || "this member";
    S.modal = {
      kind: "confirm",
      title: `Kick ${name}?`,
      body: "They lose access now but can rejoin with a new invite.",
      confirmLabel: "Kick",
      onConfirm: async () => {
        try {
          await api.removeMember(S.activeGuildId, fpr);
          await refreshRightPanel();
          flash("Member kicked");
        } catch (err) {
          flash(err);
        }
        S.modal = null;
      },
    };
    closeProfilePopover();
  }

  function ban() {
    const fpr = mem.fingerprint;
    const name = mem.name || "this member";
    S.modal = {
      kind: "confirm",
      title: `Ban ${name}?`,
      body: "They're removed now and can't rejoin, even with a new invite.",
      confirmLabel: "Ban",
      onConfirm: async () => {
        try {
          await api.banMember(S.activeGuildId, fpr);
          await refreshRightPanel();
          flash("Member banned");
        } catch (err) {
          flash(err);
        }
        S.modal = null;
      },
    };
    closeProfilePopover();
  }

  // Fingerprints already arrive space-grouped; re-chunking the raw string
  // counted those spaces as characters and produced "YXNO  YAD U MX G3" — and
  // this is the string two people read aloud to each other to verify a key.
  const fprShort = $derived(
    mem ? mem.fingerprint.replace(/\s+/g, "").replace(/(.{4})/g, "$1 ").trim() : "",
  );

  // Custom status split into emoji + text so each part can be styled.
  const statusParts = $derived(mem ? splitStatus(mem.status) : { emoji: "", text: "" });
  // When the member has no manual status, the backend fills status with the
  // "Artist — Title" line for old clients — don't render that twice here.
  const activityLine = $derived.by(() => {
    const a = mem?.activity;
    if (!a) return "";
    return a.artist ? `${a.artist} — ${a.title}` : a.title;
  });

  // Now-playing progress ticks locally: position snapshot + wall time since it
  // was taken, capped at the track length. One 1s interval, only while a card
  // with a duration is actually showing.
  let nowMs = $state(Date.now());
  $effect(() => {
    if (!mem?.activity?.durationMs) return;
    const id = setInterval(() => (nowMs = Date.now()), 1000);
    return () => clearInterval(id);
  });
  const actPosMs = $derived.by(() => {
    const a = mem?.activity;
    if (!a) return 0;
    const pos = (a.positionMs || 0) + (a.atMs ? nowMs - a.atMs : 0);
    return Math.max(0, a.durationMs ? Math.min(pos, a.durationMs) : pos);
  });
  const actPct = $derived(mem?.activity?.durationMs ? (actPosMs / mem.activity.durationMs) * 100 : 0);

  function fmtTime(ms) {
    const s = Math.floor(ms / 1000);
    const m = Math.floor(s / 60);
    return `${m}:${String(s % 60).padStart(2, "0")}`;
  }

  // The member's roles as read-only pills (managers get the toggle UI instead,
  // which already shows membership state).
  const memberRoles = $derived(mem ? S.roles.filter((r) => mem.roleIds?.includes(r.id)) : []);

  // Best-effort "also in": peer DMs carry the other side's fingerprint, so
  // shared DMs are cheap to count. Guild member lists other than the active
  // guild's aren't loaded, so mutual guilds are skipped silently.
  const sharedDMs = $derived(
    mem && !mem.isSelf
      ? S.guilds.filter(
          (g) => g.kind === "dm" && g.dmPeer === mem.fingerprint && g.id !== S.activeGuildId,
        ).length
      : 0,
  );

  // The safety number stays off the resting card (it dominated the layout) but
  // is one tap away — NOT in the overflow menu, because revealing it is the
  // start of the app's only identity check and shouldn't hide behind chrome.
  let revealFpr = $state(false);

  // Secondary and destructive actions live behind ⋯ — the card keeps chat,
  // identity and roles. Items snapshot nothing: the card is held open while
  // the menu is up (see the window pointerdown guard), so handlers can keep
  // reading `mem` live exactly as the old inline buttons did.
  const hasOverflow = $derived(
    !!mem && (canNick || canMakeAdmin || canMute || canModerate || !mem.isSelf),
  );
  function openOverflow(e) {
    openContextMenu(
      e,
      [
        canNick && {
          label: mem.isSelf
            ? mem.username
              ? "Change guild nickname"
              : "Set guild nickname"
            : mem.username
              ? "Change their nickname"
              : "Give them a nickname",
          icon: "edit",
          onClick: startEditNick,
        },
        { sep: true },
        canMakeAdmin && {
          label: isAdmin ? "Remove admin" : "Make admin",
          icon: "spark",
          onClick: toggleAdmin,
        },
        canMute && {
          label: isMuted ? "Unmute" : "Mute 10m",
          icon: isMuted ? "micOff" : "mic",
          onClick: toggleMute,
        },
        { sep: true },
        canModerate && { label: "Kick", icon: "door", onClick: kick },
        canModerate && { label: "Ban", icon: "trash", danger: true, onClick: ban },
        !mem.isSelf && {
          label: blocked ? "Unblock" : "Block",
          icon: "lock",
          danger: !blocked,
          onClick: toggleBlock,
        },
      ],
      { title: mem.name || "Member" },
    );
  }

  // Copy the full fingerprint; the button flips to "Copied" briefly.
  let copied = $state(false);
  let copyTimer;
  async function copyFpr() {
    const full = mem.fingerprint;
    try {
      await navigator.clipboard.writeText(full);
    } catch {
      // Clipboard API unavailable (e.g. insecure context): textarea fallback.
      const ta = document.createElement("textarea");
      ta.value = full;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      try {
        document.execCommand("copy");
      } finally {
        ta.remove();
      }
    }
    copied = true;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copied = false), 1600);
  }
</script>

<!-- The ⋯ menu renders at the app root, outside .pop, so while it's up every
     interaction with it (its backdrop, its sheet, an item) must not count as
     "clicked outside the card" — the menu's actions read the card's member.
     Escape likewise closes only the menu first; this component's listener was
     registered before ContextMenu's, so S.contextMenu is still set here. -->
<svelte:window
  onscroll={closeProfilePopover}
  onresize={closeProfilePopover}
  onkeydown={(e) => e.key === "Escape" && !S.contextMenu && closeProfilePopover()}
  onpointerdown={(e) => {
    if (!S.profilePopover || popoverJustOpened()) return;
    if (e.target.closest(".pop, .cm, .cm-backdrop, .bs-sheet, .bs-scrim")) return;
    closeProfilePopover();
  }}
/>

{#if S.profilePopover && mem}
  {#if S.isMobile}
    <!-- Sheet presentation gets a dimming scrim; tap it to dismiss. -->
    <button class="pp-scrim" onclick={closeProfilePopover} aria-label="Close profile"></button>
  {/if}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="pop {mem.effect ? `card-effect-${mem.effect}` : ''}"
    class:sheet={S.isMobile}
    bind:this={card}
    style={S.isMobile ? "" : pos ? `left:${pos.left}px;top:${pos.top}px` : "opacity:0;pointer-events:none"}
    role="dialog"
    aria-label="{mem.name || 'Member'} profile"
    onmouseenter={holdProfilePopover}
    onmouseleave={() => !S.contextMenu && scheduleCloseProfilePopover()}
  >
    <!-- Banner: a live preset scene wins, then an uploaded image, then the
         member's two theme colors as a gradient. It's tall, and the avatar
         straddles its bottom edge — the card is art, not empty background. -->
    <Banner
      banner={mem.banner}
      color={mem.color}
      color2={mem.color2}
      style={mem.style}
      class="banner"
    />
    {#if S.isMobile}
      <!-- Every other sheet in the app has a grip; without one this card read
           as a stuck panel whose only exit was a blind tap outside it. Tapping
           it closes, so the affordance is real and not just decoration. It
           rides on the banner art, so it's over-image chrome (light pill, dark
           shadow) rather than a themed surface. -->
      <button class="grip" onclick={closeProfilePopover} aria-label="Close profile"></button>
    {/if}
    {#if hasOverflow}
      <!-- Discord-style overflow: moderation, nickname and block live here so
           the resting card stays about the person. Rides the banner art, so
           over-image chrome like the grip, not a themed surface. -->
      <button class="more-btn" onclick={openOverflow} aria-label="More options" title="More options">
        <Icon name="dots" size={16} />
      </button>
    {/if}
    <div class="head">
      <div class="av-wrap">
        {#if mem.color}
          <!-- A soft breathing halo in the member's theme colors. -->
          <span
            class="av-glow"
            style={`background:radial-gradient(circle, ${mem.color2 || mem.color} 0%, transparent 70%)`}
            aria-hidden="true"
          ></span>
        {/if}
        <Avatar
          name={mem.name || mem.fingerprint}
          emoji={mem.emoji}
          color={mem.color}
          image={mem.avatar}
          size={72}
          online={mem.online}
          presence={mem.presence}
          frame={mem.frame}
          style={mem.style}
          color2={mem.color2}
        />
      </div>
      {#if mem.verified && !mem.isSelf}
        <span class="verified" title="You've verified this identity">
          <Icon name="check" size={12} /> Verified
        </span>
      {/if}
    </div>

    <div class="body">
      <div class="name-row">
        <strong style={roleColorFor(mem.fingerprint) ? `color:${roleColorFor(mem.fingerprint)}` : ""}
          >{mem.name || mem.fingerprint.slice(0, 9)}</strong
        >
        {#if mem.isSelf}<span class="tag">you</span>{/if}
        {#if mem.isOwner}
          <span class="role-badge owner" title="Guild owner">owner</span>
        {:else if mem.canManage}
          <span class="role-badge mod" title="Can manage members">mod</span>
        {/if}
      </div>
      {#if mem.username}<div class="username muted">{mem.username}</div>{/if}
      {#if sharedDMs > 0}
        <div class="mutual muted">
          <Icon name="members" size={12} />
          {sharedDMs === 1 ? "1 shared DM" : `${sharedDMs} shared DMs`}
        </div>
      {/if}

      {#if (statusParts.emoji || statusParts.text) && !(mem.activity && statusParts.text === activityLine)}
        <div class="status-box">
          {#if statusParts.emoji}<span class="status-emoji">{statusParts.emoji}</span>{/if}
          {#if statusParts.text}<span class="status-text">{statusParts.text}</span>{/if}
        </div>
      {/if}
      {#if mem.activity}
        <!-- Rich presence: live now-playing card (progress extrapolated locally
             from the broadcast position snapshot — no network ticking). Shown
             alongside the manual status, never instead of it. -->
        <div class="activity">
          <div class="act-label">
            <Icon name="speaker" size={12} /> Listening to music
          </div>
          <div class="act-row">
            <!-- Art renders by default: the backend only admits allowlisted
                 music-CDN URLs into profiles, so no arbitrary-host IP leak. -->
            {#if mem.activity.artUrl}
              <img class="act-art" src={mem.activity.artUrl} alt="" />
            {:else}
              <span class="act-art ph">🎵</span>
            {/if}
            <div class="act-meta">
              <div class="act-title" title={mem.activity.title}>{mem.activity.title}</div>
              {#if mem.activity.artist}<div class="act-artist">{mem.activity.artist}</div>{/if}
              {#if mem.activity.durationMs > 0}
                <div class="act-progress">
                  <div class="act-bar" style="width:{actPct}%"></div>
                </div>
                <div class="act-times">
                  <span>{fmtTime(actPosMs)}</span>
                  <span>{fmtTime(mem.activity.durationMs)}</span>
                </div>
              {/if}
            </div>
          </div>
        </div>
      {/if}

      {#if mem.bio}
        <div class="divider"></div>
        <div class="sec-label muted">About me</div>
        <div class="bio">{mem.bio}</div>
      {/if}

      {#if (mem.games || []).length || mem.isSelf}
        <div class="divider"></div>
        <GameShelf games={mem.games || []} editable={mem.isSelf} onchange={saveGames} />
      {/if}

      {#if canNick && editingNick}
        <!-- Reached from the ⋯ menu; only the live editor earns card space. -->
        <form class="nick-box" onsubmit={saveNick}>
          <input
            bind:value={nickText}
            placeholder="Nickname for this guild"
            maxlength="64"
            disabled={nickBusy}
          />
          <button type="submit" class="nick-save" disabled={nickBusy}>Save</button>
        </form>
      {/if}

      {#if canAssignRoles && S.roles.length}
        <div class="divider"></div>
        <div class="sec-label muted">Roles</div>
        <div class="role-toggles">
          {#each S.roles as role (role.id)}
            <button
              class="role-toggle"
              class:on={mem.roleIds?.includes(role.id)}
              onclick={() => toggleRole(role)}
            >
              <span class="role-dot" style="background:{role.color || 'var(--text-faint)'}"></span>
              {role.name}
              {#if mem.roleIds?.includes(role.id)}<span class="role-x">×</span>{/if}
            </button>
          {/each}
        </div>
      {:else if memberRoles.length}
        <div class="divider"></div>
        <div class="sec-label muted">Roles</div>
        <div class="role-pills">
          {#each memberRoles as role (role.id)}
            <span class="role-pill">
              <span class="role-dot" style="background:{role.color || 'var(--text-faint)'}"></span>
              {role.name}
            </span>
          {/each}
        </div>
      {/if}

      <div class="divider"></div>

      <div class="sec-head">
        <span class="sec-label muted">Safety number</span>
        {#if revealFpr}
          <button
            class="copy-btn"
            class:copied
            onclick={copyFpr}
            title="Copy the full safety number"
            aria-label="Copy safety number"
          >
            <Icon name={copied ? "check" : "copy"} size={11} />
            {copied ? "Copied" : "Copy"}
          </button>
        {:else}
          <!-- title carries the number, so a desktop hover peeks it — but the
               button is the real route: hover doesn't exist on a phone. -->
          <button
            class="copy-btn"
            onclick={() => (revealFpr = true)}
            title={fprShort}
            aria-label="Show safety number"
          >
            <Icon name="eye" size={11} /> Show
          </button>
        {/if}
      </div>
      {#if revealFpr}
        <code class="fpr">{fprShort}</code>
        {#if mem.isSelf}
          <p class="hint muted">Others confirm it's really you by comparing this out-of-band.</p>
        {:else if mem.verified}
          <p class="hint muted">You've verified this fingerprint — no one can impersonate them.</p>
        {:else}
          <p class="hint muted">Compare this with them over a call; if it matches, verify.</p>
          <!-- Verify lives behind the reveal on purpose: you cannot honestly
               confirm a number you haven't looked at. -->
          <button class="verify-btn" onclick={verify}>Verify identity</button>
        {/if}
      {/if}

      {#if mem.isSelf}
        <!-- Your own card is now what the footer opens, so the way to change
             what's on it has to live here. -->
        <button
          class="verify-btn"
          onclick={() => {
            closeProfilePopover();
            S.modal = { kind: "profile" };
          }}>Edit profile</button
        >
      {/if}

      {#if !mem.isSelf}
        <form class="dm-box" onsubmit={sendDM}>
          <input
            bind:value={dmText}
            placeholder="Message @{mem.name || 'them'}"
            disabled={dmBusy}
          />
          <button type="submit" class="dm-send" disabled={dmBusy} aria-label="Send message">
            <Icon name={dmBusy ? "spark" : "reply"} size={15} />
          </button>
        </form>
      {/if}

    </div>
  </div>
{/if}

<style>
  .pop {
    position: fixed;
    z-index: 250;
    width: 272px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
    animation: pop-in 0.12s ease;
  }
  @keyframes pop-in {
    from {
      opacity: 0;
      transform: translateY(4px) scale(0.98);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pop {
      animation: none;
    }
  }
  /* ---- mobile: the same card as a bottom sheet ---- */
  .pp-scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    z-index: 400;
    border: none;
    animation: pp-fade 0.16s ease;
  }
  @keyframes pp-fade {
    from {
      opacity: 0;
    }
  }
  .pop.sheet {
    left: 0;
    right: 0;
    top: auto;
    bottom: 0;
    width: auto;
    /* dvh: the nickname and DM fields open the keyboard, and vh does not shrink
       for it in an Android WebView — the card kept sizing to the whole screen
       and put its own input underneath. */
    max-height: 84dvh;
    overflow-y: auto;
    overscroll-behavior: contain;
    -webkit-overflow-scrolling: touch;
    z-index: 401;
    border: none;
    border-radius: 16px 16px 0 0;
    padding-bottom: var(--safe-bottom);
    animation: pp-up 0.22s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  @keyframes pp-up {
    from {
      transform: translateY(100%);
    }
  }
  .pop.sheet :global(.banner) {
    height: 130px;
  }
  /* 44px of tap area around a 40×5 pill — same pill as Modal's sheets. */
  .grip {
    position: absolute;
    top: 4px;
    left: 50%;
    transform: translateX(-50%);
    width: 64px;
    height: 44px;
    padding: 0;
    background: transparent;
    border: none;
    z-index: 1;
  }
  .grip::before {
    content: "";
    position: absolute;
    top: 6px;
    left: 12px;
    width: 40px;
    height: 5px;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.55);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.45);
  }
  .pop.sheet .body {
    padding: 8px 18px 18px;
    gap: 6px;
  }
  .pop.sheet .name-row strong {
    font-size: var(--fs-title);
  }
  .pop.sheet .bio,
  .pop.sheet .status-text {
    font-size: var(--fs-ui);
  }
  /* A safety number is compared glyph by glyph against another device. It is
     the string in the app that least tolerates being squinted at. */
  .pop.sheet .fpr {
    font-size: var(--fs-body);
  }
  /* 16px inputs: stops iOS auto-zoom on focus, and finger-sized buttons. */
  .pop.sheet input {
    font-size: 16px;
    padding: 10px 12px;
  }
  .pop.sheet .verify-btn {
    min-height: 44px;
    font-size: 14px;
  }
  /* The copy control sits inline with the safety number and can't grow without
     pushing that block around, so pad the tap area instead. */
  .pop.sheet .copy-btn {
    position: relative;
  }
  .pop.sheet .copy-btn::after {
    content: "";
    position: absolute;
    inset: -13px -4px;
  }
  .pop.sheet .dm-send {
    min-width: 48px;
  }
  .pop :global(.banner) {
    height: 112px;
  }
  /* Over-image chrome (like the grip): a dark disc so it survives any banner.
     Visually 30px; the ::after squares the hit box off at --tap-min. */
  .more-btn {
    position: absolute;
    top: 10px;
    right: 10px;
    z-index: 1;
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    padding: 0;
    border: none;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.4);
    color: #fff;
  }
  .more-btn::after {
    content: "";
    position: absolute;
    inset: calc((var(--tap-min) - 100%) / -2);
  }
  .more-btn:hover {
    background: rgba(0, 0, 0, 0.6);
  }
  .head {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    padding: 0 14px;
  }
  .av-wrap {
    position: relative;
    width: fit-content;
    margin-top: -38px;
    padding: 3px;
    background: var(--bg-1);
    border-radius: 50%;
    z-index: 1;
  }
  /* Breathing theme-colored halo behind the avatar — pure delight, no data. */
  .av-glow {
    position: absolute;
    inset: -12px;
    border-radius: 50%;
    opacity: 0.35;
    filter: blur(10px);
    animation: av-breathe 4s ease-in-out infinite;
    pointer-events: none;
  }
  @keyframes av-breathe {
    0%,
    100% {
      opacity: 0.22;
      transform: scale(0.94);
    }
    50% {
      opacity: 0.42;
      transform: scale(1.05);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .av-glow {
      animation: none;
      opacity: 0.25;
    }
  }
  .verified {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-bottom: 4px;
    padding: 2px 9px;
    font-size: var(--fs-small);
    font-weight: 600;
    border-radius: 999px;
    color: var(--ok-text);
    background: color-mix(in srgb, var(--ok) 14%, transparent);
    border: 1px solid color-mix(in srgb, var(--ok) 35%, transparent);
  }
  .body {
    padding: 6px 14px 14px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .name-row {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .name-row strong {
    /* Content, not chrome — deliberately off the UI scale. The sheet raises it
       to --fs-title, where the card is the whole screen. */
    font-size: 16px;
  }
  .tag {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    background: var(--bg-3);
    color: var(--text-muted);
    padding: 1px 6px;
    border-radius: 8px;
  }
  .username {
    font-size: var(--fs-compact);
    margin-top: -2px;
  }
  .mutual {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-small);
  }
  .status-box {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    margin-top: 4px;
    padding: 7px 10px;
    background: var(--bg-0);
    border-radius: var(--radius-sm);
  }
  /* Now-playing card: art + track + a live progress bar. */
  .activity {
    margin-top: 4px;
    padding: 9px 10px 8px;
    background: linear-gradient(135deg, color-mix(in srgb, var(--accent) 14%, var(--bg-0)), var(--bg-0) 70%);
    border: 1px solid color-mix(in srgb, var(--accent) 22%, transparent);
    border-radius: var(--radius-sm);
  }
  .act-label {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-tiny);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--accent-hover);
    margin-bottom: 7px;
  }
  .act-row {
    display: flex;
    gap: 10px;
    align-items: center;
  }
  .act-art {
    width: 52px;
    height: 52px;
    border-radius: 8px;
    object-fit: cover;
    flex-shrink: 0;
    box-shadow: 0 2px 8px rgb(0 0 0 / 0.3);
  }
  .act-art.ph {
    display: grid;
    place-items: center;
    font-size: 22px;
    background: var(--bg-3);
  }
  .act-meta {
    flex: 1;
    min-width: 0;
  }
  .act-title {
    font-size: var(--fs-ui);
    font-weight: 600;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .act-artist {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .act-progress {
    margin-top: 6px;
    height: 4px;
    border-radius: 2px;
    background: color-mix(in srgb, var(--text-faint) 30%, transparent);
    overflow: hidden;
  }
  .act-bar {
    height: 100%;
    border-radius: 2px;
    background: var(--accent);
    transition: width 1s linear;
  }
  .act-times {
    display: flex;
    justify-content: space-between;
    font-size: var(--fs-tiny);
    font-variant-numeric: tabular-nums;
    color: var(--text-faint);
    margin-top: 3px;
  }
  .status-emoji {
    font-size: 16px;
    line-height: 1.25;
  }
  .status-text {
    font-size: var(--fs-ui);
    line-height: 1.4;
    color: var(--text);
    word-break: break-word;
  }
  .sec-label {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .sec-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .bio {
    font-size: var(--fs-compact);
    line-height: 1.45;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 120px;
    overflow-y: auto;
  }
  .role-badge {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 1px 6px;
    border-radius: 8px;
    font-weight: 600;
  }
  .role-badge.owner {
    background: color-mix(in srgb, var(--accent) 22%, transparent);
    color: var(--accent-hover);
  }
  .role-badge.mod {
    background: color-mix(in srgb, var(--ok) 20%, transparent);
    color: var(--ok-text);
  }
  .role-toggles,
  .role-pills {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    margin-top: 2px;
  }
  .role-toggle,
  .role-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 3px 9px;
    font-size: var(--fs-compact);
    background: var(--bg-3);
    color: var(--text-muted);
    border: 1px solid transparent;
    border-radius: 10px;
  }
  .role-pill {
    color: var(--text);
    border-color: var(--border);
    border-radius: 999px;
  }
  /* A clickable role chip should answer the pointer — the un-toggled state was
     visually inert on hover. */
  .role-toggle:hover {
    color: var(--text);
    border-color: var(--border);
  }
  .role-toggle.on {
    background: var(--bg-4, var(--bg-3));
    color: var(--text);
    border-color: var(--border);
  }
  .role-toggle.on:hover {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
  }
  .role-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
  }
  .role-x {
    color: var(--text-faint);
    font-size: 13px;
    line-height: 1;
  }
  .nick-box {
    display: flex;
    gap: 6px;
    margin-top: 6px;
  }
  .nick-box input {
    flex: 1;
    padding: 7px 9px;
    font-size: var(--fs-ui);
    border-radius: var(--radius-sm);
  }
  .nick-save {
    padding: 0 12px;
    font-size: var(--fs-ui);
  }
  .divider {
    height: 1px;
    background: var(--border);
    margin: 8px 0 4px;
  }
  .copy-btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 7px;
    font-size: var(--fs-small);
    background: var(--bg-3);
    color: var(--text-muted);
    border-radius: 8px;
  }
  .copy-btn:hover {
    color: var(--text);
  }
  .copy-btn.copied {
    color: var(--ok-text);
  }
  .fpr {
    font-family: ui-monospace, monospace;
    /* Revealed on demand now, so it can afford to be read-aloud legible
       instead of resting-layout small. */
    font-size: var(--fs-ui);
    letter-spacing: 0.04em;
    line-height: 1.5;
    word-break: break-word;
    background: var(--bg-0);
    border-radius: var(--radius-sm);
    padding: 6px 8px;
    color: var(--text);
  }
  .hint {
    font-size: var(--fs-small);
    line-height: 1.4;
    margin: 4px 0 0;
  }
  .verify-btn {
    margin-top: 8px;
    font-size: var(--fs-ui);
    padding: 7px;
  }
  .dm-box {
    display: flex;
    gap: 6px;
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border);
  }
  .dm-box input {
    flex: 1;
    padding: 8px 10px;
    font-size: var(--fs-ui);
    border-radius: var(--radius-sm);
  }
  .dm-send {
    padding: 0 12px;
    display: grid;
    place-items: center;
  }
</style>
