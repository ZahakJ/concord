<script module>
  // "Later" on the recovery-phrase nudge lasts the session, not the mount: the
  // sidebar remounts on every shell change and the phrase must not nag back
  // the moment you resize a window.
  const phraseLater = $state({ on: false });
</script>

<script>
  // The sidebar (§2.3): the place header, the shortcut strip, the jump row,
  // the one nudge, the place's list (channels by category, or your
  // conversations), the unread-messages block, the call dock and you.
  import Icon from "./Icon.svelte";
  import OwnerMark from "./OwnerMark.svelte";
  import { ownsPlace, ownerLabel } from "./lib/ownermark.js";
  import GuildMark from "./GuildMark.svelte";
  import Badge from "./Badge.svelte";
  import Nudge from "./Nudge.svelte";
  import Kbd from "./Kbd.svelte";
  import GuildRail from "./GuildRail.svelte";
  import SetupCard from "./SetupCard.svelte";
  import Avatar from "./Avatar.svelte";
  import GroupAvatar from "./GroupAvatar.svelte";
  import Menu from "./Menu.svelte";
  import StatusPopover from "./StatusPopover.svelte";
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
    notifLevel,
    setChannelNotifs,
    setGuildNotifs,
    guildNotifLevel,
    channelShort,
    callHealth,
    voiceMembersFor,
    nameFor,
    memberByFpr,
    guildUnread,
    moveChannelToCategory,
    reorderChannel,
    openCallStage,
    closeCallStage,
    toggleCallStageChat,
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
    openInbox,
    openDMs,
    dismissNotifyAsk,
    dismissUpdate,
    openGuildPage,
    openGuildHub,
    leaveGuildLabel,
    confirmLeaveGuild,
  } from "./lib/state.svelte.js";
  import { confirmClaimOwnership } from "./lib/ownership.svelte.js";
  import { requestPermission } from "./lib/notify.js";
  import { plural } from "./lib/plural.js";
  import { tileColor } from "./lib/guildaccent.js";
  import { GSW, toggleGuildSwitcher } from "./lib/guildswitch.svelte.js";
  import { MOM } from "./lib/momentsunseen.svelte.js";
  import { guildMenu } from "./lib/guildmenu.js";
  import { syncLayer } from "./lib/navstack.svelte.js";
  import { api } from "./lib/api.js";
  import { tooltip } from "./lib/tooltip.js";
  import { RADAR, liveChannelSet } from "./lib/radar.svelte.js";
  import { PERM, has } from "./lib/perms.js";
  import { LEVELS, levelLabel } from "./lib/notifs.js";
  import { longpress, haptic } from "./lib/touch.js";
  import { draftIn } from "./lib/drafts.svelte.js";
  import { failedIn, failedBodyIn } from "./lib/outbox.svelte.js";
  import { clampToBytes, NAME_MAX_BYTES } from "./lib/postdraft.js";
  import { focusOnMount } from "./lib/focus.js";
  import { listNav } from "./lib/listnav.js";
  import { callClock } from "./lib/calltimer.svelte.js";

  const clock = $derived(callClock());
  // One state machine for the call, shared with the stage — see callHealth.
  const callState = $derived(callHealth());

  // getDisplayMedia is absent in Android/iOS WebViews, so the share button is a
  // control that can only ever fail. It goes away entirely rather than sitting
  // in the voice bar taking a quarter of the width from the ones that work.
  const canShareScreen =
    typeof navigator !== "undefined" && !!navigator.mediaDevices?.getDisplayMedia;

  // Touch: long-press opens row menus (iOS never synthesizes contextmenu for
  // plain elements, and Android's synthesized one would double-fire alongside
  // the longpress action — so on coarse pointers longpress is the only path).
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  // strip: draw the shortcut strip under the place header (the default
  // arrangement). Off when the classic rail stands beside the list instead.
  // callFloating: the floating call window is on screen, carrying the faces
  // and the controls — the bar here folds to one line so the call is not
  // drawn twice.
  let { strip = true, callFloating = false, onJoinVoice, onLeaveVoice, onToggleMute, onToggleDeafen, onToggleShare, onToggleCamera } =
    $props();

  // The sidebar's own width decides when "N reachable" folds to its glyph.
  let colW = $state(280);

  // ---- the place header ----
  function placeMenu(e) {
    if (g && g.kind !== "dm") guildMenu(e, g);
  }
  // Who gets the settings gear rather than the short menu: anyone who can
  // change something in the guild's settings.
  const canManageGuild = $derived(
    !!g &&
      !g.evicted &&
      (g.isOwner ||
        g.canManage ||
        has(g.myPerms || 0, PERM.MANAGE_GUILD) ||
        has(g.myPerms || 0, PERM.MANAGE_ROLES) ||
        has(g.myPerms || 0, PERM.MANAGE_CHANNELS)),
  );
  function memberGuildMenu(e) {
    if (!g) return;
    openContextMenu(
      e,
      [
        { label: "Notifications", header: true },
        ...LEVELS.map((l) => ({
          label: l.label,
          icon: l.id === "none" ? "bellOff" : "bell",
          active: guildNotifLevel(g.id) === l.id,
          onClick: () => setGuildNotifs(g.id, l.id),
        })),
        { sep: true },
        { label: "Mark as read", icon: "check", onClick: () => g.channels.forEach((c) => markRead(c.id)) },
        !g.evicted && g.canManage && { label: "Invite people", icon: "personPlus", onClick: invitePeople },
        { sep: true },
        { label: leaveGuildLabel(g), icon: g.isOwner ? "trash" : "door", danger: true, onClick: () => confirmLeaveGuild(g) },
      ].filter(Boolean),
      { title: g.name },
    );
  }
  async function invitePeople() {
    try {
      S.modal = { kind: "invite", code: await api.inviteCode(g.id) };
    } catch (err) {
      flash(err);
    }
  }

  // Your calendar's news: events that are new or changed since you last looked.
  const unseenEvents = $derived(Object.values(RADAR.unseen).reduce((a, b) => a + b, 0));

  // "N reachable" (§2.10): members other than you whose presence says they are
  // here right now. Invisible members are offline to everyone else, and render
  // that way, so they count that way. No new core call: the member list with
  // presence is already in state.
  const others = $derived(S.members.filter((m) => !m.isSelf));
  const reachable = $derived(others.filter((m) => m.online).length);
  const reachLabel = $derived(
    others.length === 0
      ? "Nobody else has joined yet"
      : reachable === 0
        ? "Nobody else can be reached right now"
        : `${reachable} of the ${plural(others.length, "other member")} can be reached right now`,
  );
  // Local peers: every node this device has connected to. A network log, not
  // a roster — it used to end the member list, where it read as a second list
  // of people; it belongs with "who can this device reach".
  const localPeers = $derived(S.contacts.filter((c) => c.fingerprint !== S.identity.fingerprint));
  let showPeers = $state(false);

  // "N reachable" is the second line of the place header, under the guild's
  // name, so it no longer competes with the name for one line's width. It
  // shared that line once and gave way first, which at the default 280px
  // sidebar meant nobody ever saw the words — a lone "● 3", or a hollow ring
  // at zero. Only a header narrower than the sidebar's own minimum (240px)
  // folds it to the glyph and number.
  function reachFit(node) {
    // The header is the sidebar less its own hairline, so a 240px sidebar
    // measures a little under 240 here; the fold is for anything narrower.
    const fit = () => (node.dataset.reach = node.offsetWidth < 232 ? "glyph" : "full");
    const ro = new ResizeObserver(fit);
    ro.observe(node);
    fit();
    return { destroy: () => ro.disconnect() };
  }
  let reachPop = $state(null);
  function toggleReach(e) {
    if (reachPop) {
      reachPop = null;
      return;
    }
    const r = e.currentTarget.getBoundingClientRect();
    reachPop = { x: Math.max(8, Math.min(r.left, window.innerWidth - 256)), y: r.bottom + 6 };
  }
  syncLayer("popover", () => !!reachPop, () => (reachPop = null));
  $effect(() => {
    if (!reachPop) return;
    const off = (ev) => {
      if (!ev.target.closest?.(".reach-pop, .ph-reach")) reachPop = null;
    };
    window.addEventListener("pointerdown", off, true);
    return () => window.removeEventListener("pointerdown", off, true);
  });

  // ---- the nudge queue (§5.14) ----
  // One at a time, by priority: security (the recovery phrase) > owner setup >
  // tips (notifications, an update). The connection is not a nudge: losing it
  // is the one full-width bar (App.svelte). "Later" on the phrase quiets it for
  // this session only — it keeps coming back until the words are verified.
  let setupWants = $state(false);

  // The guild's succession, as an owner-setup nudge. The owner is the guild's
  // permission root: if they vanish while nobody else can manage members and
  // no heir is named, nobody can ever add or remove a member again. This was
  // an amber card at the top of the Members tab — the loudest thing on the
  // main screen, in the one place that is meant to be only the roster. It is a
  // nudge row now, in its turn. Dismissal is per guild and local (the same key
  // the card used, so a dismissal made there still holds).
  let heirNudgeDismissed = $state(JSON.parse(localStorage.getItem("heirNudgeDismissed") || "{}"));
  function dismissHeirNudge() {
    heirNudgeDismissed = { ...heirNudgeDismissed, [S.activeGuildId]: true };
    localStorage.setItem("heirNudgeDismissed", JSON.stringify(heirNudgeDismissed));
  }
  const soleAdmin = $derived(
    !!g &&
      g.kind !== "dm" &&
      !g.evicted &&
      g.isOwner &&
      !g.heir &&
      !heirNudgeDismissed[g.id] &&
      S.members.some((m) => !m.isSelf && !m.pending) &&
      !S.members.some((m) => !m.isSelf && !m.pending && m.canManage),
  );
  // The heir's side of it: whoever was named may not know, or remember.
  const namedHeir = $derived(
    !!g &&
      g.kind !== "dm" &&
      !g.evicted &&
      !g.isOwner &&
      g.heir === S.identity.fingerprint &&
      !heirNudgeDismissed[`heir:${g.id}`],
  );
  // Once, on a desktop: that the window can be arranged another way. Last in
  // the queue, so it never stands in front of anything that matters. Not on a
  // touch screen: a phone on its side is wide enough for this layout, and there
  // the card took half of a 390px-tall list to offer arrangements for a mouse.
  const layoutHint = $derived(!S.isMobile && !coarse && !S.prefs.layoutHintSeen && !!g);
  function layoutHintDone() {
    setPref("layoutHintSeen", true);
  }
  const nudgeNow = $derived(
    S.prefs.backupPending && !phraseLater.on
      ? "phrase"
      : setupWants
        ? "setup"
        : soleAdmin
          ? "heir"
          : namedHeir
            ? "isheir"
            : S.notifyAsk
          ? "notify"
            : S.update
              ? "update"
              : layoutHint
                ? "layout"
                : "",
  );

  // ---- unread messages ----
  // Arrival order, appended at the end and never re-sorted: rows leave only
  // when they are read, so nothing jumps under the pointer.
  let dmOrder = $state([]);
  $effect(() => {
    const unread = dmList().filter((d) => !d.dmNotes && guildUnread(d).count > 0);
    const ids = new Set(unread.map((d) => d.id));
    const kept = dmOrder.filter((id) => ids.has(id));
    const fresh = unread
      .filter((d) => !kept.includes(d.id))
      .sort((a, b) => (a.lastActivity || 0) - (b.lastActivity || 0))
      .map((d) => d.id);
    const next = [...kept, ...fresh];
    if (next.join() !== dmOrder.join()) dmOrder = next;
  });
  const dmBlock = $derived(dmOrder.map((id) => S.guilds.find((x) => x.id === id)).filter(Boolean));

  // ---- unread above / below (§2.3) ----
  // When unread rows are scrolled out of the list's view, a pill on that edge
  // says so and takes you to the nearest one. Measured, not tracked: the rows
  // are re-read on scroll and whenever the unread set changes.
  let offscreen = $state({ up: 0, upM: 0, down: 0, downM: 0 });
  function measureUnread() {
    const sc = scrollEl;
    if (!sc) return;
    const box = sc.getBoundingClientRect();
    const o = { up: 0, upM: 0, down: 0, downM: 0 };
    for (const row of sc.querySelectorAll(".channel-row.unread")) {
      const r = row.getBoundingClientRect();
      const m = row.classList.contains("mentioned") ? 1 : 0;
      if (r.bottom <= box.top + 4) {
        o.up++;
        o.upM += m;
      } else if (r.top >= box.bottom - 4) {
        o.down++;
        o.downM += m;
      }
    }
    if (o.up !== offscreen.up || o.down !== offscreen.down || o.upM !== offscreen.upM || o.downM !== offscreen.downM) offscreen = o;
  }
  $effect(() => {
    void S.unread;
    void g?.id;
    void S.prefs.collapsedCats;
    const sc = scrollEl;
    if (!sc) return;
    requestAnimationFrame(measureUnread);
    const ro = new ResizeObserver(measureUnread);
    ro.observe(sc);
    return () => ro.disconnect();
  });
  function jumpUnread(dir) {
    const sc = scrollEl;
    if (!sc) return;
    const box = sc.getBoundingClientRect();
    const rows = [...sc.querySelectorAll(".channel-row.unread")];
    const target =
      dir < 0
        ? rows.filter((r) => r.getBoundingClientRect().bottom <= box.top + 4).pop()
        : rows.find((r) => r.getBoundingClientRect().top >= box.bottom - 4);
    target?.scrollIntoView({ block: "center", behavior: "smooth" });
  }
  const markLabel = (m) => (m ? plural(m, "mention") : "Unread");

  // The row drawn selected is the one the main column shows. While the call
  // stage is up that is the call's room, not the text channel still open
  // behind it: two selected-looking rows (the text channel's fill and bar,
  // and the joined voice room's tint) made it impossible to tell where you
  // were.
  const mainColumnId = $derived(
    S.callStage && !S.isMobile ? S.voice?.channelId || S.joiningVoice || "" : S.activeChannelId,
  );

  const hangupLabel = $derived.by(() => {
    const id = S.voice?.channelId;
    const cg = id && S.guilds.find((x) => x.channels.some((c) => c.id === id));
    return cg?.kind === "dm" && (cg.dmMembers ?? 2) <= 2 ? "End call" : "Leave call";
  });

  // The call's guild, for the dock's second line.
  const callGuildName = $derived.by(() => {
    const id = S.voice?.channelId;
    if (!id) return "";
    const cg = S.guilds.find((x) => x.channels.some((c) => c.id === id));
    return cg && cg.kind !== "dm" ? cg.name : "";
  });

  // Joining a call mounts the call bar under the list, and the list gives up
  // the bar's height to it. On a phone that is a sixth of the screen, and the
  // voice channels are the last rows of the list, so the row you just joined —
  // highlighted, the one thing the bar is about — went below the fold, its
  // top edge peeking out above the bar as if the bar had been laid over it.
  // So the joined room and its roster are brought into view, once, as they
  // arrive: the roster fills in over the first moments of a call, and after
  // that a scroll is the reader's own, not ours to undo when someone joins.
  let scrollEl = $state();
  let shownCall = "";
  let joinedAt = 0;
  $effect(() => {
    const id = S.voice?.channelId || "";
    const heads = id ? voiceMembersFor(id).length : 0;
    if (id !== shownCall) {
      shownCall = id;
      joinedAt = Date.now();
    }
    if (!id || !scrollEl || Date.now() - joinedAt > 4000) return;
    void heads;
    requestAnimationFrame(() => revealCall(id));
  });
  function revealCall(id) {
    const sc = scrollEl;
    const row = sc?.querySelector(`.channel-row[data-ch-id="${CSS.escape(id)}"]`);
    if (!row) return;
    let end = row;
    for (let n = row.nextElementSibling; n?.classList.contains("vc-member"); n = n.nextElementSibling) end = n;
    const box = sc.getBoundingClientRect();
    const top = row.getBoundingClientRect().top - box.top;
    const bottom = end.getBoundingClientRect().bottom - box.top;
    const room = sc.clientHeight;
    // The room's own row wins over its roster when both cannot fit.
    if (top < 0 || bottom - top > room) sc.scrollTop += top - 8;
    else if (bottom > room) sc.scrollTop += bottom - room + 8;
  }

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

  // The second line of a DM row: "You:" when the last thing said was yours, and
  // nothing otherwise. In a 1:1 the only other speaker is the person whose name
  // is the row's title, one line up, so "Lin: already on the list" under "Lin"
  // said it twice. A group DM gets no prefix either: the backend reports only
  // whether the last line was ours, and naming one of five would be a guess.
  function dmSaid(dm) {
    return dm.dmPreviewMine && !dm.dmNotes ? "You: " : "";
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
      if (days <= 0) return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit", ...clockOpts() });
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

  const typeIcon = channelTypeIcon;

  function clickChannel(c) {
    if (c.type === "voice") {
      // First click joins and leaves you where you were. A second click — the
      // channel, or your own row under it — opens the full call. Right-click
      // Open chat still reads the room without joining, and without the tiles.
      const here = (S.voice && S.voice.channelId === c.id) || S.joiningVoice === c.id;
      if (!here) {
        onJoinVoice?.(c.id);
        return;
      }
      if (!S.callStage) openCallStage();
      return;
    }
    selectChannel(c.id);
  }

  function channelMenu(e, c) {
    openContextMenu(e, [
      c.type === "voice" && {
        label: "Open chat",
        icon: "hash",
        onClick: () => {
          // Already looking at the tiles: the chat is a column beside them.
          // Otherwise it is just the room's messages, no join, no stage.
          if (S.callStage && ((S.voice && S.voice.channelId === c.id) || S.joiningVoice === c.id)) {
            if (!S.callStageChat) toggleCallStageChat();
          } else {
            closeCallStage();
            selectChannel(c.id);
          }
        },
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
      // Move to…: on desktop this also lives in the row's chevron menu and in
      // drag-and-drop, but a phone has neither, so this list is its only way.
      ...(canManageChannels && !c.parent && (g?.categories || []).length
        ? [
            { sep: true },
            { label: "Move to…", header: true },
            ...[{ id: "", name: "No category" }, ...g.categories]
              .filter((cat) => cat.id !== (c.category || ""))
              .map((cat) => ({
                label: cat.name,
                icon: cat.id ? "chevron" : "hash",
                onClick: () => moveChannelToCategory(c, cat.id),
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
      // A noisy group used to ping (sound and notification) for every
      // message with no way to quiet it short of leaving. The levels are the
      // channel's, as everywhere else: in a DM "Only @mentions" means just
      // that — the conversation still shows unread, and only an @you pings.
      ...(dm.channels?.[0]
        ? [
            { label: "Notifications", header: true },
            ...LEVELS.map((l) => ({
              label: l.label,
              icon: l.id === "none" ? "bellOff" : "bell",
              active: notifLevel(dm.channels[0].id, dm.id) === l.id,
              onClick: () => setChannelNotifs(dm.channels[0].id, l.id === "all" ? null : l.id),
            })),
            { sep: true },
          ]
        : []),
      dm.channels?.[0] && {
        label: "Disappearing messages…",
        icon: "clock",
        onClick: () => (S.modal = { kind: "disappear", channelId: dm.channels[0].id }),
      },
      isGroup && {
        label: "People…",
        icon: "members",
        onClick: () => (S.modal = { kind: "groupMembers", guildId: dm.id }),
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

<nav class="cols" aria-label="Guilds and channels" bind:clientWidth={colW}>
  <!-- The place header: where you are, and the door to everywhere else. The
       name opens the guild switcher (the inventory); a right-click or a
       long-press opens the guild's menu. "N reachable" is the honest
       peer-to-peer signal: how many members this device can reach right now.
       It gives way first when the sidebar is narrow (§2.9), then the name
       truncates; the chevron and invite never shrink. -->
  {#if g && g.kind !== "dm"}
    <header
      class="place-head"
      data-place-head
      style="--guild-accent:{tileColor(g)}"
      class:gone={!!g.evicted}
      use:reachFit
    >
      <!-- In the Rail layout every guild is already in the column beside
           this, so the name does what it did in the classic arrangement:
           opens the guild's settings. -->
      <button
        class="ph-main"
        aria-haspopup="dialog"
        aria-expanded={strip ? GSW.open : undefined}
        aria-label={strip ? `${g.name} — switch guild` : `${g.name} — guild settings`}
        aria-description={ownsPlace(g) ? "you own this guild" : undefined}
        onclick={(e) => (strip ? toggleGuildSwitcher(e.currentTarget.closest("[data-place-head]")) : openGuildHub())}
        oncontextmenu={coarse ? (e) => e.preventDefault() : placeMenu}
        use:longpress={{ handler: placeMenu }}
      >
        <span class="ph-icon" class:hasicon={!!g.icon} class:moments={MOM.unseen[g.id] > 0} title={MOM.unseen[g.id] ? "New moments: right-click the guild's name, then View moments" : undefined}>
          {#if g.icon}<img src={g.icon} alt="" />{:else}<GuildMark {g} size={13} />{/if}
        </span>
        <span class="ph-name"><bdi>{g.name}</bdi></span>
        {#if ownsPlace(g)}<OwnerMark size={12} label={ownerLabel(g)} />{/if}
        {#if strip}<span class="ph-chev" aria-hidden="true"><Icon name="chevron" size={14} /></span>{/if}
      </button>
      {#if !g.evicted}
        <button
          class="ph-reach"
          aria-label={reachLabel}
          aria-haspopup="dialog"
          aria-expanded={!!reachPop}
          use:tooltip={{ text: reachLabel }}
          onclick={toggleReach}
        >
          <span class="reach-glyph" class:none={reachable === 0} class:all={others.length > 0 && reachable === others.length} aria-hidden="true"></span>
          <!-- In words; the glyph and number only below 240px (reachFit). -->
          {#if reachable === 0}
            <span class="reach-w">Nobody reachable</span>
          {:else}
            <span class="reach-n">{reachable}<span class="reach-w">&nbsp;reachable</span></span>
          {/if}
        </button>
      {/if}
      <span class="ph-acts">
        {#if g.canManage && !g.evicted}
          <button class="iconbtn ph-invite" aria-label="Invite people" use:tooltip onclick={invitePeople}>
            <Icon name="personPlus" size={16} />
          </button>
        {/if}
        <!-- The guild's settings, in sight. The name used to open them; it
             opens the switcher now, which left settings behind a right-click
             nobody tries and a ⋯ row below the menu's fold. Someone who can
             change the guild gets its settings; anyone else gets the few
             things that are theirs to change about it. -->
        {#if canManageGuild}
          <button class="iconbtn ph-gear" aria-label="Guild settings" use:tooltip onclick={() => openGuildHub()}>
            <Icon name="gear" size={16} />
          </button>
        {:else}
          <button class="iconbtn ph-gear" aria-label="Guild options" aria-haspopup="menu" use:tooltip onclick={memberGuildMenu}>
            <Icon name="gear" size={16} />
          </button>
        {/if}
      </span>
    </header>
  {:else if g?.kind === "dm"}
    <header class="place-head dm-head" data-place-head>
      <span class="ph-main static"><span class="ph-name"><bdi>Messages</bdi></span></span>
      <button class="iconbtn ph-invite" onclick={newMessage} aria-label="New message" use:tooltip>
        <Icon name="edit" size={16} />
      </button>
    </header>
  {:else}
    <header class="place-head" data-place-head>
      <span class="ph-main static"><span class="ph-name"><bdi>Concord</bdi></span></span>
    </header>
  {/if}

  {#if strip}
    <GuildRail layout="strip" />
  {/if}

  <!-- One field for going anywhere (it opens the palette), and the inbox,
       which is not a place but a question about all of them. -->
  <div class="jump-row">
    <button class="jump" onclick={() => (S.quickSwitcher = true)} aria-keyshortcuts="Control+K">
      <Icon name="search" size={14} />
      <span class="jump-lbl">Jump to…</span>
      <!-- The chip gives way before the label does: at 240px "Jump to…" was
           cut to "Jump t…" to keep a shortcut hint. -->
      {#if !S.isMobile && !coarse && colW >= 260}<Kbd>Ctrl K</Kbd>{/if}
    </button>
    <!-- Inbox and Your calendar have their own buttons on the classic rail;
         here only when the strip stands in for it, so neither is drawn twice. -->
    {#if !S.isMobile && strip}
      <span class="inbox-wrap">
        <button
          class="iconbtn inbox-btn"
          class:on={S.modal?.kind === "myCalendar"}
          aria-label={unseenEvents > 0 ? `Your calendar — ${plural(unseenEvents, "new or changed event")}` : "Your calendar"}
          use:tooltip={{ text: "Your calendar — everything you're part of" }}
          onclick={() => (S.modal = S.modal?.kind === "myCalendar" ? null : { kind: "myCalendar" })}
        >
          <Icon name="calendar" size={16} />
        </button>
        {#if unseenEvents > 0}<span class="inbox-badge"><Badge count={unseenEvents} ring="var(--bg-1)" /></span>{/if}
      </span>
      <span class="inbox-wrap">
        <button
          class="iconbtn inbox-btn"
          class:on={S.modal?.kind === "inbox"}
          aria-label={S.inbox.unread > 0 ? `Inbox — ${S.inbox.unread} unread` : "Inbox"}
          use:tooltip={{ text: "Inbox — mentions, replies and alert words aimed at you" }}
          onclick={() => (S.modal?.kind === "inbox" ? (S.modal = null) : openInbox({ tab: S.isMobile }))}
        >
          <Icon name="bell" size={16} />
        </button>
        {#if S.inbox.unread > 0}<span class="inbox-badge"><Badge count={S.inbox.unread} ring="var(--bg-1)" /></span>{/if}
      </span>
    {/if}
  </div>

  <!-- Drag-to-reorder is a pointer-only enhancement; keyboard users reorder via
       the row menu's "Move to…", so the drag handlers stay off the a11y tree. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="list-wrap">
  {#if offscreen.up}
    <button class="edge-pill up" onclick={() => jumpUnread(-1)}>↑ {markLabel(offscreen.upM)}</button>
  {/if}
  {#if offscreen.down}
    <button class="edge-pill down" onclick={() => jumpUnread(1)}>↓ {markLabel(offscreen.downM)}</button>
  {/if}
  <!-- One Tab stop for the whole list (lib/listnav.js): the open channel's
       row holds it, arrows and typeahead move between rows, and a row's menu
       (the menu key or Shift+F10) carries everything its chevron and bell do.
       Those used to be two more stops on every row. -->
  <div
    class="scroll"
    class:channels-touch={coarse}
    ondragleave={listDragLeave}
    onscroll={measureUnread}
    bind:this={scrollEl}
    use:listNav={{
      rows: ".cat-toggle, .channel, .vc-member, .dm-item",
      current: ".channel-row.active .channel, .dm-item.active",
      park: ".ch-menu .trigger, .mute-btn, .join-btn, .cat-actions button, .cat-more",
      name: ".cat-label, .ch-name, .vc-name, .dm-name",
    }}
  >
    <!-- The one nudge (§5.14): a queue, one at a time, at the top of the list
         and never over the feed. Security outranks setup, setup outranks tips.
         The setup checklist stays mounted while it is armed — it watches the
         feed to tick its own steps — and simply draws nothing while something
         more important holds the row. -->
    {#if nudgeNow === "phrase"}
      <div class="nudge-row">
        <Nudge
          icon="key"
          tone="warn"
          action="Show me"
          onAction={() => (S.modal = { kind: "settings", focus: "phrase" })}
          dismissLabel="Later"
          onDismiss={() => (phraseLater.on = true)}
        >
          <strong>Write down your recovery phrase.</strong> It is the only way back into this account.
        </Nudge>
      </div>
    {:else if nudgeNow === "heir"}
      <div class="nudge-row">
        <Nudge
          icon="crown"
          action="Name an heir"
          onAction={() => openGuildPage("ownership")}
          onDismiss={dismissHeirNudge}
        >
          <strong>Only you can manage members.</strong>
        </Nudge>
      </div>
    {:else if nudgeNow === "isheir"}
      <div class="nudge-row">
        <Nudge
          icon="crown"
          action="Take over…"
          onAction={() => confirmClaimOwnership()}
          onDismiss={() => {
            heirNudgeDismissed = { ...heirNudgeDismissed, [`heir:${S.activeGuildId}`]: true };
            localStorage.setItem("heirNudgeDismissed", JSON.stringify(heirNudgeDismissed));
          }}
        >
          <strong>You're this guild's heir.</strong>
        </Nudge>
      </div>
    {:else if nudgeNow === "notify"}
      <div class="nudge-row">
        <Nudge
          icon="bell"
          action="Turn on"
          onAction={() => {
            requestPermission();
            dismissNotifyAsk(true);
          }}
          dismissLabel="Not now"
          onDismiss={() => dismissNotifyAsk(true)}
        >
          <strong>You just missed a message.</strong> Concord can tell you next time.
        </Nudge>
      </div>
    {:else if nudgeNow === "layout"}
      <div class="nudge-row">
        <Nudge
          icon="grid4"
          action="Choose"
          onAction={() => {
            layoutHintDone();
            S.modal = { kind: "appearance" };
          }}
          dismissLabel="Not now"
          onDismiss={layoutHintDone}
        >
          <strong>Prefer a different layout?</strong> Sidebar, Rail or Compact.
        </Nudge>
      </div>
    {:else if nudgeNow === "update"}
      <div class="nudge-row">
        <Nudge
          icon="download"
          action="Update"
          onAction={() => (S.modal = { kind: "settings", startUpdate: true })}
          onDismiss={dismissUpdate}
        >
          <strong>Concord {S.update.latest}</strong> is ready (you have {S.update.current}).
        </Nudge>
      </div>
    {/if}
    {#if g && g.kind !== "dm"}
      <SetupCard hidden={nudgeNow !== "setup"} bind:wants={setupWants} />
    {/if}
    {#if g?.kind === "dm"}
      {#if S.requests.length}
        <!-- Strangers wait here. Deliberately a quiet row, not a badge on the
             rail: a request has cost the sender nothing and must not be able to
             ring your app. -->
        <button class="dm-item requests" onclick={() => (S.modal = { kind: "requests" })}>
          <span class="dm-notes-icon"><Icon name="members" size={15} /></span>
          <span class="dm-name">Message requests</span>
          <Badge dot label="{S.requests.length} waiting" />
        </button>
      {/if}
      {#each dms as dm (dm.id)}
        {@const active = dm.id === S.activeGuildId}
        {@const unread = dm.dmNotes ? { count: 0, attention: 0 } : guildUnread(dm)}
        {@const draft = draftIn(dm.channels?.[0]?.id)}
        <button
          class="dm-item"
          data-menu-row
          class:active
          class:unread={unread.count > 0}
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
              {#if ownsPlace(dm)}<OwnerMark size={11} label={ownerLabel(dm)} />{/if}
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
              {#if failedIn(dm.channels?.[0]?.id)}
                <!-- Same precedence as a channel row: a message that did not
                     send outranks the preview AND the draft, in both places. -->
                <em class="dm-failed">Didn't send:</em> {failedBodyIn(dm.channels?.[0]?.id)}
              {:else if draft}
                <em class="dm-draft">Draft:</em> {draft}
              {:else if dm.dmPreview}
                {#if dmSaid(dm)}<span class="dm-said">{dmSaid(dm)}</span>{/if}{dm.dmPreview}
              {:else}
                <span class="dm-quiet">{dm.dmNotes ? "Anything you want to keep" : "No messages yet"}</span>
              {/if}
            </span>
          </span>
          {#if unread.attention > 0}
            <Badge count={unread.attention} label={`${unread.attention} unread`} />
          {:else if unread.count > 0}
            <Badge dot label="Unread" />
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
          <button class="primary empty-cta" onclick={newMessage}>
            <Icon name="plus" size={13} /> New message
          </button>
        </div>
      {/if}
    {:else if g}
      {#if !(g?.channels || []).length}
        <!-- On a phone this list is the whole screen, and it used to be empty
             with nothing to say why. -->
        <p class="no-ch">
          No channels yet.{canManageChannels && !g?.evicted ? " Add the first one below." : ""}
        </p>
      {/if}
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
              <!-- Selected on open, because a rename usually replaces rather
                   than continues, and because until this box got a real focus
                   action it was not focused at all.
                   The cap is the BACKEND's, in the backend's unit. `maxlength`
                   counts characters and the cap is 64 bytes, so this box used to
                   accept 39 Arabic characters (71 bytes) and hand them to a
                   truncation that cut one in half. clampToBytes stops exactly
                   where the backend would, on a rune boundary, so what you see
                   in the box is what gets stored. -->
              <input
                class="cat-edit"
                use:focusOnMount={{ select: true }}
                value={grp.name}
                oninput={(e) => {
                  const v = clampToBytes(e.currentTarget.value, NAME_MAX_BYTES);
                  if (v !== e.currentTarget.value) {
                    const at = Math.min(e.currentTarget.selectionStart ?? v.length, v.length);
                    e.currentTarget.value = v;
                    e.currentTarget.setSelectionRange(at, at);
                  }
                }}
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
              {#if canManageChannels && coarse}
                <!-- A phone gets one control here, not three: the +, pencil and
                     trash sat on every category heading permanently, because
                     there is no hover to hide them behind, and a trash can
                     beside every heading is a lot of ink for something done once
                     in a guild's life. This opens the same menu a long-press on
                     the heading does, and shows only while the heading has
                     focus. -->
                <button
                  class="cat-add cat-more"
                  aria-label="Options for {grp.name}"
                  aria-haspopup="menu"
                  onclick={(e) => categoryMenu(e, grp)}
                >
                  <Icon name="dots" size={14} />
                </button>
              {:else if canManageChannels}
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
          {@const active = c.id === mainColumnId}
          {@const inVoice = S.voice && S.voice.channelId === c.id}
          {@const joiningHere = S.joiningVoice === c.id}
          {@const occupied = c.type === "voice" && voiceMembersFor(c.id).length > 0}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="channel-row"
            data-menu-row
            data-ch-id={c.id}
            class:active
            class:unread={!!u && !isMuted(c.id, g?.id)}
            class:mentioned={!!u?.mentions && !isMuted(c.id, g?.id)}
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
              {#if !active && failedIn(c.id)}
                <!-- A send FAILED in here. Louder than the draft mark and in the
                     same slot, because the two are not the same news: a draft is
                     a thought you chose not to finish, this is a message you
                     believe you sent. It was the only thing in the app that knew
                     — leave the channel and there was no mark of any kind, so a
                     channel with unsent TEXT was marked and a channel with a
                     failed MESSAGE was not. -->
                <span
                  class="ch-failed"
                  role="img"
                  use:tooltip={failedIn(c.id) === 1
                    ? "A message here didn't send — open it to retry"
                    : `${failedIn(c.id)} messages here didn't send — open it to retry`}
                  aria-label={failedIn(c.id) === 1
                    ? "One message here didn't send"
                    : `${failedIn(c.id)} messages here didn't send`}
                >
                  <Icon name="alert" size={11} />
                </span>
              {:else if !active && draftIn(c.id)}
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
              <!-- No exception for the open channel: its unread is cleared the
                   moment it is actually being read (lib/readstate.js), so a
                   count still standing here — a phone's channel list over the
                   open channel, a minimised window — is unread for real. -->
              <!-- A number only for what is aimed at you (§1.3): the mentions,
                   in your accent. Plain unread is the weight of the name plus
                   a quiet dot. A muted channel shows nothing — unless someone
                   @-ed you anyway, which is an outlined count. The label is
                   where a screen reader learns either. -->
              {#if c.type !== "voice" && u && !isMuted(c.id, g?.id)}
                <span class="ch-mark">
                  {#if u.mentions > 0}
                    <Badge count={u.mentions} label={`${u.count} unread, ${u.mentions} mentioning you`} />
                  {:else}
                    <Badge dot label={`${u.count} unread`} />
                  {/if}
                </span>
              {:else if c.type !== "voice" && u?.mentions && isMuted(c.id, g?.id)}
                <span class="ch-mark"><Badge count={u.mentions} muted label={`Muted, ${u.mentions} mentioning you`} /></span>
              {/if}
              {#if joiningHere}
                <!-- Pressed, and saying why. A click on a voice row opens your
                     microphone and then waits on the node; without this the row
                     looked untouched for the whole wait, which is what made
                     people click it again. -->
                <span class="ch-joining">Connecting…</span>
              {:else if occupied}
                <!-- How many. The row said only "somebody is in here" — you had
                     to read the participant list under it to find out whether
                     that was one person or six, and in a collapsed category
                     there is no list to read. -->
                <span class="ch-heads">{voiceMembersFor(c.id).length} in call</span>
              {/if}
            </button>
            {#if canManageChannels && !coarse}
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
            {#if c.type !== "voice" && coarse}
              <!-- On touch the bell is not a per-row control: a long-press on
                   the row holds the channel's whole notification menu. It shows
                   only as the news that this channel IS muted, and a tap on it
                   is the quickest way back. -->
              {#if isMuted(c.id, g?.id)}
                <button
                  class="mute-btn"
                  aria-label="Unmute channel"
                  onclick={() => {
                    toggleMute(c.id);
                    haptic("light");
                  }}
                >
                  <Icon name="bellOff" size={13} />
                </button>
              {/if}
            {:else if c.type !== "voice"}
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
              {@const vst = splitStatus(vm.self ? S.identity.status : vmem?.status)}
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
                    mood={vst.emoji}
                    moodTitle={vst.text}
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
      {#if canManageChannels && !g?.evicted}
        <!-- The list's own way to grow, after its last row: a quiet row rather
             than a "+" on a CHANNELS heading the list no longer has. -->
        <div class="add-ch">
          <Menu name="Add channel or category" align="left" compact>
            {#snippet trigger()}
              <span class="add-ch-in"><Icon name="plus" size={14} /> Add a channel</span>
            {/snippet}
            <button class="menu-item" onclick={() => (S.modal = { kind: "channel" })}>
              <Icon name="hash" size={14} /> New channel
            </button>
            <button class="menu-item" onclick={() => (S.modal = { kind: "category" })}>
              <Icon name="folder" size={14} /> New category
            </button>
          </Menu>
        </div>
      {/if}
    {:else}
      <!-- No guild at all: the welcome in the conversation column offers the
           two ways in; this says what a guild is, in one line. -->
      <p class="no-ch">No guilds yet. A guild is a space of your own, kept by the people in it.</p>
    {/if}
  </div>
  </div>

  <!-- Unread messages (§2.3): direct messages waiting while you are in a
       guild. Newest last, and it never reorders while it is on screen — a row
       leaves only when it is read, so the one you were reaching for is still
       where it was. -->
  {#if !S.isMobile && strip && g?.kind !== "dm" && (dmBlock.length || S.requests.length)}
    <div class="unread-block" aria-label="Unread messages" role="group">
      <div class="ub-head">Unread messages</div>
      {#if S.requests.length}
        <button class="ub-row" onclick={() => (S.modal = { kind: "requests" })}>
          <span class="ub-ic"><Icon name="members" size={14} /></span>
          <span class="ub-name">Message requests</span>
          <Badge dot label="{S.requests.length} waiting" />
        </button>
      {/if}
      {#each dmBlock.slice(-3) as dm (dm.id)}
        {@const u = guildUnread(dm)}
        <button class="ub-row" onclick={() => selectGuild(dm.id)} data-menu-row oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => dmMenu(e, dm)}>
          {#if (dm.dmMembers ?? 2) > 2}
            <GroupAvatar faces={dm.dmFaces || []} size={20} />
          {:else}
            <Avatar
              name={dm.name}
              image={dm.dmPeerAvatar || dm.dmFaces?.[0]?.avatar || dm.icon}
              size={20}
              online={dm.dmPeer ? !!dm.dmPeerOnline : null}
              presence={dm.dmPeerPresence || ""}
            />
          {/if}
          <span class="ub-name"><bdi>{dm.name}</bdi></span>
          {#if u.attention}<Badge count={u.attention} label={`${u.attention} unread`} />{:else if u.count}<Badge dot label="Unread" />{/if}
        </button>
      {/each}
      {#if dmBlock.length > 3}
        <button class="ub-more" onclick={() => openDMs()}>+{dmBlock.length - 3} more</button>
      {/if}
    </div>
  {/if}

  <!-- The call dock (§8.12): in a call, the sidebar holds the call — which
       room, whether it is carrying, who is in it, and the five controls — so
       there is one call surface, not a floating window beside a bar. While the
       stage itself is on screen its own controls are right there, and the dock
       shrinks to the line that is still news. -->
  {#if S.voice && callFloating}
    <!-- The floating window is up and holds the faces and the controls: here
         only the line that says a call is running and takes you back to it. -->
    <button
      class="voice-line"
      class:trouble={!callState.live}
      aria-label="Return to the call in {channelShort(S.voice.channelId) || 'the call'}"
      onclick={() => openCallStage()}
    >
      <span class="vb-live" class:held={!callState.live} aria-hidden="true"></span>
      <span class="vl-text">
        {callState.live ? "In call" : callState.label} · <bdi>{channelShort(S.voice.channelId) || "Call"}</bdi>{#if clock}&nbsp;· <span class="vb-clock">{clock}</span>{/if}
      </span>
      <span class="vl-return">Return</span>
    </button>
  {:else if S.voice && !(S.callStage && !S.isMobile)}
    {@const roster = voiceMembersFor(S.voice.channelId)}
    <div class="voice-bar" class:trouble={!callState.live} role="group" aria-label="Call">
      <button class="vb-info" use:tooltip aria-label="Open the call" onclick={() => openCallStage()}>
        <span class="vb-text">
          <span class="vb-title">
            <span class="vb-label">{channelShort(S.voice.channelId) || "Call"}</span>
            {#if callGuildName}<span class="vb-guild">{callGuildName}</span>{/if}
          </span>
          <span class="vb-state" class:held={!callState.live}>
            <span class="vb-live" class:held={!callState.live} aria-hidden="true"></span>
            {callState.label}{#if clock}<span class="vb-clock">&nbsp;· {clock}</span>{/if}
          </span>
        </span>
      </button>
        {#if roster.length}
          <div class="vb-faces" aria-label="{roster.length} in this call" role="img">
            {#each roster.slice(0, 5) as vm (vm.peerId)}
              {@const vmem = memberByFpr(vm.fingerprint)}
              <span class="vb-face" class:speaking={vm.speaking} title={nameFor(vm.fingerprint)}>
                <Avatar
                  name={nameFor(vm.fingerprint)}
                  image={vm.self ? S.identity.avatar : vmem?.avatar || ""}
                  emoji={vm.self ? S.identity.emoji : vmem?.emoji || ""}
                  color={vm.self ? S.identity.color : vmem?.color || ""}
                  size={24}
                />
                {#if vm.muted || vm.deafened}<span class="vb-muted"><Icon name="micOff" size={9} /></span>{/if}
              </span>
            {/each}
            {#if roster.length > 5}<span class="vb-more">+{roster.length - 5}</span>{/if}
          </div>
        {/if}
        <span class="vb-actions">
          <button
            class="callbtn sm cut"
            class:on={S.muted}
            use:tooltip
            aria-label={S.muted ? "Unmute" : "Mute"}
            aria-pressed={S.muted}
            onclick={onToggleMute}
          >
            <Icon name={S.muted ? "micOff" : "mic"} size={14} />
          </button>
          <button
            class="callbtn sm cut"
            class:on={S.deafened}
            use:tooltip
            aria-label={S.deafened ? "Undeafen" : "Deafen"}
            aria-pressed={S.deafened}
            onclick={onToggleDeafen}
          >
            <Icon name={S.deafened ? "deafened" : "speaker"} size={14} />
          </button>
          <button
            class="callbtn sm"
            class:on={S.cameraOn}
            use:tooltip
            aria-label={S.cameraOn ? "Turn off camera" : "Turn on camera"}
            aria-pressed={S.cameraOn}
            onclick={onToggleCamera}
          >
            <Icon name={S.cameraOn ? "cameraOff" : "camera"} size={14} />
          </button>
          {#if canShareScreen}
            <button
              class="callbtn sm"
              class:on={S.sharing}
              use:tooltip
              aria-label={S.sharing ? "Stop sharing" : "Share screen"}
              aria-pressed={S.sharing}
              onclick={onToggleShare}
            >
              <Icon name={S.sharing ? "screenOff" : "screen"} size={14} />
            </button>
          {/if}
          <!-- One word for one thing (lib/callflow.js): hanging up a 1:1
               ends the call for both of you, so it says End; anywhere else
               the others carry on, so it says Leave. The dock is the one
               hang-up on screen, so it has to say which. -->
          <button class="callbtn sm hang" use:tooltip aria-label={hangupLabel} onclick={onLeaveVoice}>
            <Icon name="door" size={14} />
          </button>
        </span>
    </div>
  {/if}

  <!-- You: avatar with presence (sets your status), name and status (your
       profile card), the microphone, and Settings. -->
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
        size={32}
        online={true}
        presence={S.identity.presence || ""}
        frame={S.identity.frame || ""}
        decoration={S.identity.style?.dec || ""}
        dc={S.identity.style?.dc || ""}
        mood={myStatus.emoji}
        moodTitle={myStatus.text}
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
        <span class="small-status">
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
    {#if !S.voice}
      <!-- In a call the dock above carries the microphone; outside one this is
           the mute you join with. -->
      <button
        class="iconbtn me-mic"
        class:on={S.muted}
        use:tooltip
        aria-label={S.muted ? "Unmute your microphone" : "Mute your microphone"}
        aria-pressed={S.muted}
        onclick={onToggleMute}
      >
        <Icon name={S.muted ? "micOff" : "mic"} size={16} />
      </button>
    {/if}
    <button class="iconbtn me-gear" use:tooltip aria-label="Settings" aria-keyshortcuts="Control+," onclick={() => (S.modal = { kind: "settings" })}>
      <Icon name="gear" size={16} />
    </button>
  </div>
</nav>

{#if reachPop}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="reach-pop"
    role="dialog"
    aria-label="Who this device can reach"
    style="left:{reachPop.x}px;top:{reachPop.y}px"
  >
    {#if others.length === 0}
      <p>Nobody else has joined <bdi>{g?.name}</bdi> yet.</p>
    {:else if reachable === 0}
      <p>
        Nobody else in <bdi>{g?.name}</bdi> is online. You can still write — messages wait on this device and go out
        when someone comes back.
      </p>
    {:else}
      <!-- One verb, the header's: reachable. "3 of 3 members are online" in a
           four-member guild read as a miscount; you are the fourth, and
           saying "other" says so. -->
      <p>
        {reachable} of the {plural(others.length, "other member")}
        {reachable === 1 ? "is" : "are"} reachable now. Messages you send reach them directly; everyone
        else gets them when they next come online.
      </p>
    {/if}
    <button
      class="link"
      onclick={() => {
        reachPop = null;
        if (S.isMobile) S.membersOpen = true;
        else if (!S.prefs.memberPanel) setPref("memberPanel", true);
      }}>See who</button
    >
    <button class="peers-toggle" aria-expanded={showPeers} onclick={() => (showPeers = !showPeers)}>
      <span>Local peers · {localPeers.length}</span>
      <span class="chev" class:open={showPeers} aria-hidden="true"><Icon name="chevron" size={12} /></span>
    </button>
    {#if showPeers}
      <p class="peers-note">
        Every Concord node this device has connected to, strangers on the same network included. They can't read
        anything — messages are end-to-end encrypted. To verify a friend, open their profile.
      </p>
      <ul class="peers-list">
        {#each localPeers as c (c.peerId)}
          <li>
            <span class="mono" title={c.peerId}>{c.fingerprint.slice(0, 19)}…</span>
            {#if c.verified}<span class="peer-ok">Verified</span>{/if}
          </li>
        {:else}
          <li class="none">No peers seen yet.</li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}

{#if statusPop}
  <StatusPopover anchor={statusPop} onClose={() => (statusPop = null)} />
{/if}

<style>
  .no-ch {
    margin: var(--sp-2) var(--sp-3);
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  /* …and below the width where both still fit, the action drops its word for
     the icon the rest of the rail uses. Same target, same tooltip, same
     accessible name. 176px is measured: the title is 104px and the labelled
     pill 52px, so both survive to there and the column can be dragged narrower
     than that. */
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
    padding: var(--sp-1) var(--sp-2) var(--sp-2);
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .cat-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 28px;
    font-size: var(--fs-small);
    color: var(--text-muted);
    font-weight: 600;
    /* Sentence case, no tracking (§1.4): type does the hierarchy. */
    margin: var(--sp-3) 0 2px;
    padding: 0 var(--sp-1) 0 var(--sp-2);
  }
  .cat-head:first-child {
    margin-top: var(--sp-1);
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
  /* The caret appears where it is news: folded, or under the pointer. */
  .cat-caret {
    display: inline-grid;
    place-items: center;
    width: 0;
    overflow: hidden;
    color: var(--text-faint);
    transform: rotate(90deg);
    transition: transform var(--dur-quick) var(--ease-out);
  }
  .cat-caret.folded,
  .cat-toggle:hover .cat-caret,
  .cat-toggle:focus-visible .cat-caret {
    width: 11px;
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
    font-size: var(--fs-small);
    letter-spacing: inherit;
    font-weight: 600;
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
  .cat-add:focus-visible {
    opacity: 1; /* keyboard users must see the control they've tabbed onto */
  }
  .cat-head:hover .cat-add,
  .cat-add:hover {
    color: var(--text);
  }
  /* Being in a room is not a selection: the row says so in words ("2 in
     call", in the ok ink) and the roster under it lists you. It used to be
     an accent fill, a third kind of highlight beside selection and hover.
     Pressed while the join is in flight: the hover fill, pulsing, so the row
     is obviously doing something. */
  .voice-joining {
    background: color-mix(in srgb, var(--bg-3) 60%, transparent);
    animation: ch-joining 1.4s ease-in-out infinite;
  }
  @keyframes ch-joining {
    50% {
      opacity: 0.6;
    }
  }
  :global(:root[data-motion="reduced"]) .voice-joining {
    animation: none;
  }
  .ch-joining {
    margin-left: auto;
    flex-shrink: 0;
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    white-space: nowrap;
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
    overflow: visible;
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
  :global(:root[data-motion="reduced"]) .vc-av.speaking {
    animation: none;
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
  :global(:root[data-motion="reduced"]) .live-dot,
  :global(:root[data-motion="reduced"]) .ch-live-dot {
    animation: none;
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
  /* Hovering a drag over a category header: "drop here to file it under…". */
  .cat-head.drop-into {
    color: var(--accent-hover);
    box-shadow: inset 0 -2px 0 var(--accent);
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
  :global(:root[data-motion="reduced"]) .eq i {
    animation: none;
    height: 60%;
  }
  :global(:root[data-motion="reduced"]) .channel-row:hover .channel,
  :global(:root[data-motion="reduced"]) .dm-item:hover {
    transform: none;
  }
  /* Quiet: it is a reminder, not an alert. A draft is something YOU left, so
     it never competes with the unread count beside it. */
  .ch-draft {
    display: inline-grid;
    place-items: center;
    flex: none;
    color: var(--text-faint);
  }
  /* The failed mark is the one exception to "quiet": it is not a reminder, it
     is a message that did not arrive, and it stays lit without a hover. */
  .ch-failed {
    display: inline-grid;
    place-items: center;
    flex: none;
    color: var(--danger-text);
  }
  @keyframes unread-in {
    from {
      transform: translateY(-50%) scaleY(0.2);
      opacity: 0;
    }
  }
  :global(:root[data-motion="reduced"]) .channel-row.unread::before {
    animation: none;
  }
  @keyframes count-pop {
    from {
      transform: scale(0.4);
      opacity: 0;
    }
  }
  .ch-menu {
    display: inline-flex;
    opacity: 0;
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
    .count {
      transition: opacity var(--dur-quick) ease;
    }
  }
  .menu-head {
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
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
    padding: 6px var(--sp-2);
    background: transparent;
    color: var(--text-muted);
    text-align: left;
    border-radius: var(--radius-sm);
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
  /* …and a message that did not send is about you AND went wrong. */
  .dm-failed {
    color: var(--danger-text);
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
    background: var(--sel-bar), var(--bg-3);
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
    background: var(--bg-3);
    color: var(--text-muted);
    flex-shrink: 0;
  }
  /* Requests are neutral, not accented: a stranger knocking should read as
     something to deal with, never as something new and exciting. */
  .dm-item.requests .dm-notes-icon {
    background: var(--bg-3);
    color: var(--text-muted);
  }
  @keyframes vb-blink {
    50% {
      opacity: 0.3;
    }
  }
  :global(:root[data-motion="reduced"]) .vb-live {
    animation: none;
  }
  :global(:root[data-motion="reduced"]) .me-status-trigger:hover {
    transform: none;
  }
  .st-emoji {
    margin-right: 3px;
  }
  @media (pointer: fine) {
    .me:hover {
      background: var(--bg-3);
    }
  }
  /* Touch: taller rows (44px+ targets), slightly larger type, and the
     hover-revealed affordances (category +/trash, channel menu, mute bell)
     always visible at reduced opacity — hover doesn't exist on a phone. */
  @media (max-width: 768px) {
    /* Messages with no conversations yet. The block had no rules at all, so
       its face sat flush against the Notes row above it, a loose glyph with
       the sentence a line further down. */
    .empty-block {
      display: flex;
      flex-direction: column;
      align-items: flex-start;
      gap: var(--sp-3);
      padding: var(--sp-5) var(--sp-3) var(--sp-4);
    }
    .empty-block p {
      margin: 0;
    }
    .empty-ic {
      display: grid;
      place-items: center;
      width: var(--sp-6);
      height: var(--sp-6);
      border-radius: var(--radius-md);
      background: var(--bg-3);
      color: var(--text-muted);
    }
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
    /* A ⋮ on every heading was permanent ink on a phone. The long-press on
       the heading is the way in; the button stays for keyboards and screen
       readers, shown only while the heading has focus. */
    .cat-more {
      padding: var(--sp-1) var(--sp-2);
      opacity: 0;
      pointer-events: none;
    }
    .cat-head:focus-within .cat-more {
      opacity: 1;
      pointer-events: auto;
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
    /* Only one control is left in a touch heading (the ⋯, see the markup), so
       there is no neighbouring target for its overlay to land on and the
       heading can be its text's height again. With three buttons and a 44px
       floor, plus the section head's own 48px menu target above it, "CHANNELS"
       and the first category were 55px apart with nothing between them. */
    .channels-touch .cat-head {
      min-height: 32px;
      margin-top: var(--sp-1);
    }
    .channels-touch .mute-slot {
      display: none;
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
    .voice-bar .callbtn.sm {
      width: 44px;
      height: 44px;
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
  @keyframes uj-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
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

  /* ———————— the sidebar ———————— */
  .cols {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    height: 100%;
    background: var(--bg-1);
    border-right: 1px solid var(--hairline);
    overflow: hidden;
  }
  /* Two lines in the header row: the guild's name (the switcher), and under
     it how many members this device can reach. Invite spans both. The name's
     row sits on the conversation header's title line (app.css). */
  .place-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-content: center;
    align-items: center;
    column-gap: var(--sp-1);
    height: var(--header-h);
    flex: none;
    padding: var(--place-pad-t) var(--sp-2) 0 var(--sp-3);
    border-bottom: 1px solid var(--hairline);
    min-width: 0;
  }
  /* One line only (Messages, a guild you were removed from): centred on the
     same title line as the conversation header beside it. */
  .place-head:not(:has(.ph-reach)) {
    padding-top: 0;
    padding-bottom: var(--header-pad-b);
  }
  .ph-main {
    grid-column: 1;
    grid-row: 1;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
    /* This row + the 18px reach line fit the header row (app.css). */
    height: var(--place-name-h);
    margin-left: calc(-1 * var(--sp-1));
    padding: 0 var(--sp-1);
    border-radius: var(--radius-sm);
    color: var(--text);
    text-align: start;
  }
  .ph-main.static {
    pointer-events: none;
  }
  @media (pointer: fine) {
    button.ph-main:hover {
      background: var(--bg-3);
    }
  }
  .ph-main:focus-visible {
    outline: var(--focus-ring);
    outline-offset: -2px;
  }
  .ph-icon {
    flex: none;
    width: 24px;
    height: 24px;
    border-radius: var(--radius-sm);
    display: grid;
    place-items: center;
    overflow: hidden;
    background: color-mix(in srgb, var(--guild-accent, var(--text-muted)) 18%, var(--bg-3));
    color: color-mix(in srgb, var(--guild-accent, var(--text-muted)) 70%, var(--text));
    font-size: var(--fs-small);
    font-weight: 600;
  }
  .ph-icon img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  /* Moments nobody here has watched yet: the strip tile's ring, here too. */
  .ph-icon.moments {
    overflow: visible;
    box-shadow:
      0 0 0 1px var(--bg-1),
      0 0 0 2.5px var(--accent);
  }
  .ph-icon.moments img {
    border-radius: inherit;
  }
  .place-head.gone .ph-icon {
    filter: grayscale(1);
    opacity: 0.6;
  }
  .ph-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--display-font);
    font-size: var(--fs-title);
    font-weight: 600;
    letter-spacing: -0.01em;
  }
  .ph-chev {
    flex: none;
    display: grid;
    color: var(--text-faint);
    rotate: 90deg;
  }
  .ph-reach {
    grid-column: 1;
    grid-row: 2;
    justify-self: start;
    max-width: 100%;
    display: flex;
    align-items: center;
    gap: 5px;
    height: 18px;
    /* Its words start under the name's first letter: past the 24px icon and
       the gap, less this button's own inset. */
    margin-left: calc(24px + var(--sp-2) - 6px);
    padding: 0 6px;
    border-radius: var(--radius-sm);
    color: var(--text-faint);
    font-size: var(--fs-tiny);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .reach-n {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .place-head:global([data-reach="glyph"]) .reach-w {
    display: none;
  }
  .peers-toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    margin-top: var(--sp-2);
    padding: var(--sp-2) 0 0;
    border-top: 1px solid var(--hairline);
    border-radius: 0;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-small);
  }
  .peers-toggle .chev {
    display: inline-flex;
    transition: rotate var(--dur-quick) ease;
  }
  .peers-toggle .chev.open {
    rotate: 90deg;
  }
  .peers-note {
    margin: var(--sp-2) 0;
    color: var(--text-muted);
    font-size: var(--fs-small);
  }
  .peers-list {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 160px;
    overflow-y: auto;
  }
  .peers-list li {
    display: flex;
    justify-content: space-between;
    gap: var(--sp-2);
    padding: 4px 0;
    font-size: var(--fs-small);
  }
  .peers-list li.none {
    color: var(--text-muted);
  }
  .peer-ok {
    color: var(--ok-text);
  }
  @media (pointer: fine) {
    .ph-reach:hover {
      background: var(--bg-3);
      color: var(--text-muted);
    }
  }
  /* Hollow = nobody, half = some, full = everyone: shape, not colour. */
  .reach-glyph {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    box-shadow: inset 0 0 0 1.5px currentColor;
    background: linear-gradient(90deg, currentColor 50%, transparent 50%);
  }
  .reach-glyph.none {
    background: none;
  }
  .reach-glyph.all {
    background: currentColor;
  }
  .ph-acts {
    grid-column: 2;
    grid-row: 1 / span 2;
    display: flex;
    align-items: center;
    gap: 2px;
  }
  .reach-pop {
    position: fixed;
    z-index: var(--z-layer);
    width: 240px;
    padding: var(--sp-3);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    font-size: var(--fs-compact);
    color: var(--text);
  }
  .reach-pop p {
    margin: 0 0 var(--sp-2);
    line-height: 1.45;
  }
  .jump-row {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    margin: var(--sp-2) 10px;
  }
  .jump {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    height: 32px;
    padding: 0 6px 0 10px;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-muted);
    font-size: var(--fs-ui);
    text-align: start;
  }
  .jump-lbl {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (pointer: fine) {
    .jump:hover {
      color: var(--text);
    }
  }
  .jump:focus-visible {
    outline: var(--focus-ring);
    outline-offset: 1px;
  }
  .inbox-wrap {
    position: relative;
    display: flex;
    flex: none;
  }
  .inbox-btn.on {
    background: var(--bg-3);
    color: var(--text);
  }
  .inbox-badge {
    position: absolute;
    top: -2px;
    right: -2px;
    display: flex;
    pointer-events: none;
  }
  /* A short window (a phone on its side) has room for the list or for the
     Unread messages block, not both: two channel rows were left between the
     header and the block. The Messages tile on the strip still carries the
     count. */
  @media (max-height: 500px) {
    .unread-block {
      display: none;
    }
  }
  .list-wrap {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  /* "↑ Unread" / "↓ 2 mentions": a band at the list's edge, in the flow,
     not a pill floating over it. Floating, it sat on whichever row was at
     the edge — in phone landscape, on the last visible channel — and the
     row under it could not be read or tapped. In the flow the list gives up
     the band's height instead (and only while there is something past the
     edge), so it covers nothing and casts no shadow. */
  .edge-pill {
    flex: none;
    align-self: center;
    margin: var(--sp-1) 0;
    height: 24px;
    padding: 0 10px;
    border-radius: 999px;
    background: var(--bg-3);
    border: none;
    color: var(--text);
    font-size: var(--fs-small);
    font-weight: 600;
    white-space: nowrap;
  }
  .edge-pill.up {
    order: -1;
  }
  .edge-pill.down {
    order: 1;
  }
  .nudge-row {
    padding: var(--sp-1) 0 6px;
  }

  /* ———————— rows ———————— */
  .channel-row {
    position: relative;
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
    margin-bottom: 1px;
  }
  .channel {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-height: 32px;
    padding: 0 var(--sp-2);
    border-radius: inherit;
    color: var(--text-muted);
    font-size: var(--fs-ui);
    text-align: start;
  }
  .channel > :global(svg:first-child) {
    flex: none;
    color: var(--text-faint);
  }
  .ch-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    unicode-bidi: plaintext;
  }
  @media (pointer: fine) {
    .channel-row:hover {
      background: color-mix(in srgb, var(--bg-3) 60%, transparent);
    }
    .channel-row:hover .channel {
      color: var(--text);
    }
  }
  .channel:focus-visible {
    outline: var(--focus-ring);
    outline-offset: -2px;
  }
  .channel-row.unread .channel {
    color: var(--text);
  }
  .channel-row.unread .ch-name {
    font-weight: 600;
  }
  /* Selected: the fill alone is 1.13:1 in the light theme, so a 2px ink bar
     on the start edge is the cue. It is a short straight mark set 3px in,
     painted as a background layer, not an inset shadow along the whole edge:
     on a pack with pill-shaped rows (Atrium's 24px radius) the shadow bent
     round the curve into a half-moon. */
  .channel-row.active {
    background: var(--sel-bar), var(--bg-3);
  }
  .cols {
    --sel-bar: linear-gradient(var(--text), var(--text)) 3px 50% / 2px calc(100% - 16px) no-repeat;
  }
  .channel-row.active .channel {
    color: var(--text);
  }
  .channel-row.active:not(.unread) .ch-name {
    font-weight: 500;
  }
  .channel-row:global([data-menu-target]) {
    background: var(--bg-3);
  }
  .ch-mark {
    flex: none;
    display: flex;
    align-items: center;
    margin-left: auto;
  }
  .ch-heads {
    flex: none;
    margin-left: auto;
    color: var(--ok-text);
    font-size: var(--fs-micro);
    font-weight: 600;
    white-space: nowrap;
  }
  .channel-row.dragging {
    opacity: 0.4;
  }
  .channel-row.drop-before::before,
  .channel-row.drop-after::after {
    content: "";
    position: absolute;
    left: var(--sp-2);
    right: var(--sp-2);
    height: 2px;
    border-radius: 2px;
    background: var(--accent-hover);
    pointer-events: none;
  }
  .channel-row.drop-before::before {
    top: -1px;
  }
  .channel-row.drop-after::after {
    bottom: -1px;
  }
  .add-ch {
    margin-top: var(--sp-2);
  }
  .add-ch :global(.trigger.own) {
    display: flex;
    width: 100%;
    min-height: 32px;
    padding: 0 var(--sp-2);
    border-radius: var(--radius-sm);
    color: var(--text-faint);
    font-size: var(--fs-ui);
  }
  .add-ch :global(.menu-root) {
    display: block;
  }
  @media (pointer: fine) {
    .add-ch :global(.trigger.own:hover) {
      background: color-mix(in srgb, var(--bg-3) 60%, transparent);
      color: var(--text);
    }
  }
  .add-ch-in {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }

  /* ———————— unread messages ———————— */
  .unread-block {
    flex: none;
    border-top: 1px solid var(--hairline);
    padding: 10px var(--sp-2) 6px;
  }
  .ub-head {
    padding: 0 var(--sp-2) 6px;
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
  }
  .ub-row {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    min-height: 30px;
    padding: 0 var(--sp-2);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--fs-ui);
    font-weight: 600;
    text-align: start;
  }
  .ub-ic {
    display: grid;
    place-items: center;
    width: 20px;
    color: var(--text-muted);
  }
  .ub-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ub-more {
    padding: 2px var(--sp-2);
    color: var(--text-muted);
    font-size: var(--fs-small);
  }
  @media (pointer: fine) {
    .ub-row:hover,
    .ub-more:hover {
      background: color-mix(in srgb, var(--bg-3) 60%, transparent);
    }
  }

  /* ———————— the call dock ———————— */
  .voice-bar {
    flex: none;
    display: flex;
    flex-direction: column;
    gap: 6px;
    /* Part of the sidebar, not a card laid on it: flush, a hairline above,
       no frame and no shadow (only floating things cast one). */
    padding: var(--sp-2) 10px var(--sp-2) var(--sp-3);
    border-top: 1px solid var(--hairline);
    background: var(--bg-2);
  }
  .voice-line {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    min-height: 36px;
    padding: 0 var(--sp-3);
    border-top: 1px solid var(--hairline);
    background: var(--bg-2);
    color: var(--ok-text);
    font-size: var(--fs-small);
    font-variant-numeric: tabular-nums;
    text-align: start;
  }
  .voice-line:hover {
    background: var(--bg-3);
  }
  .voice-line.trouble {
    color: var(--text-muted);
  }
  .vl-text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .vl-return {
    flex: none;
    font-weight: 600;
  }
  .vb-info {
    display: flex;
    min-width: 0;
    flex: 1;
    padding: 0;
    text-align: start;
    color: var(--text);
  }
  .vb-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .vb-title {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
  }
  .vb-label {
    font-weight: 600;
    font-size: var(--fs-ui);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .vb-guild {
    color: var(--text-muted);
    font-size: var(--fs-small);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .vb-state {
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--ok-text);
    font-size: var(--fs-small);
    font-variant-numeric: tabular-nums;
  }
  .vb-state.held {
    color: var(--text-muted);
  }
  .vb-live {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--ok);
  }
  .vb-live.held {
    background: var(--text-faint);
    animation: vb-pulse 1.2s ease-in-out infinite;
  }
  @keyframes vb-pulse {
    50% {
      opacity: 0.35;
    }
  }
  :global(:root[data-motion="reduced"]) .vb-live.held {
    animation: none;
  }
  .vb-faces {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
  }
  .vb-face {
    position: relative;
    display: flex;
    border-radius: 50%;
  }
  .vb-face.speaking {
    box-shadow: 0 0 0 2px var(--ok);
  }
  .vb-muted {
    position: absolute;
    right: -3px;
    bottom: -3px;
    display: grid;
    place-items: center;
    width: 13px;
    height: 13px;
    border-radius: 50%;
    background: var(--bg-2);
    color: var(--text-muted);
  }
  .vb-more {
    color: var(--text-muted);
    font-size: var(--fs-small);
    font-variant-numeric: tabular-nums;
  }
  .vb-actions {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
  }
  .vb-actions .hang {
    margin-left: auto;
  }

  /* ———————— you ———————— */
  .me-row {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    height: 56px;
    padding: 0 var(--sp-2) 0 10px;
    border-top: 1px solid var(--hairline);
  }
  .me {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    padding: var(--sp-1);
    border-radius: var(--radius-sm);
    text-align: start;
    color: var(--text);
  }
  @media (pointer: fine) {
    .me:hover {
      background: var(--bg-3);
    }
  }
  .me-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .me-text strong {
    font-size: var(--fs-ui);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .small-status {
    color: var(--text-muted);
    font-size: var(--fs-small);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .me-mic.on {
    color: var(--danger-text);
  }
  .me-gear,
  .me-mic {
    flex: none;
  }

  /* Phone (§2.6): 44px visible rows inside 48px hit areas, a 40px jump field,
     32px section labels. Last in the sheet so it wins over the desktop rows. */
  @media (max-width: 768px) {
    .channel {
      min-height: 44px;
      font-size: var(--fs-body);
    }
    .channel-row {
      margin-bottom: 2px;
    }
    .channel::after {
      content: "";
      position: absolute;
      inset: -2px 0;
    }
    .cat-head {
      min-height: 32px;
    }
    .jump {
      height: 40px;
    }
    .jump-row {
      margin: var(--sp-2) var(--sp-3);
    }
    .ub-row {
      min-height: 44px;
    }
    .add-ch :global(.trigger.own) {
      min-height: 44px;
    }
    .place-head {
      padding-right: var(--sp-1);
    }
    .ph-invite,
    .ph-gear {
      width: 44px;
      height: 44px;
    }
  }
</style>
