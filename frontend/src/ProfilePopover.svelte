<script>
  // Floating Discord-style profile card. Reads S.profilePopover ({fingerprint,
  // rect}); positions itself above the anchor (or below when there's no room),
  // clamped to the viewport. Rendered once at the app root so nothing clips it.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
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
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PERM, has } from "./lib/perms.js";
  import { splitStatus } from "./lib/presence.js";

  let dmText = $state("");
  let dmBusy = $state(false);

  // Nickname editing (self, in a real guild — not a DM).
  let editingNick = $state(false);
  let nickText = $state("");
  let nickBusy = $state(false);
  const canNick = $derived(!!mem?.isSelf && activeGuild()?.kind !== "dm" && !!S.activeGuildId);

  function startEditNick() {
    nickText = mem?.username ? mem.name : "";
    editingNick = true;
  }

  async function saveNick(e) {
    e?.preventDefault();
    if (nickBusy) return;
    nickBusy = true;
    try {
      await api.setNickname(S.activeGuildId, nickText.trim());
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
  });

  let card = $state(null);
  let pos = $state(null); // {left, top} once measured

  // Measure then place: above the anchor if it fits, else below; clamp to
  // the viewport with an 8px margin.
  $effect(() => {
    const pop = S.profilePopover;
    if (!pop || !card) {
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
  const canAssignRoles = $derived(
    !!mem &&
      !mem.isSelf &&
      !mem.isOwner &&
      activeGuild()?.kind !== "dm" &&
      (has(activeGuild()?.myPerms || 0, PERM.MANAGE_ROLES) || !!activeGuild()?.isOwner),
  );

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

  const fprShort = $derived(mem ? mem.fingerprint.replace(/(.{4})/g, "$1 ").trim() : "");

  // Custom status split into emoji + text so each part can be styled.
  const statusParts = $derived(mem ? splitStatus(mem.status) : { emoji: "", text: "" });

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

<svelte:window
  onscroll={closeProfilePopover}
  onresize={closeProfilePopover}
  onkeydown={(e) => e.key === "Escape" && closeProfilePopover()}
  onpointerdown={(e) => {
    if (S.profilePopover && !e.target.closest(".pop") && !popoverJustOpened()) closeProfilePopover();
  }}
/>

{#if S.profilePopover && mem}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="pop"
    bind:this={card}
    style={pos ? `left:${pos.left}px;top:${pos.top}px` : "opacity:0;pointer-events:none"}
    role="dialog"
    aria-label="{mem.name || 'Member'} profile"
    onmouseenter={holdProfilePopover}
    onmouseleave={scheduleCloseProfilePopover}
  >
    <div
      class="banner"
      class:has-image={!!mem.banner}
      style={mem.banner
        ? `background-image:url(${mem.banner})`
        : mem.color
          ? `background:${mem.color}`
          : ""}
    ></div>
    <div class="head">
      <div class="av-wrap">
        <Avatar
          name={mem.name || mem.fingerprint}
          emoji={mem.emoji}
          color={mem.color}
          image={mem.avatar}
          size={56}
          online={mem.online}
          presence={mem.presence}
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

      {#if statusParts.emoji || statusParts.text}
        <div class="status-box">
          {#if statusParts.emoji}<span class="status-emoji">{statusParts.emoji}</span>{/if}
          {#if statusParts.text}<span class="status-text">{statusParts.text}</span>{/if}
        </div>
      {/if}

      {#if mem.bio}
        <div class="divider"></div>
        <div class="sec-label muted">About me</div>
        <div class="bio">{mem.bio}</div>
      {/if}

      {#if canNick}
        {#if editingNick}
          <form class="nick-box" onsubmit={saveNick}>
            <input
              bind:value={nickText}
              placeholder="Nickname for this guild"
              maxlength="64"
              disabled={nickBusy}
            />
            <button type="submit" class="nick-save" disabled={nickBusy}>Save</button>
          </form>
        {:else}
          <button class="nick-edit" onclick={startEditNick}>
            <Icon name="edit" size={12} />
            {mem.username ? "Change guild nickname" : "Set guild nickname"}
          </button>
        {/if}
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
      </div>
      <code class="fpr">{fprShort}</code>

      {#if mem.isSelf}
        <p class="hint muted">Others confirm it's really you by comparing this out-of-band.</p>
      {:else if mem.verified}
        <p class="hint muted">You've verified this fingerprint — no one can impersonate them.</p>
      {:else}
        <p class="hint muted">Compare this with them over a call; if it matches, verify.</p>
        <button class="verify-btn" onclick={verify}>Verify identity</button>
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

      {#if canModerate || canMute}
        <div class="divider"></div>
        <div class="mod-actions">
          {#if canMute}
            <button class="mod-btn" onclick={toggleMute}>
              <Icon name={isMuted ? "micOff" : "mic"} size={13} />
              {isMuted ? "Unmute" : "Mute 10m"}
            </button>
          {/if}
          {#if canModerate}
            <button class="mod-btn" onclick={kick}>
              <Icon name="door" size={13} /> Kick
            </button>
            <button class="mod-btn danger" onclick={ban}>
              <Icon name="trash" size={13} /> Ban
            </button>
          {/if}
        </div>
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
  .banner {
    height: 64px;
    background: linear-gradient(120deg, var(--accent), var(--accent-hover));
  }
  .banner.has-image {
    background-size: cover;
    background-position: center;
  }
  .head {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    padding: 0 14px;
  }
  .av-wrap {
    width: fit-content;
    margin-top: -28px;
    padding: 3px;
    background: var(--bg-1);
    border-radius: 50%;
  }
  .verified {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-bottom: 4px;
    padding: 2px 9px;
    font-size: 11px;
    font-weight: 600;
    border-radius: 999px;
    color: var(--ok);
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
    font-size: 16px;
  }
  .tag {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    background: var(--bg-3);
    color: var(--text-muted);
    padding: 1px 6px;
    border-radius: 8px;
  }
  .username {
    font-size: 12px;
    margin-top: -2px;
  }
  .mutual {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 11px;
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
  .status-emoji {
    font-size: 16px;
    line-height: 1.25;
  }
  .status-text {
    font-size: 13px;
    line-height: 1.4;
    color: var(--text);
    word-break: break-word;
  }
  .sec-label {
    font-size: 10px;
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
    font-size: 12px;
    line-height: 1.45;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 120px;
    overflow-y: auto;
  }
  .role-badge {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 1px 6px;
    border-radius: 8px;
    font-weight: 600;
  }
  .role-badge.owner {
    background: color-mix(in srgb, var(--accent) 22%, transparent);
    color: var(--accent);
  }
  .role-badge.mod {
    background: color-mix(in srgb, var(--ok) 20%, transparent);
    color: var(--ok);
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
    font-size: 12px;
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
  .role-toggle.on {
    background: var(--bg-4, var(--bg-3));
    color: var(--text);
    border-color: var(--border);
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
  .mod-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .mod-btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex: 1;
    justify-content: center;
    padding: 7px 8px;
    font-size: 12px;
    background: var(--bg-3);
    color: var(--text);
    border-radius: var(--radius-sm);
    white-space: nowrap;
  }
  .mod-btn:hover {
    background: var(--bg-4, var(--border));
  }
  .mod-btn.danger {
    color: var(--danger, #f04747);
  }
  .mod-btn.danger:hover {
    background: color-mix(in srgb, var(--danger, #f04747) 18%, transparent);
  }
  .nick-edit {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    align-self: flex-start;
    margin-top: 6px;
    padding: 4px 8px;
    font-size: 12px;
    background: var(--bg-3);
    color: var(--text-muted);
    border-radius: var(--radius-sm);
  }
  .nick-edit:hover {
    color: var(--text);
  }
  .nick-box {
    display: flex;
    gap: 6px;
    margin-top: 6px;
  }
  .nick-box input {
    flex: 1;
    padding: 7px 9px;
    font-size: 13px;
    border-radius: var(--radius-sm);
  }
  .nick-save {
    padding: 0 12px;
    font-size: 13px;
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
    font-size: 11px;
    background: var(--bg-3);
    color: var(--text-muted);
    border-radius: 8px;
  }
  .copy-btn:hover {
    color: var(--text);
  }
  .copy-btn.copied {
    color: var(--ok);
  }
  .fpr {
    font-family: ui-monospace, monospace;
    font-size: 11px;
    line-height: 1.5;
    word-break: break-word;
    background: var(--bg-0);
    border-radius: var(--radius-sm);
    padding: 6px 8px;
    color: var(--text);
  }
  .hint {
    font-size: 11px;
    line-height: 1.4;
    margin: 4px 0 0;
  }
  .verify-btn {
    margin-top: 8px;
    font-size: 13px;
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
    font-size: 13px;
    border-radius: var(--radius-sm);
  }
  .dm-send {
    padding: 0 12px;
    display: grid;
    place-items: center;
  }
</style>
