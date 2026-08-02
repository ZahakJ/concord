<script>
  // One calendar event, everywhere an event appears: the per-guild panel and
  // the aggregated "Your calendar". Date bubble, time/place, RSVP chips with
  // counts and names, and the per-event actions (remind me, add to my
  // calendar, edit/delete for those allowed).
  import Icon from "./Icon.svelte";
  import { S, nameFor, flash, openContextMenu, selectGuild, refreshGuilds } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PERM, has } from "./lib/perms.js";
  import { addReminder } from "./lib/scheduled.svelte.js";
  import {
    fmtEventTime,
    happeningNow,
    isPast,
    rsvpBuckets,
    guildTint,
    guildInitials,
    downloadICS,
    icsName,
    eventReminderTimes,
    loadEvents,
  } from "./lib/events.svelte.js";

  let { ev, g, showGuild = false, onEdit } = $props();

  // A slow tick so "Now" appears/disappears while the panel is open, instead
  // of only on reopen. 30s is plenty for a minute-grained state.
  let now = $state(Date.now() / 1000);
  $effect(() => {
    const t = setInterval(() => (now = Date.now() / 1000), 30000);
    return () => clearInterval(t);
  });

  const live = $derived(happeningNow(ev, now));
  const past = $derived(isPast(ev, now));
  const mine = $derived(ev.rsvps?.[S.identity.fingerprint] || "");
  const buckets = $derived(rsvpBuckets(ev));
  // Mirrors the backend's mayCurateEvent gate (author or ManageMessages), so
  // the buttons only appear where the action would succeed.
  const canEdit = $derived(
    !!g && (ev.createdBy === S.identity.fingerprint || g.isOwner || has(g.myPerms || 0, PERM.MANAGE_MESSAGES)),
  );
  const mon = $derived(new Date(ev.startUnix * 1000).toLocaleDateString([], { month: "short" }));
  const dayN = $derived(new Date(ev.startUnix * 1000).getDate());

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

  // Names line under the chips — counts alone don't answer "who's coming".
  function names(list) {
    const shown = list.slice(0, 6).map((f) => nameFor(f));
    return shown.join(", ") + (list.length > 6 ? ` +${list.length - 6}` : "");
  }

  function remind(e) {
    const times = eventReminderTimes(ev.startUnix);
    if (!times.length) {
      flash("It's already started — go!", "info");
      return;
    }
    // Jump target for the fired reminder: the guild's first ordinary text
    // channel (reminders navigate by channel; an event has no channel of its own).
    const chId =
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
  // on receive, mirrored here so the button only appears where it works.
  const isGuestHost = $derived(!!ev.guestUrl && ev.guestHost === S.identity.fingerprint);
  // Guests are for real guilds — a meeting is already a guest room, and events
  // only surface in guilds anyway. Gate like the backend does.
  const canInviteGuests = $derived(canEdit && !ev.guestUrl && !past && g?.kind !== "meeting");

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

  // ---- member Join ----
  // The Teams split, exactly: people already IN this guild/DM tap Join and
  // walk straight in as themselves — the memberCode on the event is a real
  // invite into the meeting room (full identity, E2EE, never the guest
  // knock). Only outsiders ride the copied browser link and wait at the door.
  // The backend short-circuits for the host and for a member rejoining, so
  // one handler covers all three shapes of "get me in".
  let joining = $state(false);
  async function joinRoom() {
    if (joining) return;
    joining = true;
    try {
      const room = await api.joinEventRoom(ev.guildId, ev.id);
      // A fresh join means a guild S.guilds hasn't heard of yet — refresh
      // before navigating or selectGuild lands on nothing.
      if (!S.guilds.some((x) => x.id === room.id)) await refreshGuilds();
      await selectGuild(room.id);
      S.modal = null; // the room is the destination; the calendar's job is done
    } catch (err) {
      flash(err); // e.g. "this event's room has ended" — honest, not silent
      loadEvents(ev.guildId); // a dead room usually means a stale card too
    } finally {
      joining = false;
    }
  }

  // Two-tap revoke, same in-place arming as delete below.
  let revokeArmed = $state(false);
  let revokeT;
  async function revokeGuests() {
    if (!revokeArmed) {
      revokeArmed = true;
      revokeT = setTimeout(() => (revokeArmed = false), 2600);
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

  // Two-tap delete: the trash arms, then confirms in place. A ConfirmDialog
  // would replace S.modal — i.e. close the calendar the user is standing in.
  let armed = $state(false);
  let armT;
  async function del() {
    if (!armed) {
      armed = true;
      armT = setTimeout(() => (armed = false), 2600);
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

<article class="evcard" class:live class:past>
  <div class="date" aria-hidden="true">
    <span class="mon">{mon}</span>
    <span class="dayn">{dayN}</span>
  </div>
  <div class="body">
    <header class="top">
      <strong class="title">{ev.title}</strong>
      {#if live}
        <span class="now"><span class="now-dot"></span>Now</span>
      {/if}
    </header>
    <div class="meta muted">
      {#if showGuild && g}
        <span class="gbadge" style={guildTint(g.id)}>{guildInitials(g.name)}</span>
        <span class="gname">{g.name}</span>
        <span class="dotsep">·</span>
      {/if}
      <Icon name="clock" size={11} />
      <span>{fmtEventTime(ev)}</span>
      {#if ev.location}
        <span class="dotsep">·</span>
        <span class="loc">{ev.location}</span>
      {/if}
    </div>
    {#if ev.details}
      <p class="details">{ev.details}</p>
    {/if}
    <div class="rsvps" role="group" aria-label="RSVP">
      <button class="chip going" class:on={mine === "going"} onclick={() => rsvp("going")}>
        <Icon name="check" size={11} /> Going{#if buckets.going.length}<span class="cnt">{buckets.going.length}</span>{/if}
      </button>
      <button class="chip maybe" class:on={mine === "maybe"} onclick={() => rsvp("maybe")}>
        Maybe{#if buckets.maybe.length}<span class="cnt">{buckets.maybe.length}</span>{/if}
      </button>
      <button class="chip no" class:on={mine === "no"} onclick={() => rsvp("no")}>
        <Icon name="close" size={10} /> No{#if buckets.no.length}<span class="cnt">{buckets.no.length}</span>{/if}
      </button>
      <span class="spring"></span>
      <!-- One flex unit, so a narrow card wraps the whole tool cluster to the
           next line instead of orphaning the last icon on a row of its own. -->
      <span class="actrow">
        {#if canInviteGuests}
          <button
            class="act"
            title="Open meeting room (members join in one tap; guests get a link)"
            aria-label="Open meeting room"
            onclick={inviteGuests}
          >
            <Icon name="camera" size={14} />
          </button>
        {/if}
        {#if !past}
          <button class="act" title="Remind me" aria-label="Remind me about this event" onclick={remind}>
            <Icon name="bell" size={14} />
          </button>
        {/if}
        <button class="act" title="Add to my calendar (.ics)" aria-label="Add to my calendar" onclick={addToCal}>
          <Icon name="download" size={14} />
        </button>
        {#if canEdit && onEdit}
          <button class="act" title="Edit event" aria-label="Edit event" onclick={() => onEdit(ev)}>
            <Icon name="edit" size={14} />
          </button>
        {/if}
        {#if canEdit}
          <button
            class="act danger"
            class:armed
            title={armed ? "Tap again to delete for everyone" : "Delete event"}
            aria-label={armed ? "Tap again to delete for everyone" : "Delete event"}
            onclick={del}
          >
            <Icon name="trash" size={14} />
            {#if armed}<span class="arm-label">Sure?</span>{/if}
          </button>
        {/if}
      </span>
    </div>
    {#if ev.guestUrl}
      <!-- The room row. Join is the primary way in for everyone invited
           (members walk in as themselves); the copyable guest link is the
           secondary door for outsiders. Only the host revokes. -->
      <div class="guests" class:dim={past}>
        {#if ev.memberCode && !past}
          <button class="gjoin" disabled={joining} aria-label="Join the meeting room" onclick={joinRoom}>
            {#if joining}<span class="spin" aria-hidden="true"></span> Joining…{:else}<Icon name="camera" size={13} /> Join{/if}
          </button>
        {:else}
          <span class="gtag"><Icon name="camera" size={11} /> Room open</span>
        {/if}
        <button
          class="gcopy"
          title="For people outside this {g?.kind === 'dm' ? 'chat' : 'guild'} — they join from a browser and knock"
          aria-label="Copy the guest link"
          onclick={copyGuestLink}
        >
          <Icon name="copy" size={12} /> Guest link
        </button>
        {#if isGuestHost && canEdit}
          <button
            class="grevoke"
            class:armed={revokeArmed}
            title={revokeArmed ? "Tap again — the link dies and the room is deleted" : "Revoke the guest link"}
            aria-label={revokeArmed ? "Tap again to revoke for everyone" : "Revoke the guest link"}
            onclick={revokeGuests}
          >
            <Icon name="close" size={10} /> {revokeArmed ? "Sure?" : "Revoke"}
          </button>
        {/if}
      </div>
    {/if}
    {#if buckets.going.length || buckets.maybe.length || buckets.no.length}
      <div class="who muted">
        {#if buckets.going.length}<span><Icon name="check" size={10} /> {names(buckets.going)}</span>{/if}
        {#if buckets.maybe.length}<span>? {names(buckets.maybe)}</span>{/if}
        {#if buckets.no.length}<span><Icon name="close" size={9} /> {names(buckets.no)}</span>{/if}
      </div>
    {/if}
  </div>
</article>

<style>
  .evcard {
    display: flex;
    gap: 12px;
    padding: 12px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    text-align: left;
  }
  /* Happening right now: the card glows softly — noticeable in a list scan,
     quiet enough to live next to ordinary cards. */
  .evcard.live {
    border-color: color-mix(in srgb, var(--ok) 55%, transparent);
    animation: ev-breathe 3.2s ease-in-out infinite;
  }
  @keyframes ev-breathe {
    0%,
    100% {
      box-shadow: 0 0 0 0 transparent;
    }
    50% {
      box-shadow: 0 0 14px color-mix(in srgb, var(--ok) 22%, transparent);
    }
  }
  .evcard.past {
    opacity: 0.62;
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
  .evcard.live .date {
    background: var(--ok-soft);
    color: var(--ok-text);
  }
  .mon {
    font-size: var(--fs-tiny);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    line-height: 1.1;
  }
  .dayn {
    font-size: 19px;
    font-weight: 700;
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
    align-items: center;
    gap: 8px;
    min-width: 0;
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
  .now {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex-shrink: 0;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--ok-soft);
    color: var(--ok-text);
    font-size: var(--fs-tiny);
    font-weight: 700;
  }
  .now-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--ok);
    animation: ev-now-pulse 1.4s ease-in-out infinite;
  }
  @keyframes ev-now-pulse {
    50% {
      opacity: 0.3;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .now-dot,
    .evcard.live {
      animation: none;
    }
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-compact);
    flex-wrap: wrap;
    min-width: 0;
  }
  .meta :global(svg) {
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
  .gbadge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: 6px;
    font-size: 8px;
    font-weight: 700;
    flex-shrink: 0;
  }
  .gname {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 16ch;
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
  .rsvps {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    margin-top: 2px;
  }
  .spring {
    flex: 1;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 10px;
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
  .actrow {
    display: inline-flex;
    align-items: center;
    gap: 2px;
  }
  .act {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 5px;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
  }
  .act:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .act.danger:hover {
    background: var(--danger-soft);
    color: var(--danger-text);
  }
  .act.armed {
    background: var(--danger-soft);
    color: var(--danger-text);
    font-size: var(--fs-tiny);
    font-weight: 700;
  }
  .guests {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .guests.dim {
    opacity: 0.6;
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
  /* Join is THE action on a card with a live room — a filled button, not a
     chip, so it reads before anything else on the row. */
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
  @media (prefers-reduced-motion: reduce) {
    .spin {
      animation: none;
    }
  }
  .gcopy,
  .grevoke {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 10px;
    border-radius: 999px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    color: var(--text-muted);
    font-size: var(--fs-tiny);
    font-weight: 600;
  }
  .gcopy:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .grevoke:hover,
  .grevoke.armed {
    background: var(--danger-soft);
    border-color: var(--danger);
    color: var(--danger-text);
  }
  .who {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    font-size: var(--fs-tiny);
  }
  .who span {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    min-width: 0;
  }
  /* Phones: RSVP is the thing people do most — real targets. */
  @media (pointer: coarse), (max-width: 768px) {
    .chip {
      min-height: 36px;
      padding: 4px 12px;
    }
    .act {
      min-width: 40px;
      min-height: 40px;
      justify-content: center;
    }
    .gcopy,
    .grevoke {
      min-height: 36px;
      padding: 4px 12px;
    }
    .gjoin {
      min-height: var(--tap-min, 44px);
      padding: 4px 20px;
    }
  }
</style>
