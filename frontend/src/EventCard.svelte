<script>
  // One calendar event, everywhere an event appears: the per-guild panel and
  // the aggregated "Your calendar". Editorial card: a KICKER line above the
  // title is the state machine (when / starts-in / live / ended), color is
  // reserved for state, boxes are reserved for heat — an ordinary upcoming
  // event is ink on the page, a soon event is warm, the live one is the only
  // surfaced card in the list. One phase-driven CTA; everything else folds
  // into ⋯.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { S, nameFor, flash, openContextMenu, selectGuild, refreshGuilds, jumpToChannel } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PERM, has } from "./lib/perms.js";
  import { addReminder } from "./lib/scheduled.svelte.js";
  import { haptic } from "./lib/touch.js";
  import { clockOpts } from "./lib/state.svelte.js";
  import {
    fmtEventTime,
    eventPhase,
    fmtCountdown,
    isPast,
    rsvpBuckets,
    guildTint,
    guildInitials,
    downloadICS,
    icsName,
    eventReminderTimes,
    loadEvents,
  } from "./lib/events.svelte.js";

  // bubble: "date" (default) or "time" — in agenda contexts a day heading
  // already owns the date, so the bubble shows the start time instead of
  // printing the same fact twice. onJoinVoice is App's real voice lifecycle
  // (mesh, knock, call view), threaded down so a voice-channel-located event's
  // Join can enter the call exactly like clicking the channel would.
  let { ev, g, showGuild = false, onEdit, onJoinVoice, bubble = "date" } = $props();

  // Soon-aware tick: 30s normally, 10s inside the T-60m window so the
  // countdown never visibly stalls. Minute-grained copy needs no 1s tick.
  // Paused while the tab is hidden; a visibility flip re-syncs immediately.
  let now = $state(Date.now() / 1000);
  $effect(() => {
    let t;
    const tick = () => {
      now = Date.now() / 1000;
      if (!document.hidden) schedule();
    };
    const schedule = () => {
      clearTimeout(t);
      t = setTimeout(tick, eventPhase(ev, Date.now() / 1000) === "soon" ? 10000 : 30000);
    };
    const onVis = () => {
      if (!document.hidden) tick();
    };
    document.addEventListener("visibilitychange", onVis);
    schedule();
    return () => {
      clearTimeout(t);
      document.removeEventListener("visibilitychange", onVis);
    };
  });

  const phase = $derived(eventPhase(ev, now));
  const past = $derived(isPast(ev, now));
  const mine = $derived(ev.rsvps?.[S.identity.fingerprint] || "");
  const buckets = $derived(rsvpBuckets(ev));
  // Mirrors the backend's mayCurateEvent gate (author or ManageMessages), so
  // the actions only appear where they would succeed.
  const canEdit = $derived(
    !!g && (ev.createdBy === S.identity.fingerprint || g.isOwner || has(g.myPerms || 0, PERM.MANAGE_MESSAGES)),
  );
  const mon = $derived(new Date(ev.startUnix * 1000).toLocaleDateString([], { month: "short" }));
  const dayN = $derived(new Date(ev.startUnix * 1000).getDate());
  const fmtT = (u) =>
    new Date(u * 1000).toLocaleTimeString([], { hour: "numeric", minute: "2-digit", ...clockOpts() });
  // The time bubble splits "6:00 PM" into a big number over a micro meridiem;
  // 24h locales just show the number.
  const bubbleTime = $derived.by(() => {
    const parts = fmtT(ev.startUnix).split(" ");
    return { t: parts[0], m: parts[1] || "" };
  });
  const weekday = $derived(
    new Date(ev.startUnix * 1000).toLocaleDateString([], { weekday: "long" }),
  );

  // ---- channel-located events ----
  // The location is a REAL channel of this guild when the record names one AND
  // it still resolves in OUR copy of the guild — the same receive-side rule
  // the backend's announcer holds: a record naming a foreign or vanished
  // channel renders as its free-text label, never as a door. In that mode the
  // event needs no meeting room for members: Join IS the channel (voice = its
  // call), and the guest room remains only as the outsiders' bridge.
  const locCh = $derived(g?.channels?.find((c) => c.id === ev.locationChannelId) || null);
  const locIsVoice = $derived(locCh?.type === "voice");
  // DM-located events: the DM's single channel doubles as its call (the same
  // fact ChatHeader's "Call" button rests on), so a text-typed channel still
  // joins like a voice room here. Notes is a DM of one — no call to join, so
  // it never takes the call arm (and Notes events carry no channel anyway).
  const isDM = $derived(g?.kind === "dm");
  const isNotes = $derived(!!g?.dmNotes);
  const locIsCall = $derived(locIsVoice || (isDM && !isNotes && !!locCh));

  async function joinChannelLoc() {
    if (!locCh) return;
    const chId = locCh.id;
    const voice = locIsCall;
    S.modal = null; // the channel is the destination; the calendar's job is done
    haptic("light");
    await jumpToChannel(chId);
    // Voice room: enter its call through App's one true join path — it owns
    // the knock-if-locked courtesy, mic capture and the call view. Without the
    // callback (unexpected) navigation alone is still the honest 90%.
    if (voice) onJoinVoice?.(chId);
  }

  async function rsvp(state) {
    const next = mine === state ? "" : state; // tapping your answer clears it
    // Optimistic: flip the chip under the finger; the guild-updated refresh
    // confirms, and a failure reloads the truth.
    const r = { ...(ev.rsvps || {}) };
    if (next) r[S.identity.fingerprint] = next;
    else delete r[S.identity.fingerprint];
    ev.rsvps = r;
    try {
      await api.rsvpEvent(ev.guildId, ev.id, next);
    } catch (err) {
      flash(err);
      loadEvents(ev.guildId);
    }
  }

  // ---- facepile ----
  // Faces instead of a names line: going first, maybe faded behind them, +N
  // for the rest. Tap → full names, bucketed — hover tooltips don't exist on
  // a phone, a tap target does.
  const pile = $derived(
    [
      ...buckets.going.map((f) => ({ f, st: "going" })),
      ...buckets.maybe.map((f) => ({ f, st: "maybe" })),
    ].slice(0, 5),
  );
  const pileExtra = $derived(Math.max(0, buckets.going.length + buckets.maybe.length - 5));
  const countsLine = $derived.by(() => {
    const parts = [];
    if (buckets.going.length) parts.push(`${buckets.going.length} going`);
    if (buckets.maybe.length) parts.push(`${buckets.maybe.length} maybe`);
    if (buckets.no.length) parts.push(`${buckets.no.length} can't`);
    return parts.join(" · ");
  });
  function showWho(e) {
    const bucketItems = (label, list) =>
      list.length ? [{ header: true, label }, ...list.map((f) => ({ label: nameFor(f) }))] : [];
    openContextMenu(
      e,
      [
        ...bucketItems("Going", buckets.going),
        ...bucketItems("Maybe", buckets.maybe),
        ...bucketItems("Can't", buckets.no),
      ],
      { title: ev.title },
    );
  }

  function remind(e) {
    const times = eventReminderTimes(ev.startUnix);
    if (!times.length) {
      flash("It's already started — go!", "info");
      return;
    }
    // Jump target for the fired reminder: the event's OWN channel when it has
    // one, else the guild's first ordinary text channel.
    const chId =
      locCh?.id ||
      g?.channels?.find((c) => !c.parent && c.type !== "voice" && c.type !== "forum")?.id ||
      g?.channels?.[0]?.id ||
      "";
    openContextMenu(
      e,
      times.map((t) => ({
        label: t.label,
        icon: "bell",
        onClick: () => {
          addReminder(chId, "", `📅 ${ev.title}`, t.at);
          flash("Reminder set", "success");
        },
      })),
      { title: "Remind me" },
    );
  }

  async function addToCal() {
    try {
      downloadICS(`${icsName(ev.title)}.ics`, await api.eventICS(ev.guildId, ev.id));
      flash("Saved — open it with your calendar app", "success");
    } catch (err) {
      flash(err);
    }
  }

  // ---- guest access ----
  // Only the account that opened the event to guests can revoke: the room and
  // its tokens live on that member's node — the same rule the backend enforces
  // on receive, mirrored here so the action only appears where it works.
  const isGuestHost = $derived(!!ev.guestUrl && ev.guestHost === S.identity.fingerprint);
  // Guests are for rooms with people in them — a meeting is already a guest
  // room (the backend refuses a second), and Notes is a party of one: nobody
  // to admit, so the door never renders there.
  const canInviteGuests = $derived(canEdit && !ev.guestUrl && g?.kind !== "meeting" && !isNotes);

  function inviteGuests(e) {
    // The door choice happens at mint time and governs the GUEST LINK only:
    // knock (default, safe to forward the link anywhere) or walk-in for an
    // openly shared event. Members always walk in — their Join is a real
    // invite, not the guest door.
    openContextMenu(
      e,
      [
        { label: "Guests knock, you admit", icon: "door", onClick: () => openGuests(false) },
        { label: "Open door — guests walk in", icon: "link", onClick: () => openGuests(true) },
      ],
      { title: "Open meeting room" },
    );
  }

  async function openGuests(autoAdmit) {
    try {
      const updated = await api.openEventGuests(ev.guildId, ev.id, autoAdmit);
      ev.guestUrl = updated.guestUrl;
      ev.guestHost = updated.guestHost;
      ev.memberCode = updated.memberCode;
      await copyGuestLink();
      loadEvents(ev.guildId);
    } catch (err) {
      flash(err);
    }
  }

  async function copyGuestLink() {
    try {
      await navigator.clipboard?.writeText(ev.guestUrl);
      flash("Guest link copied — anyone with it can join from a browser", "success");
    } catch {
      flash("Couldn't copy — long-press the link to copy it", "info");
    }
  }

  // ---- member Join: the threshold ----
  // People already IN this guild/DM tap Join and walk straight in as
  // themselves — the memberCode on the event is a real invite into the meeting
  // room (full identity, E2EE, never the guest knock). The veil covers the
  // ~500ms doorway: who you are, that it's E2EE, then you land mid-fade in the
  // room. A minimum dwell keeps a fast join from strobing; a slow one is
  // masked and says "Still knocking…" (JoinVeil owns that line).
  let joining = $state(false);
  async function joinRoom() {
    if (joining) return;
    joining = true;
    S.joinVeil = { title: ev.title };
    const dwell = new Promise((r) => setTimeout(r, 500));
    try {
      const [room] = await Promise.all([api.joinEventRoom(ev.guildId, ev.id), dwell]);
      // A fresh join means a guild S.guilds hasn't heard of yet — refresh
      // before navigating or selectGuild lands on nothing.
      if (!S.guilds.some((x) => x.id === room.id)) await refreshGuilds();
      await selectGuild(room.id);
      S.modal = null; // the room is the destination; the calendar's job is done
      haptic("light"); // you're through the door
      S.joinVeil = { title: ev.title, leaving: true };
      setTimeout(() => {
        if (S.joinVeil?.leaving) S.joinVeil = null;
      }, 260);
    } catch (err) {
      S.joinVeil = null; // drop the veil before the honest flash
      flash(err); // e.g. "this event's room has ended" — honest, not silent
      loadEvents(ev.guildId); // a dead room usually means a stale card too
    } finally {
      joining = false;
    }
  }

  // ---- the ⋯ menu ----
  // The whole former icon toolbar folds into one overflow: Remind / .ics /
  // room / edit / danger. Two-tap arming lives IN the item label (keepOpen
  // keeps the sheet up for the first tap), reusing the same 2.6s timers.
  let menuAnchor = null;
  const anchorEvt = () => {
    // Submenus (Remind me…, Open meeting room…) open after the ⋯ menu closed —
    // re-anchor them to the ⋯ button instead of a stale pointer position.
    const r = menuAnchor?.getBoundingClientRect();
    return {
      clientX: r ? r.left : window.innerWidth / 2,
      clientY: r ? r.bottom + 4 : window.innerHeight / 2,
      preventDefault() {},
      stopPropagation() {},
    };
  };
  function buildMenu() {
    return [
      !past && { label: "Remind me…", icon: "bell", onClick: () => remind(anchorEvt()) },
      { label: "Add to calendar (.ics)", icon: "download", onClick: addToCal },
      canInviteGuests && {
        // A channel-located event already has its members' door (the channel):
        // the room is then explicitly the OUTSIDERS' bridge, and says so.
        label: locCh ? "Invite outside guests…" : "Open meeting room…",
        icon: "camera",
        onClick: () => inviteGuests(anchorEvt()),
      },
      !!ev.guestUrl && { label: "Copy guest link", icon: "copy", onClick: copyGuestLink },
      canEdit && !!onEdit && { label: "Edit event", icon: "edit", onClick: () => onEdit(ev) },
      { sep: true },
      isGuestHost && canEdit && !!ev.guestUrl && {
        label: revokeArmed ? "Tap again — ends the room for everyone" : "End meeting room…",
        icon: "close",
        danger: true,
        keepOpen: !revokeArmed,
        onClick: revokeGuests,
      },
      canEdit && {
        label: armed ? "Tap again — deletes for everyone" : "Delete event…",
        icon: "trash",
        danger: true,
        keepOpen: !armed,
        onClick: del,
      },
    ];
  }
  function openMenu(e) {
    menuAnchor = e.currentTarget;
    openContextMenu(e, buildMenu(), { title: ev.title });
  }
  // Re-render the OPEN menu after an arming state flips, so the label is the
  // confirmation ("Tap again — …") instead of a second identical line.
  function refreshMenu() {
    if (S.contextMenu) S.contextMenu = { ...S.contextMenu, items: buildMenu().filter(Boolean) };
  }

  // Two-tap revoke, armed in place inside the menu.
  let revokeArmed = $state(false);
  let revokeT;
  async function revokeGuests() {
    if (!revokeArmed) {
      revokeArmed = true;
      refreshMenu();
      revokeT = setTimeout(() => {
        revokeArmed = false;
        refreshMenu();
      }, 2600);
      return;
    }
    clearTimeout(revokeT);
    revokeArmed = false;
    try {
      const updated = await api.revokeEventGuests(ev.guildId, ev.id);
      ev.guestUrl = updated.guestUrl || "";
      ev.guestHost = updated.guestHost || "";
      ev.memberCode = updated.memberCode || "";
      flash("Room closed — the guest link and member Join are both dead", "success");
      loadEvents(ev.guildId);
    } catch (err) {
      flash(err);
    }
  }

  // Two-tap delete: arms, then confirms in place. A ConfirmDialog would
  // replace S.modal — i.e. close the calendar the user is standing in.
  let armed = $state(false);
  let armT;
  async function del() {
    if (!armed) {
      armed = true;
      refreshMenu();
      armT = setTimeout(() => {
        armed = false;
        refreshMenu();
      }, 2600);
      return;
    }
    clearTimeout(armT);
    armed = false;
    try {
      await api.deleteEvent(ev.guildId, ev.id);
      await loadEvents(ev.guildId);
    } catch (err) {
      flash(err);
    }
  }
</script>

<article class="evcard" class:soon={phase === "soon"} class:live={phase === "live"} class:ended={phase === "ended"}>
  <div class="date" aria-hidden="true">
    {#if bubble === "time"}
      <span class="bt">{bubbleTime.t}</span>
      {#if bubbleTime.m}<span class="bm">{bubbleTime.m}</span>{/if}
    {:else}
      <span class="mon">{mon}</span>
      <span class="dayn">{dayN}</span>
    {/if}
  </div>
  <div class="body">
    <header class="top">
      <div class="kicker st" class:k-soon={phase === "soon"} class:k-live={phase === "live"}>
        {#if showGuild && g}
          <!-- The blend's source tag: a guild wears its rail badge, a DM wears
               the person/group with a tap-through into the conversation, and
               Notes wears a lock — Private is a state, not a place. -->
          {#if isNotes}
            <span class="srctag private"><Icon name="lock" size={9} /> Private</span>
          {:else if isDM}
            <button
              class="srctag dm"
              title="Open the conversation"
              onclick={async () => { S.modal = null; await selectGuild(g.id); }}
            >
              <Icon name="forum" size={10} />
              <span class="srcname">{g.name}</span>
            </button>
          {:else}
            <span class="gbadge" style={guildTint(g.id)}>{guildInitials(g.name)}</span>
            <span class="gname">{g.name}</span>
          {/if}
          <span class="ksep" aria-hidden="true">·</span>
        {/if}
        {#if phase === "live"}
          <span class="now-dot"></span><span>Happening now</span>
        {:else if phase === "soon"}
          <span>{fmtCountdown(ev.startUnix, now)}</span>
        {:else if phase === "ended"}
          <span>Ended · {fmtT(ev.endUnix || ev.startUnix + 3600)}</span>
        {:else}
          <span>{weekday} · {fmtT(ev.startUnix)}</span>
        {/if}
      </div>
      {#if canEdit}
        <!-- Delete, surfaced: the ⋯ menu kept it too well hidden. Same
             two-tap arming (shared `armed` state + timer, so the menu item
             and this button arm each other), same api.deleteEvent — this is
             a louder door to the SAME machinery, not a second path. -->
        <button
          class="del"
          class:armed
          aria-label={armed ? "Tap again — deletes for everyone" : "Delete event"}
          title={armed ? "Tap again — deletes for everyone" : "Delete event"}
          onclick={del}
        >
          {#if armed}<span class="del-txt">Tap again</span>{:else}<Icon name="trash" size={14} />{/if}
        </button>
      {/if}
      <button class="dots" aria-label="Event options" title="Event options" onclick={openMenu}>
        <Icon name="dots" size={15} />
      </button>
    </header>
    <strong class="title">{ev.title}</strong>
    <div class="meta muted">
      <Icon name="clock" size={11} />
      <span>{fmtEventTime(ev)}</span>
      {#if locCh}
        <span class="dotsep">·</span>
        <!-- The location IS a channel: a door, not a caption — tap to stand
             in it (chat only; the call stays behind the explicit Join). In a
             DM the channel's stored name ("dm") means nothing — say what it
             is: this very conversation. -->
        <button class="locbtn" title={isDM ? "Open the conversation" : `Open ${locIsVoice ? "the voice channel" : "the channel"}`} onclick={async () => { S.modal = null; await jumpToChannel(locCh.id); }}>
          <Icon name={isDM ? "phone" : locIsVoice ? "speaker" : "hash"} size={11} />
          <span class="locname">{isDM ? "This chat" : locCh.name}</span>
        </button>
      {:else if ev.location}
        <span class="dotsep">·</span>
        <span class="loc">{ev.location}</span>
      {/if}
    </div>
    {#if ev.details}
      <p class="details">{ev.details}</p>
    {/if}
    <div class="ctarow">
      <div class="rsvps" class:readonly={phase === "ended"} role="group" aria-label="RSVP">
        <button class="chip going" class:on={mine === "going"} onclick={() => rsvp("going")}>
          <Icon name="check" size={11} /> Going{#if buckets.going.length}<span class="cnt">{buckets.going.length}</span>{/if}
        </button>
        <button class="chip maybe" class:on={mine === "maybe"} onclick={() => rsvp("maybe")}>
          Maybe{#if buckets.maybe.length}<span class="cnt">{buckets.maybe.length}</span>{/if}
        </button>
        <button class="chip no" class:on={mine === "no"} onclick={() => rsvp("no")}>
          <Icon name="close" size={10} /> No{#if buckets.no.length}<span class="cnt">{buckets.no.length}</span>{/if}
        </button>
      </div>
      <span class="spring"></span>
      {#if locCh && phase !== "ended"}
        <!-- Channel mode: Join IS the channel — no meeting guild is minted,
             ever. A voice location also enters its call; a text one lands in
             the chat. This REPLACES the room join for this event; the guest
             room (if opened) stays the outsiders-only door below. -->
        <button class="gjoin" onclick={joinChannelLoc}>
          {#if locIsCall}
            <!-- A DM location wears the phone (its channel IS the call);
                 a guild voice channel keeps the speaker. -->
            {#if phase === "live"}<Icon name={isDM ? "phone" : "speaker"} size={13} /> Join the call
            {:else if phase === "soon"}<Icon name={isDM ? "phone" : "speaker"} size={13} /> Join early
            {:else if isDM}<Icon name="phone" size={13} /> Join the call
            {:else}<Icon name="speaker" size={13} /> Join in 🔊{/if}
          {:else}
            <Icon name="hash" size={13} /> Go to #{locCh.name}
          {/if}
        </button>
      {:else if ev.guestUrl && (ev.memberCode || isGuestHost)}
        <!-- THE button. The only filled control on the page when live. -->
        <!-- Visible text IS the accessible name ("Join now" / "Join early"),
             so voice control users can say what they see. -->
        <button class="gjoin" disabled={joining} onclick={joinRoom}>
          {#if joining}<span class="spin" aria-hidden="true"></span> Opening the room…
          {:else if phase === "live"}<Icon name="camera" size={13} /> Join now
          {:else if phase === "soon"}<Icon name="camera" size={13} /> Join early
          {:else}<Icon name="camera" size={13} /> Join{/if}
        </button>
      {:else if ev.guestUrl}
        <span class="gtag"><Icon name="camera" size={11} /> Room open</span>
      {/if}
      {#if isGuestHost && canEdit && ev.guestUrl}
        <!-- The loop-closer, right next to the room state: a meeting guild lives
             until you end it (or the 30-day backstop). This IS the "delete it"
             the room needs — same action as the menu's, surfaced where the eye
             already is. Two-tap so a mis-tap can't tear down a live meeting. -->
        <button class="gend" onclick={revokeGuests}>
          {revokeArmed ? "Tap again to end" : "End room"}
        </button>
      {/if}
    </div>
    {#if pile.length || buckets.no.length || ev.guestUrl}
      <div class="footrow">
        {#if pile.length || buckets.no.length}
          <button class="faces" aria-label="See who's coming" onclick={showWho}>
            {#each pile as p (p.f)}
              <span class="face" class:maybe={p.st === "maybe"}><Avatar name={nameFor(p.f)} size={22} /></span>
            {/each}
            {#if pileExtra}<span class="face more">+{pileExtra}</span>{/if}
            <span class="counts">{countsLine}</span>
          </button>
        {/if}
        <span class="spring"></span>
        {#if ev.guestUrl && !past}
          <!-- The guest door, as a whisper — outsiders only; members use Join. -->
          <button class="gcopy" aria-label="Copy the guest link" title="For people outside this {g?.kind === 'dm' ? 'chat' : 'guild'} — they join from a browser and knock" onclick={copyGuestLink}>
            <Icon name="copy" size={11} /> Guest link
          </button>
        {/if}
      </div>
    {/if}
  </div>
</article>

<style>
  /* Default temperature: ink on the page. No box, no border — the list's
     hairlines (owned by the parent) do the separating. */
  .evcard {
    display: flex;
    gap: var(--sp-3);
    padding: var(--sp-3) 2px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-lg);
    text-align: left;
  }
  /* T-60m: warmth, not alarm. A still, warm frame lifts the card off the page. */
  .evcard.soon {
    border-color: color-mix(in srgb, var(--warn) 45%, var(--border));
    padding: var(--sp-3);
    box-shadow: 0 0 10px color-mix(in srgb, var(--warn) 8%, transparent);
  }
  /* LIVE: the only surfaced card in the list — one beacon, breathing slowly. */
  .evcard.live {
    background: var(--bg-1);
    border-color: color-mix(in srgb, var(--ok) 45%, transparent);
    padding: var(--sp-3);
    animation: ev-breathe 4s ease-in-out infinite;
  }
  @keyframes ev-breathe {
    0%,
    100% {
      box-shadow: 0 0 0 0 transparent;
    }
    50% {
      box-shadow: 0 0 14px color-mix(in srgb, var(--ok) 10%, transparent);
    }
  }
  /* Ended: dimmed with ink, not opacity — a blanket fade muddies facepiles. */
  .evcard.ended .title {
    color: var(--text-muted);
  }
  .evcard.ended .meta,
  .evcard.ended .details {
    color: var(--text-faint);
  }
  .date {
    flex-shrink: 0;
    width: 46px;
    height: 50px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-md);
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .evcard.soon .date {
    background: color-mix(in srgb, var(--warn) 14%, transparent);
    color: var(--warn-text);
  }
  .evcard.live .date {
    background: var(--ok-soft);
    color: var(--ok-text);
  }
  .evcard.ended .date {
    background: var(--bg-2);
    color: var(--text-faint);
  }
  .mon {
    font-size: var(--fs-tiny);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    line-height: 1.1;
  }
  .dayn {
    font-size: var(--fs-display);
    font-weight: 700;
    line-height: 1.1;
    font-variant-numeric: tabular-nums;
  }
  /* End room: a quiet danger-text button, not a loud one — ending a meeting is
     deliberate but not the card's headline. Two-tap arming reuses revokeArmed. */
  .gend {
    flex-shrink: 0;
    padding: 3px 9px;
    border-radius: 999px;
    background: transparent;
    border: 1px solid color-mix(in srgb, var(--danger) 35%, transparent);
    color: var(--danger-text);
    font-size: var(--fs-tiny);
    font-weight: 600;
  }
  @media (pointer: coarse), (max-width: 768px) {
    .gend {
      min-height: var(--tap-min);
      padding-inline: var(--sp-3);
    }
  }

  /* Time bubble (agenda contexts, where the day heading owns the date). */
  .bt {
    font-size: var(--fs-ui);
    font-weight: 700;
    line-height: 1.15;
    font-variant-numeric: tabular-nums;
  }
  .bm {
    font-size: var(--fs-micro);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    line-height: 1.1;
  }
  .body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .top {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
    min-width: 0;
  }
  /* The kicker IS the state machine: one line above the title answers
     when / what-now before the eye reaches the words. */
  .st {
    flex: 1;
    min-width: 0;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: var(--text-faint);
    padding-top: 3px;
    white-space: nowrap;
    overflow: hidden;
  }
  .st.k-soon {
    color: var(--warn-text);
  }
  .st.k-live {
    color: var(--ok-text);
  }
  .now-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--ok);
    flex-shrink: 0;
    animation: ev-now-pulse 1.4s ease-in-out infinite;
  }
  @keyframes ev-now-pulse {
    50% {
      opacity: 0.3;
    }
  }
  .gbadge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    border-radius: 5px;
    font-size: var(--fs-micro);
    letter-spacing: 0;
    flex-shrink: 0;
  }
  .gname {
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 14ch;
    color: var(--text-muted);
  }
  /* ---- source tags (the "Your calendar" blend) ---- */
  /* Shared pill geometry; the two flavors diverge in voice: Private is a
     quiet uppercase seal (state, not place), a DM is a tinted door you can
     tap through to the conversation. */
  .srctag {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
    padding: 1px 8px;
    min-width: 0;
    border-radius: 999px;
    font-size: var(--fs-tiny);
    font-weight: 700;
    line-height: 1.6;
  }
  .srctag.private {
    background: var(--bg-2);
    border: 1px solid var(--border);
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    font-size: var(--fs-micro);
  }
  .srctag.dm {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .srctag.dm:hover {
    background: color-mix(in srgb, var(--accent) 22%, transparent);
  }
  .srcname {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 14ch;
  }
  .ksep {
    color: var(--text-faint);
  }
  .dots {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 28px;
    height: 28px;
    margin: -4px -4px 0 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
  }
  .dots:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  /* The surfaced delete: a ghost until touched, then an armed danger pill
     whose label IS the confirmation — the two-tap safety stays intact. */
  .del {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 28px;
    height: 28px;
    margin: -4px 0 0 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-faint);
    transition: background 0.12s ease, color 0.12s ease;
  }
  .del:hover {
    background: var(--danger-soft);
    color: var(--danger-text);
  }
  .del.armed {
    width: auto;
    padding: 0 10px;
    border-radius: 999px;
    background: var(--danger-soft);
    border: 1px solid var(--danger);
    color: var(--danger-text);
  }
  .del-txt {
    font-size: var(--fs-tiny);
    font-weight: 700;
    white-space: nowrap;
  }
  .title {
    font-size: var(--fs-ui);
    overflow: hidden;
    text-overflow: ellipsis;
    /* Two lines, then cut — one long title must not eat the card. */
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-compact);
    flex-wrap: wrap;
    min-width: 0;
    font-variant-numeric: tabular-nums;
  }
  .meta :global(svg) {
    flex-shrink: 0;
  }
  /* Same guard for the header's icon buttons and source tags: a flex-item svg
     with no explicit shrink lock can be crushed to 0 width (this is what made
     the ⋯ menu button render as an invisible 28px square). */
  .dots :global(svg),
  .del :global(svg),
  .srctag :global(svg) {
    flex-shrink: 0;
  }
  .dotsep {
    color: var(--text-faint);
  }
  .loc {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 24ch;
  }
  /* A channel location is a door: accent ink, inline with the meta line —
     louder than the free-text caption, quieter than the Join pill. */
  .locbtn {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 0 3px;
    background: transparent;
    border-radius: var(--radius-sm);
    color: var(--accent-hover);
    font-size: var(--fs-compact);
    font-weight: 600;
    min-width: 0;
  }
  .locbtn:hover {
    text-decoration: underline;
  }
  .locname {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 20ch;
  }
  .details {
    margin: 0;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    line-height: 1.45;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    /* Same discipline as the title: a paragraph, not an essay. */
    display: -webkit-box;
    -webkit-line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .ctarow {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-wrap: wrap;
    margin-top: 2px;
  }
  .rsvps {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  /* History matters: an ended card keeps its counts, loses its buttons. */
  .rsvps.readonly {
    pointer-events: none;
  }
  .rsvps.readonly .chip {
    background: transparent;
    border-color: var(--hairline);
    color: var(--text-faint);
  }
  .spring {
    flex: 1;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 11px;
    border-radius: 999px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    color: var(--text-muted);
    font-size: var(--fs-compact);
    font-weight: 600;
    transition: background 0.12s ease, color 0.12s ease, border-color 0.12s ease;
  }
  .chip:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .cnt {
    font-size: var(--fs-tiny);
    padding: 0 5px;
    border-radius: 999px;
    background: var(--bg-3);
    font-variant-numeric: tabular-nums;
  }
  .chip.going.on {
    background: var(--ok-soft);
    border-color: var(--ok);
    color: var(--ok-text);
  }
  .chip.maybe.on {
    background: var(--accent-soft);
    border-color: var(--accent);
    color: var(--accent-hover);
  }
  .chip.no.on {
    background: var(--danger-soft);
    border-color: var(--danger);
    color: var(--danger-text);
  }
  .chip.on .cnt {
    background: transparent;
  }
  /* Join is THE action — filled pill, promoted early ("soon") and loud when
     live. Accent before the hour, --ok during it. */
  .gjoin {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 16px;
    border-radius: 999px;
    background: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fs-compact);
    font-weight: 700;
    transition: background 0.12s ease;
  }
  .gjoin:hover {
    background: var(--accent-hover);
  }
  .gjoin:disabled {
    opacity: 0.7;
  }
  .evcard.live .gjoin {
    background: var(--ok);
    color: var(--ok-fg, #fff);
  }
  .spin {
    width: 12px;
    height: 12px;
    border: 2px solid currentColor;
    border-top-color: transparent;
    border-radius: 50%;
    animation: gjoin-spin 0.7s linear infinite;
  }
  @keyframes gjoin-spin {
    to {
      transform: rotate(360deg);
    }
  }
  .gtag {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 9px;
    border-radius: 999px;
    background: var(--ok-soft);
    color: var(--ok-text);
    font-size: var(--fs-tiny);
    font-weight: 700;
  }
  .gtag.over {
    background: transparent;
    border: 1px solid var(--hairline);
    color: var(--text-faint);
  }
  .footrow {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-wrap: wrap;
    min-height: 24px;
  }
  .faces {
    display: inline-flex;
    align-items: center;
    background: transparent;
    padding: 1px 2px;
    border-radius: 999px;
    min-width: 0;
  }
  .faces:hover .counts {
    color: var(--text);
  }
  .face {
    display: inline-flex;
    border-radius: 50%;
    border: 2px solid var(--bg-1);
    flex-shrink: 0;
  }
  .face + .face {
    margin-left: -6px;
  }
  .face.maybe {
    opacity: 0.55;
  }
  .face.more {
    width: 22px;
    height: 22px;
    align-items: center;
    justify-content: center;
    background: var(--bg-3);
    color: var(--text-muted);
    font-size: var(--fs-micro);
    font-weight: 700;
  }
  .counts {
    margin-left: 7px;
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* The guest link is a whisper, not a pill — outsiders' door, kept quiet. */
  .gcopy {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 4px;
    background: transparent;
    border-radius: var(--radius-sm);
    color: var(--text-faint);
    font-size: var(--fs-tiny);
    font-weight: 600;
  }
  .gcopy:hover {
    color: var(--text-muted);
    text-decoration: underline;
  }
  .gcopy:active {
    text-decoration: underline;
  }
  @media (prefers-reduced-motion: reduce) {
    .now-dot,
    .evcard.live,
    .spin {
      animation: none;
    }
  }
  /* Phones: every primary tappable reaches 44px; Join stretches to a full
     row so the thumb cannot miss the door. */
  @media (pointer: coarse), (max-width: 768px) {
    .chip {
      min-height: 44px;
      padding: 4px 14px;
    }
    .dots {
      width: 40px;
      height: 40px;
      margin-top: -8px;
    }
    .del {
      width: 40px;
      height: 40px;
      margin-top: -8px;
    }
    .del.armed {
      width: auto;
    }
    .srctag {
      min-height: 24px; /* inline kicker chip; the row's 40px buttons carry the tap floor */
    }
    .gjoin {
      min-height: 44px;
      padding: 4px 20px;
      flex: 1 1 auto;
      justify-content: center;
    }
    .gcopy,
    .faces {
      min-height: 36px;
    }
    .locbtn {
      min-height: 32px;
      padding-inline: var(--sp-2);
    }
  }
</style>
