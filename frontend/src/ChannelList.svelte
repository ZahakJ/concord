<script>
  // Second column: the active guild's channels, unread counts, per-channel
  // mute, and the self row (profile + network settings) pinned to the bottom.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import GroupAvatar from "./GroupAvatar.svelte";
  import Menu from "./Menu.svelte";
  import StatusPopover from "./StatusPopover.svelte";
  import Banner from "./Banner.svelte";
  import { guildBannerArt } from "./lib/guildbanners.js";
  import { splitStatus, presenceLabel } from "./lib/presence.js";
  import {
    S,
    openProfilePopover,
    activeGuild,
    selectGuild,
    selectNotes,
    selectChannel,
    toggleMute,
    isMuted,
    setChannelNotifs,
    setGuildNotifs,
    guildNotifLevel,
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
    dmList,
    isCallLocked,
    CHANNEL_TYPES,
    channelTypeIcon,
    setChannelType,
    canModerateVoice,
    moveVoiceMember,
    disconnectVoiceMember,
    clockOpts,
    setPref,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { tooltip } from "./lib/tooltip.js";
  import { RADAR, liveChannelSet } from "./lib/radar.svelte.js";
  import { PERM, has } from "./lib/perms.js";
  import { LEVELS, levelLabel } from "./lib/notifs.js";
  import { longpress, haptic } from "./lib/touch.js";
  import { draftIn } from "./lib/drafts.svelte.js";
  import { callClock } from "./lib/calltimer.svelte.js";

  const clock = $derived(callClock());

  // getDisplayMedia is absent in Android/iOS WebViews, so the share button is a
  // control that can only ever fail. It goes away entirely rather than sitting
  // in the voice bar taking a quarter of the width from the ones that work.
  const canShareScreen =
    typeof navigator !== "undefined" && !!navigator.mediaDevices?.getDisplayMedia;

  // Touch: long-press opens row menus (iOS never synthesizes contextmenu for
  // plain elements, and Android's synthesized one would double-fire alongside
  // the longpress action — so on coarse pointers longpress is the only path).
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  let { onJoinVoice, onLeaveVoice, onToggleMute, onToggleDeafen, onToggleShare, onToggleCamera } =
    $props();

  const g = $derived(activeGuild());
  const canManageChannels = $derived(has(g?.myPerms || 0, PERM.MANAGE_CHANNELS));

  // Channels hosting a live channel-located event right now (the event radar's
  // passive indicator). Derives from RADAR.now's tick, so the badge appears at
  // start and clears itself when the live window ends — no timers here.
  const liveChannels = $derived(liveChannelSet());

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

  // ---- categories: rename in place, and fold ----
  //
  // Neither existed. A typo meant delete, recreate and re-drag every channel
  // back — and deleting mints a NEW id, so a peer that missed the removal
  // keeps a dangling reference. The header is now the fold control and
  // double-click (or the menu, or the pencil) starts the rename.
  let renamingCat = $state("");

  // The same shorthand the header pill uses, so a row and a header never
  // disagree about how long an interval is.
  const slowShort = (secs) =>
    secs >= 3600 ? `${Math.round(secs / 3600)}h` : secs >= 60 ? `${Math.round(secs / 60)}m` : `${secs}s`;

  async function commitCatRename(grp, value) {
    const name = (value || "").trim();
    renamingCat = "";
    if (!name || name === grp.name) return;
    try {
      await api.renameCategory(S.activeGuildId, grp.id, name);
      await refreshGuilds();
    } catch (err) {
      flash(err);
    }
  }

  // Folded state is per DEVICE, not per guild member: which parts of a sidebar
  // you keep shut is a reading preference, and shipping it would mean deciding
  // what everyone else's sidebar looks like.
  const collapsed = (catID) => !!catID && !!S.prefs.collapsedCats?.[catID];
  function toggleCat(catID) {
    if (!catID) return;
    const next = { ...(S.prefs.collapsedCats || {}) };
    if (next[catID]) delete next[catID];
    else next[catID] = true;
    setPref("collapsedCats", next);
  }

  // A folded category still shows the rows you would lose track of otherwise:
  // the channel you are reading, and anything unread. Folding a category must
  // hide clutter, never a mention.
  function keepVisible(c) {
    if (c.id === S.activeChannelId) return true;
    if (S.voice?.channelId === c.id) return true;
    const u = channelUnread(c);
    return !!u && !isMuted(c.id, g?.id);
  }

  function categoryMenu(e, grp) {
    if (!canManageChannels) return;
    openContextMenu(
      e,
      [
        {
          label: collapsed(grp.id) ? "Expand" : "Collapse",
          icon: "chevron",
          onClick: () => toggleCat(grp.id),
        },
        { sep: true },
        { label: "Rename category…", icon: "edit", onClick: () => (renamingCat = grp.id) },
        {
          label: "Create channel here…",
          icon: "plus",
          onClick: () => (S.modal = { kind: "channel", category: grp.id }),
        },
        { sep: true },
        {
          label: "Delete category",
          icon: "trash",
          danger: true,
          onClick: () => deleteCategory({ id: grp.id, name: grp.name }),
        },
      ],
      { title: grp.name },
    );
  }

  // In the DMs area, the channel column becomes a conversation list (Notes
  // first, then peer DMs).
  const dms = $derived.by(() => {
    // Shared with openDMs so the button and the list can never disagree about
    // what "your DMs" means or which one is first.
    const list = dmList();
    // Notes always appears first, even before it's been created (a placeholder
    // that materializes the self-DM on first click).
    if (!list.some((x) => x.dmNotes)) {
      list.unshift({ id: "__notes__", dmNotes: true, name: "Notes", dmMembers: 1 });
    }
    return list;
  });

  // The second line of a DM row: who said the last thing. "You:" for our own,
  // and the counterpart's FIRST NAME for theirs — a peer DM's title is already
  // their full name, so repeating it reads as stutter and a bare "Them:" says
  // less than the name it replaces. A group DM gets no prefix: the backend
  // reports only whether the last line was ours, and guessing which of five
  // people said it would be a guess.
  function dmSaid(dm) {
    if (dm.dmPreviewMine) return "You: ";
    if (dm.dmNotes || (dm.dmMembers ?? 2) > 2) return "";
    const first = (dm.name || "").trim().split(/\s+/)[0];
    return first ? `${first}: ` : "";
  }

  // A conversation's clock: the time today, the weekday this week, a date
  // beyond that. The same shape a messenger's list has always used, because it
  // is the shortest string that is never ambiguous.
  function dmWhen(ms) {
    if (!ms) return "";
    const d = new Date(ms);
    const now = new Date();
    const days = Math.floor((now.setHours(0, 0, 0, 0) - new Date(ms).setHours(0, 0, 0, 0)) / 86400000);
    try {
      if (days <= 0) return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", ...clockOpts() });
      if (days === 1) return "Yesterday";
      if (days < 7) return d.toLocaleDateString([], { weekday: "short" });
      return d.toLocaleDateString([], { month: "short", day: "numeric" });
    } catch {
      return "";
    }
  }

  // Group channels under their category (uncategorized first), each group
  // ordered by the channel's position.
  const groups = $derived.by(() => {
    if (!g) return [];
    const cats = [...(g.categories || [])].sort((a, b) => a.position - b.position);
    const byCat = (id) =>
      g.channels
        .filter((c) => !c.parent && (c.category || "") === id) // threads nest under their parent (forum board or text channel)
        .sort((a, b) => a.position - b.position);
    const out = [{ id: "", name: "", channels: byCat("") }];
    for (const cat of cats) out.push({ id: cat.id, name: cat.name, channels: byCat(cat.id) });
    return out.filter((grp) => grp.channels.length || grp.id);
  });

  // A channel's unread, INCLUDING its threads: forum posts and message-started
  // threads don't appear in the sidebar, so their unread has to surface on the
  // parent's row or it's invisible. Text channels can parent threads too now,
  // so both kinds roll up; voice and announcement rows keep the cheap path.
  function channelUnread(c) {
    const own = S.unread[c.id];
    if (c.type !== "forum" && (c.type || "text") !== "text") return own;
    let count = own?.count || 0;
    let mentions = own?.mentions || 0;
    for (const t of g?.channels || []) {
      if (t.parent !== c.id || isMuted(t.id, g?.id)) continue;
      const u = S.unread[t.id];
      if (!u) continue;
      count += u.count;
      mentions += u.mentions;
    }
    return count ? { count, mentions } : null;
  }

  // Unread channels in this guild, in sidebar order — powers the jump pill and
  // Alt+↑/↓ navigation. The shortcut alone is not the feature: without a
  // visible affordance nothing tells you how many are waiting, or where.
  const unreadChannels = $derived.by(() =>
    groups
      .flatMap((grp) => grp.channels)
      .filter(
        (c) =>
          c.type !== "voice" &&
          c.id !== S.activeChannelId &&
          !isMuted(c.id, g?.id) &&
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

  const typeIcon = channelTypeIcon;

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
        label: "Open chat",
        icon: "hash",
        onClick: () => selectChannel(c.id), // view messages without joining the call
      },
      c.type === "voice" && { sep: true },
      { label: "Mark as read", icon: "check", onClick: () => markRead(c.id) },
      // Notification level, listed flat with a tick on the one in force — the
      // same shape "Change type to…" uses below, since this menu has no
      // submenus. "Use guild default" is a fourth, distinct answer: it's not a
      // level, it's declining to pin one.
      { sep: true },
      { label: "Notifications", header: true },
      {
        label: `Use guild default (${levelLabel(guildNotifLevel(g?.id))})`,
        icon: "bell",
        active: !S.notifs.channels[c.id],
        onClick: () => setChannelNotifs(c.id, null),
      },
      ...LEVELS.map((l) => ({
        label: l.label,
        icon: l.id === "none" ? "bellOff" : "bell",
        active: S.notifs.channels[c.id] === l.id,
        onClick: () => setChannelNotifs(c.id, l.id),
      })),
      { sep: true },
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
      canManageChannels && {
        label: "Rename channel",
        icon: "edit",
        onClick: () => (S.modal = { kind: "renameChannel", guildId: g.id, channelId: c.id, current: c.name }),
      },
      // Named after the DIALOG it opens, not after one of its fields. An
      // organizer looking for rate limiting had no reason to open a menu item
      // about topics, and the panel that holds slow mode was called "Edit
      // topic" the whole time.
      canManageChannels && c.type !== "voice" && {
        label: "Channel settings…",
        icon: "gear",
        onClick: () => (S.modal = { kind: "channelTopic", channel: c }),
      },
      canManageChannels && c.type === "announcement" && {
        label: "Linked channels",
        icon: "link",
        onClick: () => (S.modal = { kind: "channelLinks", channel: c }),
      },
      canManageChannels && { sep: true },
      canManageChannels && {
        label: "Delete channel",
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

  // Every channel reorders, voice included. Voice used to be excluded here,
  // which nobody noticed until channels could change type — convert a text
  // channel to voice and it silently stopped being draggable. Reordering is
  // type-agnostic (it only renumbers positions), and the participant rows
  // underneath a voice channel are siblings of the row rather than children, so
  // they don't capture the row's own drag. Threads stay out: they belong to
  // their forum, not to this list.
  function dragStart(e, c) {
    drag = { channel: c };
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", c.id); // Firefox needs data for a drag to start
    // A compact "lifted" ghost chip instead of the browser's full-row snapshot.
    const ghost = document.createElement("div");
    // Carry the channel's own glyph, not always a hash.
    const glyph = { voice: "🔊", forum: "🗂", announcement: "📣" }[c.type] || "#";
    ghost.textContent = `${glyph} ${c.name}`;
    ghost.style.cssText =
      "position:fixed;top:-100px;left:-100px;max-width:220px;overflow:hidden;" +
      "white-space:nowrap;text-overflow:ellipsis;padding:6px 14px;font-size:var(--fs-ui);" +
      "font-weight:600;color:var(--text);background:var(--bg-2);" +
      "border:1px solid var(--accent);border-radius:var(--radius-md);" +
      "box-shadow:var(--shadow-pop);pointer-events:none;";
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

  // --- Drag a person between voice channels ---------------------------------
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
      "white-space:nowrap;text-overflow:ellipsis;padding:5px 12px;font-size:var(--fs-compact);" +
      "font-weight:600;color:var(--accent-fg);background:var(--accent);" +
      "border-radius:999px;box-shadow:var(--shadow-pop);pointer-events:none;";
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
  // Clicking your own name opens the same profile card everyone else gets —
  // banner, bio, games and the now-playing card — rather than jumping straight
  // into the edit form. "Edit profile" is a button on the card.
  let meTrigger = $state(null);
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
  // — it reopens, history intact, when either side messages again. Leaving a
  // group DM removes it locally.
  function dmMenu(e, dm) {
    const isGroup = (dm.dmMembers ?? 2) > 2;
    openContextMenu(e, [
      {
        label: "Mark as read",
        icon: "check",
        onClick: () => markRead(dm.channels?.[0]?.id),
      },
      dm.channels?.[0] && {
        label: "Disappearing messages…",
        icon: "clock",
        onClick: () => (S.modal = { kind: "disappear", channelId: dm.channels[0].id }),
      },
      isGroup && {
        label: "Rename group",
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
        label: (dm.dmMembers ?? 2) > 2 ? "Leave group" : "Close DM",
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
    <!-- The banner used to be interpolated straight into this button's
         background-image — an unquoted url(…) built from a string a PEER
         sends. It now goes through Banner.svelte instead, which (a) draws
         and animates the preset templates rather than decoding an image, and
         (b) is the one place that vets an image value before it reaches a CSS
         url(). guildBannerArt() decides what is safe to paint at all: an
         unknown template id or a non-image string yields null, and the header
         simply renders without a banner. -->
    {@const art = guildBannerArt(g.banner)}
    <button
      class="guild-name guild-header"
      class:has-banner={!!art}
      class:ink-dark={art?.ink === "dark"}
      use:tooltip={{ text: g.description || g.name }}
      aria-label={g.description || g.name}
      onclick={() => (S.modal = { kind: "guildHub" })}
      oncontextmenu={(e) => openContextMenu(e, guildMenuItems(g), { title: g.name })}
    >
      {#if art}
        <Banner banner={g.banner} scrim={art.ink} tint class="gh-art" />
      {/if}
      <!-- One wrapper so the row can sit ABOVE the art layer: the art is
           absolutely positioned, and positioned boxes paint over in-flow ones
           whatever the DOM order. -->
      <span class="gh-row">
        {#if g.icon}
          <img class="g-icon" src={g.icon} alt="" />
        {/if}
        <strong>{g.name}</strong>
        <Icon name="chevron" size={13} />
      </span>
    </button>
  {:else}
    <header class="guild-name" class:dm-head={g?.kind === "dm"}>
      {#if g?.kind === "dm"}
        <!-- The bar. It was a label with a bare 13px "+" hanging off the end,
             in the one column of the app that has no guild header to give it
             weight. The action now says what it does instead of making you
             hover a plus to find out, and it is a pill rather than a ghost
             glyph, because starting a conversation is the only thing this
             column offers and it should look like an offer.
             No section glyph: the sidebar is 220px by default and a plate plus
             its gap is a third of what the title needs, which cost the word
             "messages". -->
        <span class="dm-head-title">
          <strong>Direct messages</strong>
        </span>
        <button class="dm-new" onclick={newMessage}>
          <Icon name="plus" size={13} /> New
        </button>
      {:else}
        <strong>Concord</strong>
      {/if}
    </header>
  {/if}

  <!-- Drag-to-reorder is a pointer-only enhancement; keyboard users reorder via
       the row menu's "Move to…", so the drag handlers stay off the a11y tree. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="scroll" ondragleave={listDragLeave}>
    {#if g?.kind === "dm"}
      {#if S.requests.length}
        <!-- Strangers wait here. Deliberately a quiet row, not a badge on the
             rail: a request has cost the sender nothing and must not be able to
             ring your app. -->
        <button class="dm-item requests" onclick={() => (S.modal = { kind: "requests" })}>
          <span class="dm-notes-icon"><Icon name="members" size={15} /></span>
          <span class="dm-name">Message requests</span>
          <span class="count">{S.requests.length > 99 ? "99+" : S.requests.length}</span>
        </button>
      {/if}
      {#each dms as dm (dm.id)}
        {@const active = dm.id === S.activeGuildId}
        {@const unread = dm.dmNotes ? { count: 0 } : guildUnread(dm)}
        {@const draft = draftIn(dm.channels?.[0]?.id)}
        <button
          class="dm-item"
          class:active
          class:unread={unread.count > 0 && !active}
          onclick={() => (dm.dmNotes ? selectNotes() : selectGuild(dm.id))}
          oncontextmenu={dm.dmNotes ? undefined : coarse ? (e) => e.preventDefault() : (e) => dmMenu(e, dm)}
          use:longpress={{ handler: (e) => !dm.dmNotes && dmMenu(e, dm) }}
        >
          {#if dm.dmNotes}
            <span class="dm-notes-icon"><Icon name="bubble" size={17} /></span>
          {:else if (dm.dmMembers ?? 2) > 2}
            <GroupAvatar faces={dm.dmFaces || []} size={36} />
          {:else}
            <Avatar
              name={dm.name}
              image={dm.dmPeerAvatar || dm.dmFaces?.[0]?.avatar || dm.icon}
              size={36}
              online={dm.dmPeer ? !!dm.dmPeerOnline : null}
              presence={dm.dmPeerPresence || ""}
            />
          {/if}
          <span class="dm-col">
            <span class="dm-top">
              <span class="dm-name">{dm.dmNotes ? "Notes (you)" : dm.name}</span>
              {#if !dm.dmNotes && liveChannels.has(dm.channels?.[0]?.id)}
                <!-- A DM-located event is live: the conversation IS the meeting. -->
                <span class="ch-live" use:tooltip={"A scheduled event is live in this conversation"}>
                  <i class="ch-live-dot"></i>LIVE
                </span>
              {:else if !dm.dmNotes && RADAR.unseen[dm.id]}
                <!-- They put something on your shared calendar — same nudge the
                     guild pill wears in the rail. Cleared by opening the calendar. -->
                <span
                  class="ev-dot"
                  role="img"
                  use:tooltip
                  aria-label="New event on this conversation's calendar"
                ></span>
              {:else if dm.dmPreviewAt}
                <span class="dm-when">{dmWhen(dm.dmPreviewAt)}</span>
              {/if}
            </span>
            <span class="dm-sub">
              {#if draft}
                <em class="dm-draft">Draft:</em> {draft}
              {:else if dm.dmPreview}
                {#if dmSaid(dm)}<span class="dm-said">{dmSaid(dm)}</span>{/if}{dm.dmPreview}
              {:else}
                <span class="dm-quiet">{dm.dmNotes ? "Anything you want to keep" : "No messages yet"}</span>
              {/if}
            </span>
          </span>
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
        <!-- "Jump to unread" is usually a shortcut and nothing else. This SHOWS
             what's waiting, where, and lets one click walk it. -->
        <button
          class="unread-jump"
          class:mention={unreadMentions > 0}
          use:tooltip={"Jump to the next unread channel (mentions first) · Alt+Shift+↓"}
          aria-label="Jump to the next unread channel (mentions first) · Alt+Shift+↓"
          onclick={jumpToNextUnread}
        >
          <span class="uj-dot"></span>
          <!-- Short on purpose. "3 channels unread · 2 @you" needs 196px in
               Gruvbox's mono face and gets 168, so the pack that packs tightest
               was the one that truncated. The tooltip and aria-label still
               carry the whole sentence. -->
          <span class="uj-text">
            {unreadChannels.length} unread
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
            class="add-locked tap-hit"
            use:tooltip={"You need the Manage Channels permission to add channels"}
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
            {#if renamingCat === grp.id}
              <!-- svelte-ignore a11y_autofocus -->
              <input
                class="cat-edit"
                autofocus
                value={grp.name}
                maxlength="40"
                aria-label="Rename category {grp.name}"
                onkeydown={(e) => {
                  if (e.key === "Enter") commitCatRename(grp, e.currentTarget.value);
                  if (e.key === "Escape") renamingCat = "";
                }}
                onblur={(e) => commitCatRename(grp, e.currentTarget.value)}
              />
            {:else}
              <!-- The header is the collapse control. The first thing that
                   happens to a healthy community is thirty channels, and a
                   sidebar that can never fold is unnavigable long before
                   that. -->
              <button
                class="cat-toggle"
                aria-expanded={!collapsed(grp.id)}
                onclick={() => toggleCat(grp.id)}
                ondblclick={() => canManageChannels && (renamingCat = grp.id)}
                oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => categoryMenu(e, grp)}
                use:longpress={{ handler: (e) => categoryMenu(e, grp) }}
              >
                <span class="cat-caret" class:folded={collapsed(grp.id)}><Icon name="chevron" size={11} /></span>
                <span class="cat-label">{grp.name}</span>
                {#if collapsed(grp.id)}
                  <span class="cat-count">{grp.channels.length}</span>
                {/if}
              </button>
              {#if canManageChannels}
                <span class="cat-actions">
                  <button
                    class="cat-add"
                    use:tooltip
                    aria-label="Create channel in {grp.name}"
                    onclick={() => (S.modal = { kind: "channel", category: grp.id })}
                  >
                    <Icon name="plus" size={12} />
                  </button>
                  <button
                    class="cat-add"
                    use:tooltip
                    aria-label="Rename category {grp.name}"
                    onclick={() => (renamingCat = grp.id)}
                  >
                    <Icon name="edit" size={12} />
                  </button>
                  <button
                    class="cat-add"
                    use:tooltip
                    aria-label="Delete category {grp.name}"
                    onclick={() => deleteCategory({ id: grp.id, name: grp.name })}
                  >
                    <Icon name="trash" size={12} />
                  </button>
                </span>
              {/if}
            {/if}
          </div>
        {/if}
        {#each collapsed(grp.id) ? grp.channels.filter((c) => keepVisible(c)) : grp.channels as c (c.id)}
          {@const u = channelUnread(c)}
          {@const active = c.id === S.activeChannelId}
          {@const inVoice = S.voice && S.voice.channelId === c.id}
          {@const joiningHere = S.joiningVoice === c.id}
          {@const occupied = c.type === "voice" && voiceMembersFor(c.id).length > 0}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="channel-row"
            data-ch-id={c.id}
            class:active
            class:unread={!!u && !active && !isMuted(c.id, g?.id)}
            class:mentioned={!!u?.mentions && !active && !isMuted(c.id, g?.id)}
            class:voice-active={inVoice}
            class:voice-joining={joiningHere}
            class:vdrop={vDropId === c.id}
            ondragleave={() => (vDropId === c.id ? (vDropId = "") : null)}
            class:dragging={drag?.channel.id === c.id}
            class:drop-before={dropHint?.rowId === c.id && dropHint.edge === "before"}
            class:drop-after={dropHint?.rowId === c.id && dropHint.edge === "after"}
            draggable={canManageChannels && !c.parent}
            ondragstart={(e) => dragStart(e, c)}
            ondragend={dragEnd}
            ondragover={(e) => rowDragOver(e, grp, c)}
            ondrop={(e) => rowDrop(e, grp, c)}
          >
            <button
              class="channel"
              class:muted-ch={isMuted(c.id, g?.id)}
              onclick={() => clickChannel(c)}
              oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => channelMenu(e, c)}
              use:longpress={{ handler: (e) => channelMenu(e, c) }}
            >
              <Icon name={typeIcon(c.type)} size={13} />
              <span class="ch-name">{c.name}</span>
              {#if Number(c.slowMode) > 0}
                <!-- A paced room says so where you choose which room to enter,
                     not only once you are in it and the composer refuses. -->
                <span
                  class="ch-slow"
                  role="img"
                  use:tooltip={`Slow mode: one message every ${slowShort(c.slowMode)}`}
                  aria-label={`Slow mode: one message every ${slowShort(c.slowMode)}`}
                ><Icon name="clock" size={11} /></span
                >
              {/if}
              {#if c.type === "voice" && isCallLocked(c.id)}
                <span class="ch-lock" role="img" use:tooltip={"Call locked — knock to join"} aria-label="Call locked — knock to join"
                  ><Icon name="lock" size={11} /></span
                >
              {/if}
              {#if liveChannels.has(c.id)}
                <!-- A scheduled event is happening IN here right now — the
                     channel wears it, so the meeting is findable even after
                     the go-live banner is gone. -->
                <span
                  class="ch-live"
                  use:tooltip={"A scheduled event is live in here — join in"}
                >
                  <i class="ch-live-dot"></i>LIVE
                </span>
              {/if}
              {#if !active && draftIn(c.id)}
                <!-- Something unsent lives here. The draft already survived the
                     switch and the reload; the row said nothing about it, which
                     is how a half-written thought gets lost. -->
                <span
                  class="ch-draft"
                  role="img"
                  use:tooltip={"You have an unsent draft in here"}
                  aria-label="Unsent draft"
                >
                  <Icon name="edit" size={11} />
                </span>
              {/if}
              {#if c.type !== "voice" && !active && u && !isMuted(c.id, g?.id)}
                <span class="count" class:mention={u.mentions > 0}>
                  {u.mentions > 0 ? (u.mentions > 99 ? "99+" : u.mentions) : u.count > 99 ? "99+" : u.count}
                </span>
              {/if}
              {#if joiningHere}
                <!-- Pressed, and saying why. A click on a voice row opens your
                     microphone and then waits on the node; without this the row
                     looked untouched for the whole wait, which is what made
                     people click it again. -->
                <span class="ch-joining">Connecting…</span>
              {:else if occupied}
                <!-- How many, then the equalizer. The row said only "somebody is
                     in here" — you had to read the participant list under it to
                     find out whether that was one person or six, and in a
                     collapsed category there is no list to read. -->
                <span class="ch-heads" aria-label="{voiceMembersFor(c.id).length} in this call">
                  {voiceMembersFor(c.id).length}
                </span>
                <span class="eq" class:you={inVoice} aria-hidden="true"><i></i><i></i><i></i></span>
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
                  <button
                    class="menu-item"
                    onclick={() => (S.modal = { kind: "renameChannel", guildId: g.id, channelId: c.id, current: c.name })}
                  >
                    <Icon name="edit" size={13} /> Rename channel
                  </button>
                  <button class="menu-item danger" onclick={() => deleteChannel(c)}>
                    <Icon name="trash" size={13} /> Delete channel
                  </button>
                </Menu>
              </span>
            {/if}
            {#if c.type !== "voice"}
              <button
                class="mute-btn"
                use:tooltip
                aria-label={isMuted(c.id, g?.id) ? "Unmute channel" : "Mute channel"}
                onclick={() => {
                  toggleMute(c.id);
                  haptic("light"); // a bell that silently flips needs an answer in the hand
                }}
              >
                <Icon name={isMuted(c.id, g?.id) ? "bellOff" : "bell"} size={13} />
              </button>
            {:else if inVoice}
              <!-- Nothing to mute in a voice channel; the bell's footprint is
                   kept either way, or the row's chevron slides 33px right of
                   every other row's — a ragged edge in an otherwise even
                   column. In a voice room it holds the way in instead. -->
              <span class="mute-slot" aria-hidden="true"></span>
            {:else}
              <!-- The row has always joined on click, and still does. This says
                   so before you commit a microphone to it, and is the only
                   keyboard-reachable way in that isn't "press Enter and find
                   out". -->
              <button
                class="join-btn"
                aria-label="Join {c.name}"
                onclick={(e) => {
                  e.stopPropagation();
                  onJoinVoice?.(c.id);
                }}
              >
                Join
              </button>
            {/if}
          </div>
          {#if c.type === "voice"}
            {#each voiceMembersFor(c.id) as vm (vm.peerId)}
              {@const vmem = memberByFpr(vm.fingerprint)}
              {@const vcTip = canDragPerson(vm)
                ? `${nameFor(vm.fingerprint)} — drag to another voice channel`
                : nameFor(vm.fingerprint)}
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
                use:tooltip={{ text: vcTip }}
                aria-label={vcTip}
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
                <span class="vc-name">
                  {nameFor(vm.fingerprint)}{vm.self
                    ? " (you)"
                    : vm.otherDevice
                      ? " (your other device)"
                      : ""}
                </span>
                {#if vm.deafened}
                  <span class="vc-mark" role="img" use:tooltip={"Deafened"} aria-label="Deafened"
                    ><Icon name="deafened" size={11} /></span
                  >
                {:else if vm.muted}
                  <span class="vc-mark" role="img" use:tooltip={"Muted"} aria-label="Muted"
                    ><Icon name="micOff" size={11} /></span
                  >
                {/if}
                {#if vm.sharing}
                  <span
                    class="vc-live"
                    role="img"
                    use:tooltip={{ text: vm.self ? "You're sharing" : "Live — click to watch" }}
                    aria-label={vm.self ? "You're sharing" : "Live — click to watch"}
                  >
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

  <!-- Persistent bottom-left call controller: shows whenever
       you're in a call, whatever you're viewing. When you're on the call's own
       channel the VoicePanel also shows full controls — that's fine, this is the
       always-there compact bar; when you navigate away the FloatingCall dock
       joins it too. -->
  {#if S.voice}
    <div class="voice-bar">
      <button
        class="vb-info"
        use:tooltip
        aria-label="Return to call"
        onclick={() => jumpToChannel(S.voice.channelId)}
      >
        <span class="vb-live"></span>
        <span class="vb-text">
          <strong>
            Voice connected
            <!-- The elapsed time doubles as proof the call is alive: a clock
                 that has stopped is a call that has died. -->
            {#if clock}<span class="vb-clock">{clock}</span>{/if}
          </strong>
          <span class="muted vb-ch">{channelShort(S.voice.channelId)}</span>
        </span>
      </button>
      <span class="vb-actions">
        <!-- Deafen was missing here and on the header pill, so two of the four
             places you can mute offered a different SET of controls as well as a
             different look. Same glyphs, same 15px, same round plate, and the
             same rule about colour everywhere: mic and deafen light danger when
             they are stopping something, camera and screen light green when
             they are sending it. This bar had mic lighting green for muted,
             which said the opposite of what it meant. -->
        <button
          class="vb-btn cut"
          class:on={S.muted}
          use:tooltip
          aria-label={S.muted ? "Unmute" : "Mute"}
          aria-pressed={S.muted}
          onclick={onToggleMute}
        >
          <Icon name={S.muted ? "micOff" : "mic"} size={15} />
        </button>
        <button
          class="vb-btn cut"
          class:on={S.deafened}
          use:tooltip
          aria-label={S.deafened ? "Undeafen" : "Deafen"}
          aria-pressed={S.deafened}
          onclick={onToggleDeafen}
        >
          <Icon name={S.deafened ? "deafened" : "speaker"} size={15} />
        </button>
        <button
          class="vb-btn"
          class:on={S.cameraOn}
          use:tooltip
          aria-label={S.cameraOn ? "Turn off camera" : "Turn on camera"}
          aria-pressed={S.cameraOn}
          onclick={onToggleCamera}
        >
          <Icon name={S.cameraOn ? "cameraOff" : "camera"} size={15} />
        </button>
        {#if canShareScreen}
          <button
            class="vb-btn"
            class:on={S.sharing}
            use:tooltip
            aria-label={S.sharing ? "Stop sharing" : "Share screen"}
            aria-pressed={S.sharing}
            onclick={onToggleShare}
          >
            <Icon name={S.sharing ? "screenOff" : "screen"} size={15} />
          </button>
        {/if}
        <button class="vb-btn leave" use:tooltip aria-label="Disconnect" onclick={onLeaveVoice}>
          <Icon name="door" size={15} />
        </button>
      </span>
    </div>
  {/if}

  <div class="me-row">
    <button
      bind:this={statusTrigger}
      class="me-status-trigger"
      use:tooltip
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
        decoration={S.identity.style?.dec || ""}
        dc={S.identity.style?.dc || ""}
      />
    </button>
    <button
      bind:this={meTrigger}
      class="me"
      onclick={() => openProfilePopover(S.identity.fingerprint, meTrigger)}
      use:tooltip={"Your profile"}
      aria-label="Your profile"
    >
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
    <button class="me-gear ghost" use:tooltip aria-label="Settings" onclick={() => (S.modal = { kind: "settings" })}>
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
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: var(--fs-body);
  }
  /* DM column title carries the "New message" + on the right (no separate
     "Direct messages" section header below — that was a duplicate label). */
  .guild-name.dm-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    overflow: visible;
  }
  .dm-head-title {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
    overflow: hidden;
  }
  /* The title gives before the action does, and it ellipsises rather than being
     cut mid-word by the header's own clip. */
  .dm-head-title strong {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dm-new {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    flex: none;
    padding: 4px 9px 4px 6px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
    font-size: var(--fs-compact);
    cursor: pointer;
    transition:
      background var(--dur-standard) ease,
      color var(--dur-standard) ease;
  }
  .dm-new:hover {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .guild-header {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    background: transparent;
    color: var(--text);
    text-align: left;
    border-radius: 0;
  }
  @media (pointer: fine) {
    .guild-header:hover {
      background: var(--bg-3);
    }
  }
  .guild-header:active {
    background: var(--bg-3);
  }
  .guild-header.has-banner {
    position: relative;
    color: #fff;
    min-height: 56px;
    align-items: flex-end;
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  }
  /* The pale templates (Linen Press) ask for dark ink; Banner.svelte flips its
     scrim to match, so the pair stays readable together. */
  .guild-header.has-banner.ink-dark {
    color: #12161a;
    text-shadow: 0 1px 2px rgba(255, 255, 255, 0.65);
  }
  .guild-header :global(.gh-art) {
    position: absolute;
    inset: 0;
  }
  /* A banner header is a big button: it still has to answer the cursor, and it
     can't do that with a background colour any more. */
  .guild-header.has-banner:hover :global(.gh-art) {
    filter: brightness(1.14);
  }
  .gh-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex: 1;
    min-width: 0;
  }
  .guild-header strong {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-body);
  }
  .g-icon {
    width: 26px;
    height: 26px;
    border-radius: var(--radius-md);
    object-fit: cover;
    flex-shrink: 0;
  }
  .scroll {
    flex: 1;
    overflow-y: auto;
    /* A fling that runs out of channel list must not continue into the message
       feed showing through the open drawer. */
    overscroll-behavior: contain;
    /* Decorative avatar rings (which spin OUTSIDE the avatar box) must not
       make this column think it needs a horizontal scrollbar — that's what
       made the divider "pulsate". clip keeps them visible; hidden would too,
       but clip doesn't create a scroll port. */
    overflow-x: clip;
    padding: var(--sp-2);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .section-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    text-transform: uppercase;
    font-size: var(--fs-small);
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
    font-size: var(--fs-tiny);
    letter-spacing: 0.06em;
    color: var(--text-faint);
    font-weight: 700;
    /* Same 6px inset as .section-head — the two headings stack in one column
       and a 2px difference reads as a wobble. */
    margin: 10px 6px 2px;
  }
  /* The header is the fold control, so it has to BE a button and fill the
     row — a caret you have to hit exactly is worse than no caret. */
  .cat-toggle {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
    text-align: left;
  }
  .cat-toggle:hover {
    color: var(--text-muted);
  }
  /* The glyph is a right-pointing chevron, so OPEN is the rotated state: it
     points down at what it is showing, and folds back to pointing at the rows
     it is hiding. */
  .cat-caret {
    display: inline-grid;
    place-items: center;
    transform: rotate(90deg);
    transition: transform var(--dur-quick) var(--ease-out);
  }
  .cat-caret.folded {
    transform: none;
  }
  .cat-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* What folding cost you, so the number is never a surprise. */
  .cat-count {
    padding: 0 5px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-faint);
    font-size: var(--fs-micro);
  }
  .cat-edit {
    flex: 1;
    min-width: 0;
    padding: 2px 6px;
    font-size: var(--fs-tiny);
    letter-spacing: inherit;
    text-transform: uppercase;
    font-weight: 700;
  }
  .ch-slow {
    flex: none;
    display: inline-grid;
    place-items: center;
    color: var(--warn-text);
    opacity: 0.85;
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
  /* Pressed while the join is in flight: the same lit background as being IN
     the call, pulsing, so the row is obviously doing something. */
  .voice-joining {
    background: var(--accent-soft) !important;
    animation: ch-joining 1.4s ease-in-out infinite;
  }
  @keyframes ch-joining {
    50% {
      opacity: 0.6;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .voice-joining {
      animation: none;
    }
  }
  .ch-joining {
    margin-left: auto;
    flex-shrink: 0;
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    white-space: nowrap;
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
    font-size: var(--fs-compact);
    text-align: left;
  }
  @media (pointer: fine) {
    .vc-member:hover {
      background: var(--bg-3);
      color: var(--text);
    }
  }
  .vc-member:active {
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
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--danger) 20%, transparent);
    color: var(--danger-text);
    font-size: var(--fs-micro);
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
  /* A live scheduled event in this channel/DM. Same chip anatomy as the
     screen-share LIVE above, but in --ok: a meeting is an invitation, not an
     alarm — red stays reserved for mentions and broadcasts. */
  .ch-live {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 1px 5px;
    border-radius: var(--radius-sm);
    background: var(--ok-soft);
    color: var(--ok-text);
    font-size: var(--fs-micro);
    font-weight: 800;
    letter-spacing: 0.04em;
    flex-shrink: 0;
  }
  .ch-live-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--ok);
    animation: live-pulse 1.4s ease-in-out infinite;
  }
  /* Unseen new/changed event on a DM's calendar — a quiet accent dot, the
     same voice the guild pill uses in the rail. */
  .ev-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 6px color-mix(in srgb, var(--accent) 55%, transparent);
    flex-shrink: 0;
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
    .live-dot,
    .ch-live-dot {
      animation: none;
    }
  }
  .channel-row {
    position: relative;
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
    transition: background var(--dur-standard) ease;
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
    animation: nub-in 0.25s var(--ease-spring);
  }
  @keyframes nub-in {
    from {
      transform: translateY(-50%) scaleY(0.2);
    }
  }
  /* Hover is a mouse state: a tap synthesises it and it STICKS, so every row a
     finger passed over keeps a highlight that reads as a stuck selection. */
  @media (pointer: fine) {
    .channel-row:hover {
      background: var(--bg-3);
    }
  }
  .channel-row:active {
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
    color: var(--accent-hover);
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
    font-size: var(--fs-ui);
    text-align: left;
    min-width: 0;
    border-radius: var(--radius-sm);
    /* The row's ground already eases; its ink did not, so the name snapped to
       full brightness a beat before the background caught up. */
    transition: color var(--dur-standard) ease;
  }
  .channel:hover,
  .channel:active {
    background: transparent;
    color: var(--text);
  }
  /* Rows glide a hair right on hover — the list feels sprung, not painted. */
  .channel,
  .dm-item {
    transition:
      background var(--dur-standard) ease,
      color var(--dur-standard) ease,
      transform var(--dur-standard) ease;
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
    color: var(--ok-text);
  }
  .eq.you {
    color: var(--accent-hover);
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
  /* Quiet: it is a reminder, not an alert. A draft is something YOU left, so
     it never competes with the unread count beside it. */
  .ch-draft {
    display: inline-grid;
    place-items: center;
    flex: none;
    color: var(--text-faint);
  }
  .channel-row:hover .ch-draft,
  .channel-row.active .ch-draft {
    color: var(--accent-hover);
  }
  .ch-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* ---- unread & mentions ----------------------------------------------
     The usual unread cue is a white bar and a bold name. This does that AND
     tints the bar (accent = unread, danger = @you), tints the count pill to
     match, and keeps the whole row a touch brighter so it reads at a glance. */
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
    animation: unread-in 0.22s var(--ease-spring);
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
    border-radius: var(--radius-md);
    background: var(--text-muted);
    color: var(--bg-1);
    font-size: var(--fs-tiny);
    font-weight: 700;
    display: grid;
    place-items: center;
    animation: count-pop 0.25s var(--ease-spring) both;
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
    color: var(--danger-fg);
  }
  .ch-menu {
    display: inline-flex;
    opacity: 0;
  }
  .channel-row:hover .ch-menu {
    opacity: 1;
  }
  /* ---- reclaiming the hover gutter (mouse only) ----
     The chevron and the bell are hover affordances, but they were laid out all
     the time: two controls holding ~47px of a 240px rail open for a moment that
     may never come, and the channel's own NAME paying for it in ellipsis. Names
     are the thing this list is FOR.
     Out of flow they cost nothing until they appear, and when they do they are
     drawn over the tail of a name that is being truncated anyway. The count
     pill steps aside for the same reason — the row is under the cursor, so the
     thing being read right now is the control, not the badge.
     Mouse only, deliberately: on touch these are permanently visible (see the
     coarse block below) and out of flow they would sit on top of the name for
     good. */
  @media (pointer: fine) {
    .ch-menu,
    .mute-btn {
      position: absolute;
      top: 50%;
      transform: translateY(-50%);
    }
    /* Bell outermost, chevron inside it — the order they sit in while in flow,
       so the two layouts do not swap places at the moment of hover. */
    .mute-btn {
      right: 2px;
    }
    .ch-menu {
      right: 26px;
    }
    .mute-slot {
      display: none;
    }
    .channel-row:hover .count {
      opacity: 0;
    }
    .count {
      transition: opacity var(--dur-quick) ease;
    }
  }
  .menu-head {
    font-size: var(--fs-tiny);
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
  .mute-slot {
    flex: none;
    width: 25px; /* 13px glyph + .mute-btn's 6px sides */
  }
  .channel-row:hover .mute-btn,
  .mute-btn:focus-visible {
    opacity: 1;
  }
  .mute-btn:hover {
    background: transparent;
    color: var(--text);
  }
  /* Says what a click on the row will do, in the slot the bell would occupy —
     so a voice row keeps the same right edge as every other row whether the
     button is showing or not. */
  .join-btn {
    flex: none;
    padding: 2px 8px;
    background: var(--accent-soft);
    color: var(--accent-hover);
    border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
    border-radius: 999px;
    font-size: var(--fs-micro);
    font-weight: 700;
    opacity: 0;
  }
  .channel-row:hover .join-btn,
  .join-btn:focus-visible {
    opacity: 1;
  }
  .join-btn:hover {
    background: var(--accent);
    color: var(--accent-fg);
  }
  /* Coarse pointers have no hover to reveal it with, and a permanently visible
     pill on every voice row would shout over the channel names. The row itself
     is the target there, at the 48px floor. */
  @media (pointer: coarse) {
    .join-btn {
      display: none;
    }
  }
  /* How many people are in the room, beside the equalizer that says somebody
     is. */
  .ch-heads {
    flex: none;
    min-width: 16px;
    padding: 0 5px;
    text-align: center;
    font-size: var(--fs-micro);
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: var(--ok-text);
    background: var(--ok-soft);
    border-radius: 999px;
  }
  /* Gentle empty states for "no DMs yet" / "no guilds yet": a soft icon chip,
     one line of copy, and the single action that fixes it. */
  .empty-block {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: var(--sp-2);
    margin: 14px 6px;
    padding: var(--sp-4) var(--sp-3);
    border: 1px dashed var(--border);
    border-radius: var(--radius-md);
  }
  .empty-block p {
    margin: 0;
    font-size: var(--fs-compact);
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
    font-size: var(--fs-ui);
    font-weight: 600;
    border-radius: var(--radius-sm);
  }
  /* A DM row is TWO lines, and the second one is the point. The list used to be
     a bare column of names — no snippet, no time, no unread, no presence line —
     so a conversation ending in "See you at 19:00." reached the list as the
     word "Bilal Rahman", and sorting by recency (a settled decision) was
     invisible because there was nothing on screen to sort BY. The height comes
     with the content: a 36px face and two lines is the density every messenger
     converged on because it is what one glance needs. */
  .dm-item {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    padding: var(--sp-2);
    background: transparent;
    color: var(--text-muted);
    text-align: left;
    border-radius: var(--radius-md);
    transition:
      background var(--dur-standard) ease,
      color var(--dur-standard) ease;
  }
  .dm-col {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .dm-top {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
  }
  .dm-when {
    margin-left: auto;
    flex: none;
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    font-variant-numeric: tabular-nums;
  }
  .dm-sub {
    display: block;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-compact);
    color: var(--text-faint);
    /* Somebody else's words in the app's own furniture — resolve them on their
       own, the way every other quoted name and body in the app is. */
    unicode-bidi: plaintext;
  }
  .dm-said {
    color: var(--text-muted);
  }
  /* An unsent message is the one thing in this row that is about YOU, so it is
     the one thing that gets the accent. */
  .dm-draft {
    color: var(--accent-hover);
    font-style: normal;
    font-weight: 600;
  }
  .dm-quiet {
    font-style: italic;
  }
  @media (pointer: fine) {
    .dm-item:hover {
      background: var(--bg-3);
      color: var(--text);
    }
  }
  .dm-item:active {
    background: var(--bg-3);
    color: var(--text);
  }
  .dm-item.active {
    background: var(--accent-soft);
    color: var(--text);
  }
  .dm-name {
    min-width: 0; /* let it shrink so long group-DM names ellipsize, not overflow */
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-ui);
    unicode-bidi: plaintext;
  }
  .dm-item.unread {
    color: var(--text);
  }
  .dm-item.unread .dm-name {
    font-weight: 600;
  }
  .dm-item.unread .dm-sub {
    color: var(--text-muted);
  }
  .dm-notes-icon {
    width: 36px;
    height: 36px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent-hover);
    flex-shrink: 0;
  }
  /* Requests are neutral, not accented: a stranger knocking should read as
     something to deal with, never as something new and exciting. */
  .dm-item.requests .dm-notes-icon {
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .dm-item.requests .count {
    background: var(--bg-3);
    color: var(--text-muted);
  }
  /* This bar's entire job is to say "you are in a call, and it's here", and it
     was losing that sentence to its own buttons: "Voice connected" needs 104px
     and had 49, at 1280, 1440 and 1920 alike, because four icons and a label
     were sharing one line of a fixed 210px rail. Any layout where the status
     text loses to its own controls is the wrong layout — so the text takes the
     row and the controls take the next one, which is what the phone tier had
     already worked out for itself. */
  .voice-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    row-gap: 6px;
    gap: 6px;
    padding: 8px 10px;
    margin: 0 8px;
    border-radius: var(--radius-md);
    background: var(--ok-soft);
    color: var(--ok-text);
    border: 1px solid color-mix(in srgb, var(--ok) 28%, transparent);
    box-shadow: 0 0 14px color-mix(in srgb, var(--ok) 10%, transparent);
  }
  .vb-info {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
    flex: 1 0 100%;
    background: transparent;
    color: var(--ok-text);
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
    display: flex;
    align-items: baseline;
    gap: 6px;
    font-size: var(--fs-compact);
    /* "Voice connected" is two words, and a rail dragged narrow will break it
       across two lines and push the mic button off its own row. */
    overflow: hidden;
    white-space: nowrap;
  }
  /* Tabular figures so the seconds don't shuffle the label sideways every
     tick, and a hair dimmer than the words it follows: the clock is evidence,
     not a heading. */
  .vb-clock {
    margin-left: auto;
    padding-left: 6px;
    font-variant-numeric: tabular-nums;
    font-weight: 600;
    opacity: 0.8;
  }
  .vb-ch {
    font-size: var(--fs-small);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .vb-actions {
    display: flex;
    flex: 1 0 100%;
    justify-content: space-between;
    gap: 2px;
  }
  /* Round, like the same control on the other three surfaces. This was the one
     square-cornered call button in the app. */
  .vb-btn {
    background: transparent;
    color: var(--ok-text);
    padding: 5px;
    display: grid;
    place-items: center;
    border-radius: 50%;
  }
  @media (pointer: fine) {
    .vb-btn:hover {
      background: color-mix(in srgb, var(--ok) 22%, transparent);
    }
  }
  .vb-btn:active {
    background: color-mix(in srgb, var(--ok) 22%, transparent);
  }
  .vb-btn.on {
    background: var(--ok);
    color: var(--ok-fg);
  }
  .vb-btn.cut.on {
    background: var(--danger);
    color: var(--danger-fg);
  }
  @media (pointer: fine) {
    .vb-btn.cut.on:hover {
      background: color-mix(in srgb, var(--danger) 82%, white);
    }
  }
  .vb-btn.leave {
    color: var(--danger-text);
  }
  .vb-btn.leave:hover {
    background: var(--danger-soft);
  }
  .me-row {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    /* Pad past the phone's bottom system bar (Android gesture/button bar, iOS
       home indicator) — without this, edge-to-edge draw (viewport-fit=cover)
       lets the OS nav overlap the self row and its settings/sign-out button.
       env() is 0 on desktop, so this is a no-op there. */
    /* No safe-area inset: on a phone this row lives inside the left drawer,
       which pads its own bottom, and on desktop the inset is 0 anyway. Adding it
       here stacked the gesture-bar gap twice inside the drawer. */
    padding: var(--sp-2);
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
      background var(--dur-quick) ease,
      transform var(--dur-standard) var(--ease-spring);
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
    margin-right: var(--sp-1);
    vertical-align: baseline;
    color: var(--accent-hover);
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
  @media (pointer: fine) {
    .me:hover {
      background: var(--bg-3);
    }
  }
  .me:active {
    background: var(--bg-3);
  }
  .me-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: var(--fs-ui);
  }
  .me-text strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .small-status {
    font-size: var(--fs-small);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .me-gear {
    padding: var(--sp-2);
    border: none;
  }
  /* Touch: taller rows (44px+ targets), slightly larger type, and the
     hover-revealed affordances (category +/trash, channel menu, mute bell)
     always visible at reduced opacity — hover doesn't exist on a phone. */
  @media (pointer: coarse), (max-width: 768px) {
    .channel {
      min-height: var(--tap-min);
      font-size: var(--fs-body);
    }
    .dm-item {
      min-height: 48px;
    }
    .dm-name {
      font-size: var(--fs-body);
    }
    /* The one-tap way to walk unreads — the control this column exists to offer
       on a phone — was the only affordance the touch block never grew. */
    .unread-jump {
      min-height: var(--tap-min);
      font-size: var(--fs-ui);
    }
    .count {
      height: 18px;
      min-width: 20px;
    }
    /* A badge is already the smallest ink on the screen; tracking it out on top
       is what turns it from small into unreadable. */
    .vc-live {
      letter-spacing: 0;
      padding: 2px 6px;
    }
    /* No hover on a phone, so this dimming is the FINAL state, not a resting
       one — at 0.55 the glyphs sat under 2:1. --text-faint already says
       "quiet"; the opacity was saying "invisible" on top of it. (.always keeps
       its own 0.7 on desktop, and that has to go the same way here.) */
    .cat-add,
    .cat-add.always,
    .ch-menu,
    .mute-btn {
      opacity: 1;
    }
    .mute-btn {
      padding: 10px 12px;
      /* Invisible overlay pads the tap area out to ~44px. */
      position: relative;
    }
    .mute-btn::after {
      content: "";
      position: absolute;
      /* Right-biased: the channel-options chevron is immediately to the left
         and carries its own overlay, so growing leftward would put two hit
         boxes on top of each other. Vertically it fills the row instead.
         The button measures 37x39, so these are the exact numbers that reach
         Android's 48dp floor without a leftward centimetre: 39+5+5 and 37+11,
         the extra width taken out of the row's own end padding. Hand-placed
         rather than calc()'d because the left side must stay pinned at 0. */
      inset: -5px -11px -5px 0;
    }
    .mute-slot {
      width: 37px; /* tracks .mute-btn's touch padding above */
    }
    /* The category +/trash render at 16px. Spread them apart first — they sit
       side by side and the right one deletes the category, so overlapping tap
       areas would let a miss on "add channel" destroy the whole category. */
    .cat-actions {
      /* 28px box + 16px gap = 44px between centres, so the two 44px tap areas
         abut instead of overlapping. */
      gap: var(--sp-4);
    }
    .cat-add {
      padding: var(--sp-2);
      position: relative;
    }
    .cat-add::after {
      content: "";
      position: absolute;
      inset: -8px;
    }
    /* Room for that overlay inside the header. Without it the bottom 4px fell
       under the next channel row, and the row would win the tap — a miss on
       "add channel" is a hair from "delete category". */
    .cat-head {
      min-height: 44px;
    }
    /* Participant rows abut the channel row above them with no gap, and each
       one is BOTH a tap target (joins the call) and a long-press target
       (moderation). A slightly low tap on "#general" landing on the first
       participant joined the call, so they get the full target plus a hairline
       of separation from the row that owns them. */
    .vc-member {
      min-height: var(--tap-min);
    }
    .channel-row + .vc-member {
      margin-top: var(--sp-1);
    }
    /* The two-row arrangement is the base layout now (see .voice-bar); the
       phone only widens the gutter between finger-sized targets. */
    .vb-actions {
      gap: var(--sp-1);
    }
    .vb-btn {
      min-width: 44px;
      min-height: 44px;
    }
    .me {
      min-height: 44px;
    }
    .me-gear {
      min-width: 44px;
      min-height: 44px;
    }
    /* Invisible overlay pads the avatar/status tap area out to the 48dp floor.
       Biased left and vertical, never right: .me sits immediately to the right
       with its own 44px target and opens a different thing, so a symmetric
       overlay would hand four pixels of "set status" to "open profile". The
       left edge is the drawer's own padding, where nothing else is listening. */
    .me-status-trigger {
      position: relative;
    }
    .me-status-trigger::after {
      content: "";
      position: absolute;
      inset: calc((var(--tap-min) - 100%) / -2) 0 calc((var(--tap-min) - 100%) / -2)
        calc(100% - var(--tap-min));
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
  /* Unread summary pill: what's waiting and where, without a shortcut. */
  .unread-jump {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    margin: 0 0 6px;
    padding: 7px 10px;
    border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
    border-radius: 999px;
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    color: var(--text);
    font-size: var(--fs-compact);
    cursor: pointer;
    animation: uj-in 0.25s ease;
    transition:
      background var(--dur-quick) ease,
      border-color var(--dur-quick) ease;
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
  @media (pointer: fine) {
    .unread-jump:hover {
      background: color-mix(in srgb, var(--accent) 22%, transparent);
      border-color: var(--accent);
    }
  }
  .unread-jump:active {
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
    font-size: var(--fs-small);
    font-weight: 700;
    color: var(--accent-hover);
    letter-spacing: 0.02em;
  }
  .unread-jump.mention .uj-cta {
    color: var(--danger-text);
  }
</style>
