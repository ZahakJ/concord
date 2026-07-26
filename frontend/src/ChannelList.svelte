<script>
  // Second column: the active guild's channels, unread counts, per-channel
  // mute, and the self row (profile + network settings) pinned to the bottom.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import GroupAvatar from "./GroupAvatar.svelte";
  import Menu from "./Menu.svelte";
  import StatusPopover from "./StatusPopover.svelte";
  import { splitStatus, presenceLabel } from "./lib/presence.js";
  import {
    S,
    activeGuild,
    selectGuild,
    selectNotes,
    selectChannel,
    toggleMute,
    channelShort,
    voiceMembersFor,
    nameFor,
    memberByFpr,
    guildUnread,
    moveChannelToCategory,
    reorderChannel,
    jumpToChannel,
    markRead,
    openContextMenu,
    guildMenuItems,
    flash,
    refreshGuilds,
    isBlocked,
    blockUser,
    unblockUser,
    isCallLocked,
    CHANNEL_TYPES,
    setChannelType,
    canModerateVoice,
    moveVoiceMember,
    disconnectVoiceMember,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PERM, has } from "./lib/perms.js";
  import { longpress } from "./lib/touch.js";

  // Touch: long-press opens row menus (iOS never synthesizes contextmenu for
  // plain elements, and Android's synthesized one would double-fire alongside
  // the longpress action — so on coarse pointers longpress is the only path).
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  let { onJoinVoice, onLeaveVoice, onToggleMute, onToggleShare, onToggleCamera } = $props();

  const g = $derived(activeGuild());
  const canManageChannels = $derived(has(g?.myPerms || 0, PERM.MANAGE_CHANNELS));

  function confirmDelete(title, body, onConfirm, confirmLabel = "Delete") {
    S.modal = { kind: "confirm", title, body, confirmLabel, onConfirm };
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
    // Hide empty pending DMs — a freshly-created invite nobody has joined yet
    // (just you) is noise until a peer redeems it and it gets a name/avatar.
    const list = S.guilds.filter(
      (x) => x.kind === "dm" && (x.dmNotes || (x.dmMembers ?? 2) >= 2),
    );
    // Notes pinned on top, then conversations by recency — the DM you just
    // got a message in is always first.
    list.sort(
      (a, b) => (a.dmNotes ? -1 : b.dmNotes ? 1 : 0) || (b.lastActivity || 0) - (a.lastActivity || 0),
    );
    // Notes always appears first, even before it's been created (a placeholder
    // that materializes the self-DM on first click).
    if (!list.some((x) => x.dmNotes)) {
      list.unshift({ id: "__notes__", dmNotes: true, name: "Notes", dmMembers: 1 });
    }
    return list;
  });

  // Group channels under their category (uncategorized first), each group
  // ordered by the channel's position.
  const groups = $derived.by(() => {
    if (!g) return [];
    const cats = [...(g.categories || [])].sort((a, b) => a.position - b.position);
    const byCat = (id) =>
      g.channels
        .filter((c) => !c.parent && (c.category || "") === id) // threads nest under their forum
        .sort((a, b) => a.position - b.position);
    const out = [{ id: "", name: "", channels: byCat("") }];
    for (const cat of cats) out.push({ id: cat.id, name: cat.name, channels: byCat(cat.id) });
    return out.filter((grp) => grp.channels.length || grp.id);
  });

  // A channel's unread, INCLUDING its forum posts: threads don't appear in the
  // sidebar, so their unread has to surface on the forum row or it's invisible.
  function channelUnread(c) {
    const own = S.unread[c.id];
    if (c.type !== "forum") return own;
    let count = own?.count || 0;
    let mentions = own?.mentions || 0;
    for (const t of g?.channels || []) {
      if (t.parent !== c.id || S.mutes[t.id]) continue;
      const u = S.unread[t.id];
      if (!u) continue;
      count += u.count;
      mentions += u.mentions;
    }
    return count ? { count, mentions } : null;
  }

  // Unread channels in this guild, in sidebar order — powers the jump pill and
  // Alt+↑/↓ navigation (a real gap in Discord: it has the shortcut but no
  // visible affordance telling you how many are waiting or where).
  const unreadChannels = $derived.by(() =>
    groups
      .flatMap((grp) => grp.channels)
      .filter(
        (c) =>
          c.type !== "voice" &&
          c.id !== S.activeChannelId &&
          !S.mutes[c.id] &&
          !!channelUnread(c),
      ),
  );
  const unreadMentions = $derived(
    unreadChannels.reduce((n, c) => n + (channelUnread(c)?.mentions || 0), 0),
  );

  function jumpToNextUnread() {
    const list = unreadChannels;
    if (!list.length) return;
    // Prefer a channel with a mention; otherwise the next one after the
    // current position (wrapping), so repeated presses walk the list.
    const mention = list.find((c) => (channelUnread(c)?.mentions || 0) > 0);
    if (mention) {
      selectChannel(mention.id);
      return;
    }
    const all = groups.flatMap((grp) => grp.channels);
    const here = all.findIndex((c) => c.id === S.activeChannelId);
    const next =
      list.find((c) => all.indexOf(c) > here) || list[0];
    selectChannel(next.id);
  }

  // (Alt+Shift+↑/↓ already walks unread channels — see lib/shortcuts.js. The
  // pill below is the DISCOVERABLE version of that: it says how many are
  // waiting and jumps mention-first on click.)

  const typeIcon = (t) =>
    t === "voice" ? "speaker" : t === "announcement" ? "megaphone" : t === "forum" ? "forum" : "hash";

  function clickChannel(c) {
    if (c.type === "voice") {
      // Clicking a voice channel joins it and shows the call view. Clicking it
      // again while already in does nothing extra (no rejoin). To read its chat
      // WITHOUT joining, right-click → Open Chat.
      selectChannel(c.id);
      if (!S.voice || S.voice.channelId !== c.id) onJoinVoice?.(c.id);
    } else {
      selectChannel(c.id);
    }
  }

  function channelMenu(e, c) {
    openContextMenu(e, [
      c.type === "voice" && {
        label: "Open Chat",
        icon: "hash",
        onClick: () => selectChannel(c.id), // view messages without joining the call
      },
      c.type === "voice" && { sep: true },
      { label: "Mark As Read", icon: "check", onClick: () => markRead(c.id) },
      {
        label: S.mutes[c.id] ? "Unmute Channel" : "Mute Channel",
        icon: S.mutes[c.id] ? "bell" : "bellOff",
        onClick: () => toggleMute(c.id),
      },
      c.type !== "voice" && {
        label: "Disappearing messages…",
        icon: "clock",
        onClick: () => (S.modal = { kind: "disappear", channelId: c.id }),
      },
      // Change type: managers only, and never on a thread (it belongs to its
      // forum) — listed flat rather than nested because the menu has no
      // submenus and four short items read faster than a fly-out anyway.
      ...(canManageChannels && !c.parent
        ? [
            { sep: true },
            { label: "Change type to…", header: true },
            ...CHANNEL_TYPES.filter((t) => t.id !== (c.type || "text")).map((t) => ({
              label: t.label,
              icon: t.icon,
              onClick: () => setChannelType(c, t.id),
            })),
          ]
        : []),
      canManageChannels && c.type !== "voice" && {
        label: "Edit Topic",
        icon: "edit",
        onClick: () => (S.modal = { kind: "channelTopic", channel: c }),
      },
      canManageChannels && c.type === "announcement" && {
        label: "Linked Channels",
        icon: "link",
        onClick: () => (S.modal = { kind: "channelLinks", channel: c }),
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

  // --- Drag-to-reorder (Manage Channels only) -------------------------------
  // Text channel rows are HTML5-draggable: drop between rows to reorder within
  // a category, or onto a category header to move the channel there (top).
  // `drag` holds the lifted channel; `dropHint` drives the insertion line.
  // Keyboard/non-drag users keep the row menu's "Move to…" as before.
  let drag = $state(null); // { channel } — the row being dragged
  let dropHint = $state(null); // { catId, rowId, edge: "before"|"after" } | { catId, head: true }

  function dragStart(e, c) {
    drag = { channel: c };
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", c.id); // Firefox needs data for a drag to start
    // A compact "lifted" ghost chip instead of the browser's full-row snapshot.
    const ghost = document.createElement("div");
    ghost.textContent = `# ${c.name}`;
    ghost.style.cssText =
      "position:fixed;top:-100px;left:-100px;max-width:220px;overflow:hidden;" +
      "white-space:nowrap;text-overflow:ellipsis;padding:6px 14px;font-size:13px;" +
      "font-weight:600;color:var(--text);background:var(--bg-2);" +
      "border:1px solid var(--accent);border-radius:8px;" +
      "box-shadow:0 8px 24px rgba(0,0,0,0.35);pointer-events:none;";
    document.body.appendChild(ghost);
    e.dataTransfer.setDragImage(ghost, 16, 16);
    requestAnimationFrame(() => ghost.remove()); // only needed for the snapshot
  }
  function dragEnd() {
    drag = null;
    dropHint = null;
  }
  // Which side of a row the pointer is on decides insert-before vs after.
  const rowEdge = (e) => {
    const r = e.currentTarget.getBoundingClientRect();
    return e.clientY < r.top + r.height / 2 ? "before" : "after";
  };
  // Insertion index in grp's list *without* the dragged row (what reorderChannel
  // splices into), or -1 when the drop wouldn't move anything — used both to
  // hide no-op indicators and to skip no-op drops.
  function dropIndex(e, grp, c) {
    let idx = grp.channels.findIndex((x) => x.id === c.id);
    if (rowEdge(e) === "after") idx++;
    const from = grp.channels.findIndex((x) => x.id === drag.channel.id);
    if (from !== -1) {
      if (idx === from || idx === from + 1) return -1; // its own slot
      if (from < idx) idx--;
    }
    return idx;
  }
  function rowDragOver(e, grp, c) {
    if (vdrag) return personDragOver(e, c); // a person is in flight, not a channel
    if (!drag) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    dropHint =
      dropIndex(e, grp, c) === -1 ? null : { catId: grp.id, rowId: c.id, edge: rowEdge(e) };
  }
  function rowDrop(e, grp, c) {
    if (vdrag) return personDrop(e, c);
    if (!drag) return;
    e.preventDefault();
    const idx = dropIndex(e, grp, c);
    const ch = drag.channel;
    dragEnd();
    if (idx !== -1) reorderChannel(ch, grp.id, idx);
  }
  function headDragOver(e, grp) {
    if (!drag) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    // Already the first channel of this category? Dropping here is a no-op.
    dropHint = grp.channels[0]?.id === drag.channel.id ? null : { catId: grp.id, head: true };
  }
  function headDrop(e, grp) {
    if (!drag) return;
    e.preventDefault();
    const ch = drag.channel;
    dragEnd();
    reorderChannel(ch, grp.id, 0);
  }
  // Clear the indicator when the drag leaves the channel column entirely.
  function listDragLeave(e) {
    if (drag && !e.currentTarget.contains(e.relatedTarget)) dropHint = null;
  }

  // --- Drag a person between voice channels (Discord-style) ----------------
  // Separate from the channel-reorder drag above: this one lifts a PERSON, and
  // the drop targets are voice-channel rows rather than gaps between them.
  // Dragging yourself is just a fast way to switch rooms and needs no
  // permission; dragging someone else is a moderator action.
  const canModerate = $derived(canModerateVoice(S.identity.fingerprint, g));
  let vdrag = $state(null); // { fingerprint, name, from } — the person being carried
  let vDropId = $state(""); // voice channel currently under the pointer

  const voiceChannels = $derived((g?.channels || []).filter((c) => c.type === "voice"));
  const canDragPerson = (vm) => vm.self || canModerate;

  function personDragStart(e, vm, fromId) {
    if (!canDragPerson(vm)) return;
    vdrag = { fingerprint: vm.fingerprint, name: nameFor(vm.fingerprint), from: fromId, self: vm.self };
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", vm.fingerprint);
    // Carry a name chip rather than the browser's row snapshot, so it's obvious
    // a PERSON is in flight and not a channel.
    const ghost = document.createElement("div");
    ghost.textContent = vdrag.name;
    ghost.style.cssText =
      "position:fixed;top:-100px;left:-100px;max-width:200px;overflow:hidden;" +
      "white-space:nowrap;text-overflow:ellipsis;padding:5px 12px;font-size:12.5px;" +
      "font-weight:600;color:var(--accent-fg);background:var(--accent);" +
      "border-radius:999px;box-shadow:0 8px 24px rgba(0,0,0,0.4);pointer-events:none;";
    document.body.appendChild(ghost);
    e.dataTransfer.setDragImage(ghost, 20, 14);
    requestAnimationFrame(() => ghost.remove());
  }
  function personDragEnd() {
    vdrag = null;
    vDropId = "";
  }
  function personDragOver(e, c) {
    if (!vdrag || c.type !== "voice" || c.id === vdrag.from) return;
    e.preventDefault();
    e.stopPropagation(); // don't let the channel-reorder handler claim it
    e.dataTransfer.dropEffect = "move";
    vDropId = c.id;
  }
  function personDrop(e, c) {
    if (!vdrag || c.type !== "voice" || c.id === vdrag.from) return;
    e.preventDefault();
    e.stopPropagation();
    const p = vdrag;
    personDragEnd();
    // Dragging yourself is just joining the other room.
    if (p.self) onJoinVoice?.(c.id);
    else moveVoiceMember(p.fingerprint, p.from, c.id);
  }

  // Right-click / long-press a person in a voice channel.
  function personMenu(e, vm, fromId) {
    if (vm.self || !canModerate) return;
    const others = voiceChannels.filter((c) => c.id !== fromId);
    openContextMenu(e, [
      { label: nameFor(vm.fingerprint), header: true },
      ...(others.length ? [{ label: "Move to…", header: true }] : []),
      ...others.map((c) => ({
        label: c.name,
        icon: "speaker",
        onClick: () => moveVoiceMember(vm.fingerprint, fromId, c.id),
      })),
      { sep: true },
      {
        label: "Disconnect",
        icon: "door",
        danger: true,
        onClick: () => disconnectVoiceMember(vm.fingerprint, fromId),
      },
    ]);
  }

  // Presence + custom-status popover, anchored to the self-row avatar. Stores
  // the trigger's rect at open so the popover can position itself fixed (the
  // column clips overflow, so it can't be absolutely positioned in here).
  let statusPop = $state(null);
  let statusTrigger = $state(null);
  function toggleStatusPop(e) {
    if (statusPop) {
      statusPop = null;
      return;
    }
    const r = e.currentTarget.getBoundingClientRect();
    statusPop = { x: r.x, y: r.y, w: r.width, h: r.height };
  }
  // The command palette's "Set status" action can't reach this local popover
  // state, so it raises S.statusPopRequest; consume it and open at the self row.
  $effect(() => {
    if (S.statusPopRequest && statusTrigger) {
      S.statusPopRequest = false;
      const r = statusTrigger.getBoundingClientRect();
      statusPop = { x: r.x, y: r.y, w: r.width, h: r.height };
    }
  });
  const myStatus = $derived(splitStatus(S.identity.status));
  // Footer one-liner mirrors the member list: live activity wins over status.
  const myActivityLine = $derived.by(() => {
    const a = S.identity.activity;
    if (!a) return "";
    return a.artist ? `${a.artist} — ${a.title}` : a.title;
  });

  // One entry point for starting conversations: pick one person (→ a 1:1 DM)
  // or several (→ a group DM), or invite by link from inside the picker.
  function newMessage() {
    S.modal = { kind: "newDM" };
  }

  // Right-click a DM to close it. Closing a 1:1 DM only hides the conversation
  // (Discord-style) — it reopens, history intact, when either side messages
  // again. Leaving a group DM removes it locally.
  function dmMenu(e, dm) {
    const isGroup = (dm.dmMembers ?? 2) > 2;
    openContextMenu(e, [
      {
        label: "Mark As Read",
        icon: "check",
        onClick: () => markRead(dm.channels?.[0]?.id),
      },
      dm.channels?.[0] && {
        label: "Disappearing messages…",
        icon: "clock",
        onClick: () => (S.modal = { kind: "disappear", channelId: dm.channels[0].id }),
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
      !isGroup &&
        dm.dmPeer &&
        (isBlocked(dm.dmPeer)
          ? { label: "Unblock", icon: "lock", onClick: () => unblockUser(dm.dmPeer, dm.name) }
          : {
              label: "Block",
              icon: "lock",
              danger: true,
              onClick: () => blockUser(dm.dmPeer, dm.name),
            }),
      {
        label: (dm.dmMembers ?? 2) > 2 ? "Leave Group" : "Close DM",
        icon: "door",
        danger: true,
        onClick: () =>
          confirmDelete(
            (dm.dmMembers ?? 2) > 2 ? `Leave “${dm.name}”?` : `Close DM with ${dm.name}?`,
            (dm.dmMembers ?? 2) > 2
              ? "It's removed from your list. You can be re-invited later."
              : "The conversation is hidden from your list — messaging each other brings it back, history intact.",
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
            (dm.dmMembers ?? 2) > 2 ? "Leave" : "Close",
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
      oncontextmenu={(e) => openContextMenu(e, guildMenuItems(g), { title: g.name })}
    >
      {#if g.icon}
        <img class="g-icon" src={g.icon} alt="" />
      {/if}
      <strong>{g.name}</strong>
      <Icon name="chevron" size={13} />
    </button>
  {:else}
    <header class="guild-name" class:dm-head={g?.kind === "dm"}>
      <strong>{g?.kind === "dm" ? "Direct messages" : "Concord"}</strong>
      {#if g?.kind === "dm"}
        <button class="cat-add always" title="New message" aria-label="New message" onclick={newMessage}>
          <Icon name="plus" size={13} />
        </button>
      {/if}
    </header>
  {/if}

  <!-- Drag-to-reorder is a pointer-only enhancement; keyboard users reorder via
       the row menu's "Move to…", so the drag handlers stay off the a11y tree. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="scroll" ondragleave={listDragLeave}>
    {#if g?.kind === "dm"}
      {#each dms as dm (dm.id)}
        {@const active = dm.id === S.activeGuildId}
        {@const unread = dm.dmNotes ? { count: 0 } : guildUnread(dm)}
        <button
          class="dm-item"
          class:active
          class:unread={unread.count > 0 && !active}
          onclick={() => (dm.dmNotes ? selectNotes() : selectGuild(dm.id))}
          oncontextmenu={dm.dmNotes ? undefined : coarse ? (e) => e.preventDefault() : (e) => dmMenu(e, dm)}
          use:longpress={{ handler: (e) => !dm.dmNotes && dmMenu(e, dm) }}
        >
          {#if dm.dmNotes}
            <span class="dm-notes-icon"><Icon name="edit" size={15} /></span>
          {:else if (dm.dmMembers ?? 2) > 2}
            <GroupAvatar faces={dm.dmFaces || []} size={26} />
          {:else}
            <Avatar
              name={dm.name}
              image={dm.dmPeerAvatar || dm.dmFaces?.[0]?.avatar || dm.icon}
              size={26}
              online={dm.dmPeer ? !!dm.dmPeerOnline : null}
              presence={dm.dmPeerPresence || ""}
            />
          {/if}
          <span class="dm-name">{dm.dmNotes ? "Notes (you)" : dm.name}</span>
          {#if unread.count > 0 && !active}
            <span class="count" class:mention={unread.mentions > 0}
              >{unread.count > 99 ? "99+" : unread.count}</span
            >
          {/if}
        </button>
      {/each}
      {#if !dms.some((d) => !d.dmNotes)}
        <div class="empty-block">
          <span class="empty-ic"><Icon name="smile" size={18} /></span>
          <p class="muted">
            It's quiet in here. Start a conversation — every DM is end-to-end encrypted, just
            between you two.
          </p>
          <button class="empty-cta" onclick={newMessage}>
            <Icon name="plus" size={13} /> New message
          </button>
        </div>
      {/if}
    {:else if g}
      {#if unreadChannels.length}
        <!-- Better than Discord: it hides "jump to unread" behind a shortcut.
             We SHOW what's waiting, where, and let one click walk it. -->
        <button
          class="unread-jump"
          class:mention={unreadMentions > 0}
          title="Jump to the next unread channel (mentions first) · Alt+Shift+↓"
          onclick={jumpToNextUnread}
        >
          <span class="uj-dot"></span>
          <span class="uj-text">
            {unreadChannels.length}
            {unreadChannels.length === 1 ? "channel" : "channels"} unread
            {#if unreadMentions > 0}· {unreadMentions} @you{/if}
          </span>
          <span class="uj-cta">Jump ↓</span>
        </button>
      {/if}
      <div class="section-head">
        <span>Channels</span>
        {#if !canManageChannels}
          <!-- No silent nothing: members without Manage Channels see WHY. -->
          <button
            class="add-locked"
            title="You need the Manage Channels permission to add channels"
            aria-label="Adding channels requires the Manage Channels permission"
            onclick={() => flash("You need the Manage Channels permission to add channels here — ask an admin for a role that grants it.")}
          >
            <Icon name="plus" size={13} />
          </button>
        {:else}
        <Menu label="Add channel or category" icon="plus" align="right" compact>
          <button class="menu-item" onclick={() => (S.modal = { kind: "channel" })}>
            <Icon name="hash" size={14} /> New channel
          </button>
          <button class="menu-item" onclick={() => (S.modal = { kind: "category" })}>
            <Icon name="chevron" size={14} /> New category
          </button>
        </Menu>
        {/if}
      </div>

      {#each groups as grp (grp.id || "_uncat")}
        {#if grp.name}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="cat-head"
            class:drop-into={dropHint?.head && dropHint.catId === grp.id}
            ondragover={(e) => headDragOver(e, grp)}
            ondrop={(e) => headDrop(e, grp)}
          >
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
          {@const u = channelUnread(c)}
          {@const active = c.id === S.activeChannelId}
          {@const inVoice = S.voice && S.voice.channelId === c.id}
          {@const occupied = c.type === "voice" && voiceMembersFor(c.id).length > 0}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="channel-row"
            class:active
            class:unread={!!u && !active && !S.mutes[c.id]}
            class:mentioned={!!u?.mentions && !active && !S.mutes[c.id]}
            class:voice-active={inVoice}
            class:vdrop={vDropId === c.id}
            ondragleave={() => (vDropId === c.id ? (vDropId = "") : null)}
            class:dragging={drag?.channel.id === c.id}
            class:drop-before={dropHint?.rowId === c.id && dropHint.edge === "before"}
            class:drop-after={dropHint?.rowId === c.id && dropHint.edge === "after"}
            draggable={canManageChannels && c.type !== "voice"}
            ondragstart={(e) => dragStart(e, c)}
            ondragend={dragEnd}
            ondragover={(e) => rowDragOver(e, grp, c)}
            ondrop={(e) => rowDrop(e, grp, c)}
          >
            <button
              class="channel"
              class:muted-ch={S.mutes[c.id]}
              onclick={() => clickChannel(c)}
              oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => channelMenu(e, c)}
              use:longpress={{ handler: (e) => channelMenu(e, c) }}
            >
              <Icon name={typeIcon(c.type)} size={13} />
              <span class="ch-name">{c.name}</span>
              {#if c.type === "voice" && isCallLocked(c.id)}
                <span class="ch-lock" title="Call locked — knock to join"><Icon name="lock" size={11} /></span>
              {/if}
              {#if c.type !== "voice" && !active && u && !S.mutes[c.id]}
                <span class="count" class:mention={u.mentions > 0}>
                  {u.mentions > 0 ? (u.mentions > 99 ? "99+" : u.mentions) : u.count > 99 ? "99+" : u.count}
                </span>
              {/if}
              {#if occupied}
                <!-- Live equalizer: someone is in this voice channel right now. -->
                <span class="eq" class:you={inVoice} aria-label="Voice active"><i></i><i></i><i></i></span>
              {/if}
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
              <button
                class="vc-member"
                class:draggable={canDragPerson(vm)}
                class:lifted={vdrag?.fingerprint === vm.fingerprint}
                draggable={canDragPerson(vm)}
                ondragstart={(e) => personDragStart(e, vm, c.id)}
                ondragend={personDragEnd}
                oncontextmenu={(e) => personMenu(e, vm, c.id)}
                use:longpress={{ handler: (e) => personMenu(e, vm, c.id) }}
                onclick={() => clickChannel(c)}
                title={canDragPerson(vm)
                  ? `${nameFor(vm.fingerprint)} — drag to another voice channel`
                  : nameFor(vm.fingerprint)}
              >
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
                {#if vm.deafened}
                  <span class="vc-mark" title="Deafened"><Icon name="deafened" size={11} /></span>
                {:else if vm.muted}
                  <span class="vc-mark" title="Muted"><Icon name="micOff" size={11} /></span>
                {/if}
                {#if vm.sharing}
                  <span class="vc-live" title={vm.self ? "You're sharing" : "Live — click to watch"}>
                    <span class="live-dot"></span>LIVE
                  </span>
                {/if}
              </button>
            {/each}
          {/if}
        {/each}
      {/each}
    {:else}
      <!-- No CTAs here: the center welcome cards already offer Create/Join
           prominently, so repeating them in the sidebar was redundant. Just a
           gentle pointer. -->
      <div class="empty-block">
        <span class="empty-ic"><Icon name="concorde" size={20} /></span>
        <p class="muted">
          No guilds yet. A guild is your own space — channels, voice, files, all
          encrypted and hosted by its members. Start one from the welcome cards →
        </p>
      </div>
    {/if}
  </div>

  <!-- Persistent bottom-left call controller (Discord-style): shows whenever
       you're in a call, whatever you're viewing. When you're on the call's own
       channel the VoicePanel also shows full controls — that's fine, this is the
       always-there compact bar; when you navigate away the FloatingCall dock
       joins it too. -->
  {#if S.voice}
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
    <button
      bind:this={statusTrigger}
      class="me-status-trigger"
      title="Set status"
      aria-label="Set status"
      aria-haspopup="dialog"
      aria-expanded={!!statusPop}
      onclick={toggleStatusPop}
    >
      <Avatar
        name={S.displayName}
        emoji={S.identity.emoji}
        color={S.identity.color}
        image={S.identity.avatar}
        size={34}
        online={true}
        presence={S.identity.presence || ""}
        frame={S.identity.frame || ""}
      />
    </button>
    <button class="me" onclick={() => (S.modal = { kind: "profile" })} title="Edit profile">
      <span class="me-text">
        <strong>{S.displayName || "Set your name"}</strong>
        <span class="muted small-status">
          {#if myActivityLine}
            <span class="eq me-eq" aria-label="Listening"><i></i><i></i><i></i></span>
            {myActivityLine}
          {:else}
            {#if myStatus.emoji}<span class="st-emoji">{myStatus.emoji}</span>{/if}
            {myStatus.text || presenceLabel(S.identity.presence)}
          {/if}
        </span>
      </span>
    </button>
    <button class="me-gear ghost" title="Settings" aria-label="Settings" onclick={() => (S.modal = { kind: "settings" })}>
      <Icon name="gear" />
    </button>
  </div>
</aside>

{#if statusPop}
  <StatusPopover anchor={statusPop} onClose={() => (statusPop = null)} />
{/if}

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
  /* DM column title carries the "New message" + on the right (no separate
     "Direct messages" section header below — that was a duplicate label). */
  .guild-name.dm-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    overflow: visible;
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
    /* Decorative avatar rings (which spin OUTSIDE the avatar box) must not
       make this column think it needs a horizontal scrollbar — that's what
       made the divider "pulsate". clip keeps them visible; hidden would too,
       but clip doesn't create a scroll port. */
    overflow-x: clip;
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
    letter-spacing: 0.06em;
    font-weight: 700;
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
  /* A voice row lit up as a landing spot while someone is being carried. */
  .channel-row.vdrop {
    background: var(--accent-soft);
    box-shadow: inset 0 0 0 1.5px var(--accent);
    border-radius: var(--radius-sm);
  }
  .channel-row.vdrop .channel {
    color: var(--text);
  }
  /* The person in flight fades in place, so it's clear which one you picked
     up and that they haven't moved yet. */
  .vc-member.lifted {
    opacity: 0.4;
  }
  /* Muted/deafened marks sit with the name, quiet enough not to compete with
     the speaking ring. */
  .vc-mark {
    display: grid;
    place-items: center;
    flex-shrink: 0;
    color: var(--text-faint);
  }
  .vc-member.draggable {
    cursor: grab;
  }
  .vc-member.draggable:active {
    cursor: grabbing;
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
    animation: speak-ring 1.1s ease-in-out infinite;
  }
  @keyframes speak-ring {
    0%,
    100% {
      box-shadow: 0 0 0 0 color-mix(in srgb, var(--ok) 40%, transparent);
    }
    50% {
      box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 22%, transparent);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .vc-av.speaking {
      animation: none;
    }
  }
  .vc-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ch-lock {
    display: inline-grid;
    place-items: center;
    color: var(--text-faint);
    flex-shrink: 0;
  }
  .vc-live {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    margin-left: auto;
    padding: 1px 5px;
    border-radius: 4px;
    background: color-mix(in srgb, #f04747 20%, transparent);
    color: #ff6b6b;
    font-size: 9px;
    font-weight: 800;
    letter-spacing: 0.04em;
    flex-shrink: 0;
  }
  .live-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: #f04747;
    animation: live-pulse 1.4s ease-in-out infinite;
  }
  @keyframes live-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .live-dot {
      animation: none;
    }
  }
  .channel-row {
    position: relative;
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
    transition: background 0.15s ease;
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
    box-shadow: 0 0 6px color-mix(in srgb, var(--accent) 60%, transparent);
    animation: nub-in 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  @keyframes nub-in {
    from {
      transform: translateY(-50%) scaleY(0.2);
    }
  }
  .channel-row:hover {
    background: var(--bg-3);
  }
  /* Active reads as "accent-charged", distinct from a passing hover. */
  .channel-row.active {
    background: var(--accent-soft);
  }
  .channel-row.active .channel {
    color: var(--text);
  }
  /* Drag-to-reorder affordances. The source row dims while lifted; the target
     shows a slim accent insertion line in the 2px gap between rows. Transitions
     are already zeroed globally under prefers-reduced-motion. */
  .channel-row.dragging {
    opacity: 0.35;
  }
  .channel-row.drop-before::after,
  .channel-row.drop-after::after {
    content: "";
    position: absolute;
    left: 4px;
    right: 4px;
    height: 2px;
    border-radius: 1px;
    background: var(--accent);
    pointer-events: none;
    z-index: 1;
  }
  .channel-row.drop-before::after {
    top: -2px;
  }
  .channel-row.drop-after::after {
    bottom: -2px;
  }
  /* Hovering a drag over a category header: "drop here to file it under…". */
  .cat-head.drop-into {
    color: var(--accent);
    box-shadow: inset 0 -2px 0 var(--accent);
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
  /* Rows glide a hair right on hover — the list feels sprung, not painted. */
  .channel,
  .dm-item {
    transition:
      background 0.15s ease,
      color 0.15s ease,
      transform 0.15s ease;
  }
  @media (pointer: fine) {
    .channel-row:hover .channel,
    .dm-item:hover {
      transform: translateX(2px);
    }
  }
  /* Live-voice equalizer: three bars dancing while the room is occupied.
     Only occupied voice channels run it (a rarity, not a list-wide loop). */
  .eq {
    display: inline-flex;
    align-items: flex-end;
    gap: 1.5px;
    height: 12px;
    flex-shrink: 0;
    color: var(--ok);
  }
  .eq.you {
    color: var(--accent);
  }
  .eq i {
    width: 2.5px;
    border-radius: 1px;
    background: currentColor;
    height: 30%;
    animation: eq-bounce 1s ease-in-out infinite;
  }
  .eq i:nth-child(2) {
    animation-delay: 0.25s;
  }
  .eq i:nth-child(3) {
    animation-delay: 0.5s;
  }
  @keyframes eq-bounce {
    0%,
    100% {
      height: 30%;
    }
    50% {
      height: 100%;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .eq i {
      animation: none;
      height: 60%;
    }
    .channel-row:hover .channel,
    .dm-item:hover {
      transform: none;
    }
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
  /* ---- unread & mentions ----------------------------------------------
     Discord marks unread with a white bar + bold name. We do that AND tint
     the bar (accent = unread, danger = @you), tint the count pill to match,
     and keep the whole row a touch brighter so it reads at a glance. */
  .channel-row {
    position: relative;
  }
  .channel-row.unread::before {
    content: "";
    position: absolute;
    left: -6px;
    top: 50%;
    transform: translateY(-50%);
    width: 4px;
    height: 18px;
    border-radius: 0 3px 3px 0;
    background: var(--text);
    animation: unread-in 0.22s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .channel-row.mentioned::before {
    background: var(--danger);
    box-shadow: 0 0 8px color-mix(in srgb, var(--danger) 55%, transparent);
  }
  @keyframes unread-in {
    from {
      transform: translateY(-50%) scaleY(0.2);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .channel-row.unread::before {
      animation: none;
    }
  }
  .channel-row.unread .channel {
    color: var(--text);
  }
  .channel-row.unread .ch-name {
    font-weight: 600;
  }
  .channel-row.mentioned .ch-name {
    color: var(--text);
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
    animation: count-pop 0.25s cubic-bezier(0.34, 1.56, 0.64, 1) both;
  }
  @keyframes count-pop {
    from {
      transform: scale(0.4);
      opacity: 0;
    }
  }
  .channel-row.unread .count {
    background: var(--accent);
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
  /* Gentle empty states for "no DMs yet" / "no guilds yet": a soft icon chip,
     one line of copy, and the single action that fixes it. */
  .empty-block {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 8px;
    margin: 14px 6px;
    padding: 16px 12px;
    border: 1px dashed var(--border);
    border-radius: var(--radius-md);
  }
  .empty-block p {
    margin: 0;
    font-size: 12.5px;
    line-height: 1.5;
  }
  .empty-ic {
    width: 38px;
    height: 38px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .empty-cta {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    justify-content: center;
    padding: 7px 10px;
    font-size: 13px;
    font-weight: 600;
    border-radius: var(--radius-sm);
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
    transition:
      background 0.15s ease,
      color 0.15s ease;
  }
  .dm-item:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .dm-item.active {
    background: var(--accent-soft);
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
  .dm-item.unread {
    color: var(--text);
  }
  .dm-item.unread .dm-name {
    font-weight: 600;
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
    border: 1px solid color-mix(in srgb, var(--ok) 28%, transparent);
    box-shadow: 0 0 14px color-mix(in srgb, var(--ok) 10%, transparent);
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
  @media (prefers-reduced-motion: reduce) {
    .vb-live {
      animation: none;
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
    /* Pad past the phone's bottom system bar (Android gesture/button bar, iOS
       home indicator) — without this, edge-to-edge draw (viewport-fit=cover)
       lets the OS nav overlap the self row and its settings/sign-out button.
       env() is 0 on desktop, so this is a no-op there. */
    padding: 8px;
    padding-bottom: calc(8px + env(safe-area-inset-bottom));
    border-top: 1px solid var(--border);
    background: var(--bg-0);
    /* Your own framed avatar lives here; its ring overflows by design. */
    overflow: clip;
  }
  .me-status-trigger {
    background: transparent;
    padding: 3px;
    border-radius: 50%;
    line-height: 0;
    flex-shrink: 0;
    transition:
      background 0.12s ease,
      transform 0.15s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .me-status-trigger:hover {
    background: var(--bg-3);
    transform: scale(1.07);
  }
  @media (prefers-reduced-motion: reduce) {
    .me-status-trigger:hover {
      transform: none;
    }
  }
  /* The footer's listening equalizer sits inline with the status line. */
  .me-eq {
    height: 9px;
    margin-right: 4px;
    vertical-align: baseline;
    color: var(--accent);
  }
  /* The dot's cutout ring should match this row's background, not the column's. */
  .me-status-trigger :global(.dot) {
    border-color: var(--bg-0);
  }
  .st-emoji {
    margin-right: 3px;
  }
  .me {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 6px;
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
  /* Touch: taller rows (44px+ targets), slightly larger type, and the
     hover-revealed affordances (category +/trash, channel menu, mute bell)
     always visible at reduced opacity — hover doesn't exist on a phone. */
  @media (pointer: coarse), (max-width: 700px) {
    .channel {
      min-height: 44px;
      font-size: 15px;
    }
    .dm-item {
      min-height: 48px;
    }
    .dm-name {
      font-size: 15px;
    }
    .cat-add,
    .ch-menu,
    .mute-btn {
      opacity: 0.55;
    }
    .mute-btn {
      padding: 8px 10px;
      /* Invisible overlay pads the tap area out to ~44px. */
      position: relative;
    }
    .mute-btn::after {
      content: "";
      position: absolute;
      inset: -3px -6px;
    }
    .vc-member {
      min-height: 38px;
    }
    .me {
      min-height: 44px;
    }
    .me-gear {
      min-width: 44px;
      min-height: 44px;
    }
    /* Invisible overlay pads the avatar/status tap area out to 44px. */
    .me-status-trigger {
      position: relative;
    }
    .me-status-trigger::after {
      content: "";
      position: absolute;
      inset: -2px;
    }
  }
  .add-locked {
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    padding: 0;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--text-faint);
    opacity: 0.55;
    cursor: help;
  }
  .add-locked:hover {
    background: var(--bg-3);
    opacity: 0.8;
  }
  /* Unread summary pill: the affordance Discord never gives you. */
  .unread-jump {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    margin: 0 0 6px;
    padding: 7px 10px;
    border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
    border-radius: 999px;
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    color: var(--text);
    font-size: 12px;
    cursor: pointer;
    animation: uj-in 0.25s ease;
    transition:
      background 0.14s ease,
      border-color 0.14s ease;
  }
  @keyframes uj-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .unread-jump {
      animation: none;
    }
  }
  .unread-jump:hover {
    background: color-mix(in srgb, var(--accent) 22%, transparent);
    border-color: var(--accent);
  }
  .unread-jump.mention {
    border-color: color-mix(in srgb, var(--danger) 55%, transparent);
    background: color-mix(in srgb, var(--danger) 12%, transparent);
  }
  .unread-jump.mention:hover {
    background: color-mix(in srgb, var(--danger) 20%, transparent);
  }
  .uj-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    flex: none;
    animation: uj-pulse 2.4s ease-in-out infinite;
  }
  .unread-jump.mention .uj-dot {
    background: var(--danger);
  }
  @keyframes uj-pulse {
    0%,
    100% {
      box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent) 50%, transparent);
    }
    50% {
      box-shadow: 0 0 0 4px transparent;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .uj-dot {
      animation: none;
    }
  }
  .uj-text {
    flex: 1;
    min-width: 0;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .uj-cta {
    flex: none;
    font-size: 11px;
    font-weight: 700;
    color: var(--accent);
    letter-spacing: 0.02em;
  }
  .unread-jump.mention .uj-cta {
    color: var(--danger);
  }
</style>
