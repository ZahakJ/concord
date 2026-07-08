<script>
  // Second column: the active guild's channels, unread counts, per-channel
  // mute, and the self row (profile + network settings) pinned to the bottom.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import GroupAvatar from "./GroupAvatar.svelte";
  import Menu from "./Menu.svelte";
  import {
    S,
    activeGuild,
    selectGuild,
    selectChannel,
    toggleMute,
    channelShort,
    voiceMembersFor,
    nameFor,
    memberByFpr,
    moveChannelToCategory,
    jumpToChannel,
    markRead,
    openContextMenu,
    flash,
    refreshGuilds,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PERM, has } from "./lib/perms.js";

  let { onJoinVoice, onLeaveVoice, onToggleMute, onToggleShare, onToggleCamera } = $props();

  const g = $derived(activeGuild());
  const canManageChannels = $derived(has(g?.myPerms || 0, PERM.MANAGE_CHANNELS));

  function confirmDelete(title, body, onConfirm) {
    S.modal = { kind: "confirm", title, body, confirmLabel: "Delete", onConfirm };
  }
  function deleteChannel(c) {
    confirmDelete(`Delete #${c.name}?`, "This removes the channel and its messages for everyone.", async () => {
      try {
        await api.deleteChannel(S.activeGuildId, c.id);
        await refreshGuilds();
      } catch (err) {
        flash(err);
      }
      S.modal = null;
    });
  }
  function deleteCategory(cat) {
    confirmDelete(`Delete category ${cat.name}?`, "Channels inside it stay, just un-categorized.", async () => {
      try {
        await api.deleteCategory(S.activeGuildId, cat.id);
        await refreshGuilds();
      } catch (err) {
        flash(err);
      }
      S.modal = null;
    });
  }

  // In the DMs area, the channel column becomes a conversation list (Notes
  // first, then peer DMs).
  const dms = $derived.by(() => {
    // Notes (your self-DM) always shows; otherwise hide empty pending DMs — a
    // freshly-created invite nobody has joined yet (just you) is noise until a
    // peer redeems it and it gets a name/avatar.
    const list = S.guilds.filter(
      (x) => x.kind === "dm" && (x.dmNotes || (x.dmMembers ?? 2) >= 2),
    );
    return list.sort((a, b) => (a.dmNotes ? -1 : b.dmNotes ? 1 : 0));
  });

  // Group channels under their category (uncategorized first), each group
  // ordered by the channel's position.
  const groups = $derived.by(() => {
    if (!g) return [];
    const cats = [...(g.categories || [])].sort((a, b) => a.position - b.position);
    const byCat = (id) =>
      g.channels.filter((c) => (c.category || "") === id).sort((a, b) => a.position - b.position);
    const out = [{ id: "", name: "", channels: byCat("") }];
    for (const cat of cats) out.push({ id: cat.id, name: cat.name, channels: byCat(cat.id) });
    return out.filter((grp) => grp.channels.length || grp.id);
  });

  const typeIcon = (t) => (t === "voice" ? "speaker" : t === "announcement" ? "megaphone" : "hash");

  function clickChannel(c) {
    // Always open the channel's view — never auto-join. For a voice channel you
    // can read its chat without joining, and if you're already in the call this
    // opens the call view (Discord-style). Joining is the header's explicit
    // "Join voice" button.
    selectChannel(c.id);
  }

  function channelMenu(e, c) {
    openContextMenu(e, [
      { label: "Mark As Read", icon: "check", onClick: () => markRead(c.id) },
      {
        label: S.mutes[c.id] ? "Unmute Channel" : "Mute Channel",
        icon: S.mutes[c.id] ? "bell" : "bellOff",
        onClick: () => toggleMute(c.id),
      },
      canManageChannels && c.type !== "voice" && {
        label: "Edit Topic",
        icon: "edit",
        onClick: () => (S.modal = { kind: "channelTopic", channel: c }),
      },
      canManageChannels && { sep: true },
      canManageChannels && {
        label: "Delete Channel",
        icon: "trash",
        danger: true,
        onClick: () => deleteChannel(c),
      },
    ]);
  }

  // One entry point for starting conversations: pick one person (→ a 1:1 DM)
  // or several (→ a group DM), or invite by link from inside the picker.
  function newMessage() {
    S.modal = { kind: "newDM" };
  }

  // Right-click a DM to close it (leaves the group; local delete). Group DMs
  // can be left; a 1:1 just disappears from your list.
  function dmMenu(e, dm) {
    const isGroup = (dm.dmMembers ?? 2) > 2;
    openContextMenu(e, [
      {
        label: "Mark As Read",
        icon: "check",
        onClick: () => markRead(dm.channels?.[0]?.id),
      },
      isGroup && {
        label: "Rename Group",
        icon: "edit",
        onClick: () =>
          (S.modal = {
            kind: "renameGroup",
            guildId: dm.id,
            current: dm.dmNamed ? dm.name : "",
          }),
      },
      { sep: true },
      {
        label: (dm.dmMembers ?? 2) > 2 ? "Leave Group" : "Close DM",
        icon: "door",
        danger: true,
        onClick: () =>
          confirmDelete(
            (dm.dmMembers ?? 2) > 2 ? `Leave “${dm.name}”?` : `Close DM with ${dm.name}?`,
            "It's removed from your list. You can be re-invited later.",
            async () => {
              try {
                if (S.activeGuildId === dm.id) S.activeGuildId = null;
                await api.leaveGuild(dm.id);
                await refreshGuilds();
              } catch (err) {
                flash(err);
              }
              S.modal = null;
            },
          ),
      },
    ]);
  }
</script>

<aside class="cols">
  {#if g && g.kind !== "dm"}
    <button
      class="guild-name guild-header"
      class:has-banner={!!g.banner}
      style={g.banner ? `background-image:linear-gradient(rgba(0,0,0,0.15),rgba(0,0,0,0.55)),url(${g.banner})` : ""}
      title={g.description || g.name}
      onclick={() => (S.modal = { kind: "guildSettings" })}
    >
      {#if g.icon}
        <img class="g-icon" src={g.icon} alt="" />
      {/if}
      <strong>{g.name}</strong>
      <Icon name="chevron" size={13} />
    </button>
  {:else}
    <header class="guild-name">
      <strong>{g?.kind === "dm" ? "Direct messages" : "Concord"}</strong>
    </header>
  {/if}

  <div class="scroll">
    {#if g?.kind === "dm"}
      <div class="section-head">
        <span>Direct messages</span>
        <button class="cat-add always" title="New message" aria-label="New message" onclick={newMessage}>
          <Icon name="plus" size={12} />
        </button>
      </div>
      {#each dms as dm (dm.id)}
        {@const active = dm.id === S.activeGuildId}
        <button
          class="dm-item"
          class:active
          onclick={() => selectGuild(dm.id)}
          oncontextmenu={dm.dmNotes ? undefined : (e) => dmMenu(e, dm)}
        >
          {#if dm.dmNotes}
            <span class="dm-notes-icon"><Icon name="edit" size={15} /></span>
          {:else if (dm.dmMembers ?? 2) > 2}
            <GroupAvatar faces={dm.dmFaces || []} size={26} />
          {:else}
            <Avatar
              name={dm.name}
              image={dm.dmPeerAvatar || dm.icon}
              size={26}
              online={dm.dmPeer ? !!dm.dmPeerOnline : null}
              presence={dm.dmPeerPresence || ""}
            />
          {/if}
          <span class="dm-name">{dm.dmNotes ? "Notes (you)" : dm.name}</span>
        </button>
      {/each}
    {:else if g}
      <div class="section-head">
        <span>Channels</span>
        <Menu label="Add channel or category" icon="plus" align="right" compact>
          <button class="menu-item" onclick={() => (S.modal = { kind: "channel" })}>
            <Icon name="hash" size={14} /> New channel
          </button>
          <button class="menu-item" onclick={() => (S.modal = { kind: "category" })}>
            <Icon name="chevron" size={14} /> New category
          </button>
        </Menu>
      </div>

      {#each groups as grp (grp.id || "_uncat")}
        {#if grp.name}
          <div class="cat-head">
            <span>{grp.name}</span>
            {#if canManageChannels}
              <span class="cat-actions">
                <button
                  class="cat-add"
                  title="Create channel in {grp.name}"
                  aria-label="Create channel in {grp.name}"
                  onclick={() => (S.modal = { kind: "channel", category: grp.id })}
                >
                  <Icon name="plus" size={12} />
                </button>
                <button
                  class="cat-add"
                  title="Delete category {grp.name}"
                  aria-label="Delete category {grp.name}"
                  onclick={() => deleteCategory({ id: grp.id, name: grp.name })}
                >
                  <Icon name="trash" size={12} />
                </button>
              </span>
            {/if}
          </div>
        {/if}
        {#each grp.channels as c (c.id)}
          {@const u = S.unread[c.id]}
          {@const active = c.id === S.activeChannelId}
          {@const inVoice = S.voice && S.voice.channelId === c.id}
          <div class="channel-row" class:active class:voice-active={inVoice}>
            <button class="channel" class:muted-ch={S.mutes[c.id]} onclick={() => clickChannel(c)} oncontextmenu={(e) => channelMenu(e, c)}>
              <Icon name={typeIcon(c.type)} size={13} />
              <span class="ch-name">{c.name}</span>
              {#if c.type !== "voice" && c.id !== S.activeChannelId && u && !S.mutes[c.id]}
                <span class="count" class:mention={u.mentions > 0}>{u.count > 99 ? "99+" : u.count}</span>
              {/if}
              {#if inVoice}<Icon name="speaker" size={12} />{/if}
            </button>
            {#if canManageChannels}
              <span class="ch-menu">
                <Menu label="Channel options" icon="chevron" align="right" compact>
                  {#if (g?.categories || []).length}
                    <div class="menu-head">Move to…</div>
                    <button class="menu-item" onclick={() => moveChannelToCategory(c, "")}>
                      <Icon name="hash" size={13} /> No category
                    </button>
                    {#each g.categories as cat (cat.id)}
                      <button class="menu-item" onclick={() => moveChannelToCategory(c, cat.id)}>
                        <Icon name="chevron" size={13} /> {cat.name}
                      </button>
                    {/each}
                    <div class="menu-sep"></div>
                  {/if}
                  <button class="menu-item danger" onclick={() => deleteChannel(c)}>
                    <Icon name="trash" size={13} /> Delete channel
                  </button>
                </Menu>
              </span>
            {/if}
            {#if c.type !== "voice"}
              <button
                class="mute-btn"
                title={S.mutes[c.id] ? "Unmute channel" : "Mute channel"}
                aria-label={S.mutes[c.id] ? "Unmute channel" : "Mute channel"}
                onclick={() => toggleMute(c.id)}
              >
                <Icon name={S.mutes[c.id] ? "bellOff" : "bell"} size={13} />
              </button>
            {/if}
          </div>
          {#if c.type === "voice"}
            {#each voiceMembersFor(c.id) as vm (vm.fingerprint)}
              {@const vmem = memberByFpr(vm.fingerprint)}
              <button class="vc-member" onclick={() => clickChannel(c)} title={nameFor(vm.fingerprint)}>
                <span class="vc-av" class:speaking={vm.speaking}>
                  <Avatar
                    name={nameFor(vm.fingerprint)}
                    image={vmem?.avatar || ""}
                    emoji={vmem?.emoji || ""}
                    color={vmem?.color || ""}
                    size={20}
                  />
                </span>
                <span class="vc-name">{nameFor(vm.fingerprint)}{vm.self ? " (you)" : ""}</span>
                {#if vm.sharing}<span class="vc-share" title="Sharing video"><Icon name="screen" size={12} /></span>{/if}
              </button>
            {/each}
          {/if}
        {/each}
      {/each}
    {:else}
      <p class="muted empty-hint">
        No servers yet. Create one with <Icon name="plus" size={12} /> in the rail, or join a
        friend's with their invite code.
      </p>
    {/if}
  </div>

  <!-- The bottom voice bar is the "you're in a call elsewhere" indicator; while
       you're viewing the call itself the VoicePanel already has the controls, so
       showing it too would triplicate them. -->
  {#if S.voice && S.voice.channelId !== S.activeChannelId}
    <div class="voice-bar">
      <button
        class="vb-info"
        title="Return to call"
        aria-label="Return to call"
        onclick={() => jumpToChannel(S.voice.channelId)}
      >
        <span class="vb-live"></span>
        <span class="vb-text">
          <strong>Voice connected</strong>
          <span class="muted vb-ch">{channelShort(S.voice.channelId)}</span>
        </span>
      </button>
      <span class="vb-actions">
        <button class="vb-btn" title={S.muted ? "Unmute" : "Mute"} aria-label={S.muted ? "Unmute" : "Mute"} onclick={onToggleMute}>
          <Icon name={S.muted ? "micOff" : "mic"} size={14} />
        </button>
        <button
          class="vb-btn"
          class:on={S.cameraOn}
          title={S.cameraOn ? "Turn off camera" : "Turn on camera"}
          aria-label={S.cameraOn ? "Turn off camera" : "Turn on camera"}
          onclick={onToggleCamera}
        >
          <Icon name={S.cameraOn ? "cameraOff" : "camera"} size={14} />
        </button>
        <button
          class="vb-btn"
          class:on={S.sharing}
          title={S.sharing ? "Stop sharing" : "Share screen"}
          aria-label={S.sharing ? "Stop sharing" : "Share screen"}
          onclick={onToggleShare}
        >
          <Icon name={S.sharing ? "screenOff" : "screen"} size={14} />
        </button>
        <button class="vb-btn leave" title="Disconnect" aria-label="Disconnect" onclick={onLeaveVoice}>
          <Icon name="door" size={14} />
        </button>
      </span>
    </div>
  {/if}

  <div class="me-row">
    <button class="me" onclick={() => (S.modal = { kind: "profile" })} title="Edit profile">
      <Avatar
        name={S.displayName}
        emoji={S.identity.emoji}
        color={S.identity.color}
        image={S.identity.avatar}
        size={34}
      />
      <span class="me-text">
        <strong>{S.displayName || "Set your name"}</strong>
        <span class="muted small-status">{S.identity.status || "click to edit profile"}</span>
      </span>
    </button>
    <button class="me-gear ghost" title="Settings" aria-label="Settings" onclick={() => (S.modal = { kind: "settings" })}>
      <Icon name="gear" />
    </button>
  </div>
</aside>

<style>
  .cols {
    background: var(--bg-1);
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    min-width: 0;
    overflow-x: hidden; /* the column is a fixed width; never scroll sideways */
  }
  .guild-name {
    padding: 14px;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: 15px;
  }
  .guild-header {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    background: transparent;
    color: var(--text);
    text-align: left;
    border-radius: 0;
  }
  .guild-header:hover {
    background: var(--bg-3);
  }
  .guild-header.has-banner {
    background-size: cover;
    background-position: center;
    color: #fff;
    min-height: 56px;
    align-items: flex-end;
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  }
  .guild-header strong {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 15px;
  }
  .g-icon {
    width: 26px;
    height: 26px;
    border-radius: 8px;
    object-fit: cover;
    flex-shrink: 0;
  }
  .scroll {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .section-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    text-transform: uppercase;
    font-size: 11px;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    margin: 6px 6px 4px;
  }
  .cat-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.06em;
    color: var(--text-faint);
    font-weight: 700;
    margin: 10px 8px 2px;
  }
  .cat-actions {
    display: inline-flex;
    gap: 2px;
  }
  .cat-add {
    background: transparent;
    color: var(--text-faint);
    padding: 2px;
    display: grid;
    place-items: center;
    opacity: 0;
  }
  /* The DM "New message" + is the only way to start a conversation, so it's
     always visible (not hover-gated like the category adders). */
  .cat-add.always {
    opacity: 0.7;
  }
  .cat-add.always:hover {
    opacity: 1;
  }
  .cat-add:focus-visible {
    opacity: 1; /* keyboard users must see the control they've tabbed onto */
  }
  .cat-head:hover .cat-add,
  .section-head:hover .cat-add {
    opacity: 1;
  }
  .cat-add:hover {
    color: var(--text);
  }
  .voice-active {
    background: var(--accent-soft) !important;
  }
  .vc-member {
    display: flex;
    align-items: center;
    gap: 7px;
    width: calc(100% - 30px); /* == the 8px + 22px horizontal margins, so it doesn't overflow */
    margin: 1px 8px 1px 22px;
    padding: 3px 6px;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
    font-size: 12px;
    text-align: left;
  }
  .vc-member:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .vc-av {
    display: inline-grid;
    place-items: center;
    border-radius: 50%;
    border: 2px solid transparent;
    line-height: 0;
  }
  .vc-av.speaking {
    border-color: var(--ok);
  }
  .vc-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .vc-share {
    color: var(--ok);
    display: inline-grid;
    place-items: center;
  }
  .channel-row {
    position: relative;
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
  }
  .channel-row.active::before {
    content: "";
    position: absolute;
    left: -8px;
    top: 50%;
    transform: translateY(-50%);
    width: 3px;
    height: 60%;
    border-radius: 0 3px 3px 0;
    background: var(--accent);
  }
  .channel-row:hover,
  .channel-row.active {
    background: var(--bg-3);
  }
  .channel-row.active .channel {
    color: var(--text);
  }
  .channel {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 7px;
    background: transparent;
    color: var(--text-muted);
    padding: 7px 8px;
    font-size: 14px;
    text-align: left;
    min-width: 0;
    border-radius: var(--radius-sm);
  }
  .channel:hover {
    background: transparent;
    color: var(--text);
  }
  .channel.muted-ch {
    color: var(--text-faint);
  }
  .ch-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    min-width: 18px;
    padding: 0 5px;
    height: 16px;
    border-radius: 8px;
    background: var(--text-faint);
    color: white;
    font-size: 10px;
    font-weight: 700;
    display: grid;
    place-items: center;
  }
  .count.mention {
    background: var(--danger);
  }
  .ch-menu {
    display: inline-flex;
    opacity: 0;
  }
  .channel-row:hover .ch-menu {
    opacity: 1;
  }
  .menu-head {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-faint);
    padding: 4px 10px 2px;
  }
  .mute-btn {
    background: transparent;
    color: var(--text-faint);
    padding: 4px 6px;
    opacity: 0;
  }
  .channel-row:hover .mute-btn,
  .mute-btn:focus-visible {
    opacity: 1;
  }
  .mute-btn:hover {
    background: transparent;
    color: var(--text);
  }
  .empty-hint {
    font-size: 13px;
    line-height: 1.5;
    padding: 8px;
  }
  .dm-item {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 7px 8px;
    background: transparent;
    color: var(--text-muted);
    text-align: left;
    border-radius: var(--radius-sm);
  }
  .dm-item:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .dm-item.active {
    background: var(--bg-3);
    color: var(--text);
  }
  .dm-name {
    flex: 1;
    min-width: 0; /* let it shrink so long group-DM names ellipsize, not overflow */
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 14px;
  }
  .dm-notes-icon {
    width: 26px;
    height: 26px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent-hover);
    flex-shrink: 0;
  }
  .voice-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    padding: 8px 10px;
    margin: 0 8px;
    border-radius: var(--radius-md);
    background: var(--ok-soft);
    color: var(--ok);
  }
  .vb-info {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    flex: 1;
    background: transparent;
    color: var(--ok);
    text-align: left;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
  }
  .vb-info:hover {
    background: color-mix(in srgb, var(--ok) 18%, transparent);
  }
  .vb-live {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--ok);
    flex-shrink: 0;
    animation: vb-blink 1.4s ease-in-out infinite;
  }
  @keyframes vb-blink {
    50% {
      opacity: 0.3;
    }
  }
  .vb-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .vb-text strong {
    font-size: 12px;
  }
  .vb-ch {
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .vb-actions {
    display: flex;
    gap: 2px;
    flex-shrink: 0;
  }
  .vb-btn {
    background: transparent;
    color: var(--ok);
    padding: 5px;
    display: grid;
    place-items: center;
    border-radius: var(--radius-sm);
  }
  .vb-btn:hover {
    background: color-mix(in srgb, var(--ok) 22%, transparent);
  }
  .vb-btn.on {
    background: var(--ok);
    color: #fff;
  }
  .vb-btn.leave {
    color: var(--danger);
  }
  .vb-btn.leave:hover {
    background: var(--danger-soft);
  }
  .me-row {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 8px;
    border-top: 1px solid var(--border);
    background: var(--bg-0);
  }
  .me {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 10px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 6px;
    border-radius: var(--radius-sm);
    min-width: 0;
  }
  .me:hover {
    background: var(--bg-3);
  }
  .me-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: 13px;
  }
  .me-text strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .small-status {
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .me-gear {
    padding: 8px;
    border: none;
  }
</style>
