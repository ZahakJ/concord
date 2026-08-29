<script>
  // Bookings: publish office hours as one public URL. The page itself lives on
  // the rendezvous, but every fact on it — availability, name, blurb, free
  // slots — is served live from THIS node over the same relay browser guests
  // use, and every booking lands here: a knock-to-enter meeting room plus an
  // event in your Notes calendar. Nothing about your calendar is stored on the
  // server, which is also why the page honestly reads "unreachable" when this
  // app is closed.
  import SettingsShell from "./SettingsShell.svelte";
  import Icon from "../Icon.svelte";
  import Select from "../Select.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import { onMount } from "svelte";
  import { api } from "../lib/api.js";
  import { flash } from "../lib/state.svelte.js";

  let { onClose } = $props();

  let loaded = $state(false);
  let enabled = $state(false);
  let blurb = $state("");
  let slotMinutes = $state(30);
  let horizonDays = $state(14);
  let windows = $state([]);
  let url = $state("");
  let bookings = $state([]);
  // What the disk currently holds, for the dirty check: availability edits
  // are batched behind one Save, so half-typed hours never go live.
  let savedShape = $state("");

  const shape = () => JSON.stringify({ blurb, slotMinutes, horizonDays, windows });
  const dirty = $derived(loaded && shape() !== savedShape);

  function adopt(view) {
    enabled = !!view.enabled;
    blurb = view.blurb || "";
    slotMinutes = view.slotMinutes || 30;
    horizonDays = view.horizonDays || 14;
    windows = (view.windows || []).map((w) => ({ ...w }));
    url = view.url || "";
    bookings = view.bookings || [];
    savedShape = shape();
    loaded = true;
  }

  onMount(async () => {
    try {
      adopt(await api.bookingSettings());
    } catch (err) {
      flash(err);
    }
  });

  async function save(nextEnabled) {
    try {
      adopt(
        await api.setBookingConfig({
          enabled: nextEnabled,
          blurb,
          slotMinutes: Number(slotMinutes),
          horizonDays: Number(horizonDays),
          windows: windows.map((w) => ({
            weekday: Number(w.weekday),
            startMin: w.startMin,
            endMin: w.endMin,
          })),
        }),
      );
      return true;
    } catch (err) {
      flash(err);
      return false;
    }
  }

  async function toggleEnabled() {
    const was = enabled;
    if (!(await save(!was))) enabled = was; // backend refused (e.g. no windows yet)
  }
  async function saveAvailability() {
    if (await save(enabled)) flash("Availability saved", "success");
  }

  const dayNames = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
  // Editing order Mon-first (how people think about office hours), values
  // stay 0=Sunday (what the backend and time.Weekday agree on).
  const dayOrder = [1, 2, 3, 4, 5, 6, 0];
  const toClock = (min) =>
    `${String(Math.floor(min / 60)).padStart(2, "0")}:${String(min % 60).padStart(2, "0")}`;
  const fromClock = (v) => {
    const [h, m] = String(v).split(":").map(Number);
    return (Number.isFinite(h) ? h : 0) * 60 + (Number.isFinite(m) ? m : 0);
  };

  function addWindow() {
    windows = [...windows, { weekday: 1, startMin: 9 * 60, endMin: 17 * 60 }];
  }
  function removeWindow(i) {
    windows = windows.filter((_, j) => j !== i);
  }

  let copied = $state(false);
  function copyLink() {
    navigator.clipboard?.writeText(url);
    copied = true;
    setTimeout(() => (copied = false), 1600);
  }

  // Cancelling frees the slot for new visitors AND kills the visitor's
  // meeting link, so it gets a confirm instead of a bare click. Rendered
  // locally, not via S.modal — replacing this panel would drop the trail.
  let confirmCancel = $state(null);
  async function cancelBooking(b) {
    confirmCancel = null;
    try {
      await api.cancelBooking(b.eventId);
      adopt(await api.bookingSettings());
      flash("Booking cancelled — the slot is open again", "success");
    } catch (err) {
      flash(err);
    }
  }

  const whenFmt = new Intl.DateTimeFormat(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
</script>

<SettingsShell title="Bookings" here="bookings" {onClose}>
  <SettingGroup
    label="Public booking page"
    info="The link points at your rendezvous server, but the server stores nothing: every visit asks your running Concord app for free slots, so the page only works while this app is open — and says so honestly when it isn't. Each booking creates a knock-to-enter meeting room and an event in your Notes calendar. The visitor keeps their meeting link and an .ics file; no email is involved, on purpose."
  >
    <SettingRow
      icon="clock"
      title="Booking page"
      sub={enabled ? "Live — anyone with the link can book a slot" : "Off — the link answers nothing"}
      checked={enabled}
      onclick={toggleEnabled}
    />
    {#if enabled && url}
      <div class="linkrow">
        <code class="addr" title={url}>{url}</code>
        <button class="addr-act" onclick={copyLink} aria-label="Copy booking link">
          {#if copied}<Icon name="check" size={15} />{:else}<Icon name="copy" size={15} />{/if}
        </button>
      </div>
    {/if}
    <div class="fieldrow">
      <label class="flab" for="bk-blurb">Blurb shown under your name</label>
      <input
        id="bk-blurb"
        class="fin"
        maxlength="280"
        placeholder="e.g. 30 minutes, straight to a demo — bring questions"
        bind:value={blurb}
      />
    </div>
  </SettingGroup>

  <SettingGroup
    label="Office hours"
    note="Hours are this computer's local time; visitors see them converted to theirs."
  >
    {#each windows as w, i (i)}
      <div class="winrow">
        <span class="fsel day">
          <Select
            label="Weekday"
            value={w.weekday}
            onPick={(v) => (w.weekday = v)}
            options={dayOrder.map((d) => ({ value: d, label: dayNames[d].slice(0, 3) }))}
          />
        </span>
        <input
          class="fin time"
          type="time"
          aria-label="Start"
          value={toClock(w.startMin)}
          onchange={(e) => (w.startMin = fromClock(e.target.value))}
        />
        <span class="dash">–</span>
        <input
          class="fin time"
          type="time"
          aria-label="End"
          value={toClock(w.endMin)}
          onchange={(e) => (w.endMin = fromClock(e.target.value))}
        />
        <button class="addr-act" onclick={() => removeWindow(i)} aria-label="Remove window">
          <Icon name="trash" size={15} />
        </button>
      </div>
    {/each}
    <div class="winrow">
      <button class="ghostbtn" onclick={addWindow}><Icon name="plus" size={14} /> Add hours</button>
    </div>
    <div class="winrow opts">
      <span class="flab">Slot length</span>
      <span class="fsel">
        <Select
          label="Slot length"
          value={slotMinutes}
          onPick={(v) => (slotMinutes = v)}
          options={[15, 20, 30, 45, 60, 90].map((m) => ({ value: m, label: `${m} min` }))}
        />
      </span>
      <span class="flab">Bookable</span>
      <span class="fsel">
        <Select
          label="Bookable"
          value={horizonDays}
          onPick={(v) => (horizonDays = v)}
          options={[
            { value: 7, label: "1 week ahead" },
            { value: 14, label: "2 weeks ahead" },
            { value: 21, label: "3 weeks ahead" },
            { value: 30, label: "30 days ahead" },
          ]}
        />
      </span>
    </div>
    {#if dirty}
      <div class="winrow">
        <button class="savebtn" onclick={saveAvailability}>Save availability</button>
      </div>
    {/if}
  </SettingGroup>

  <SettingGroup
    label="Upcoming bookings"
    info="Each booking is also an event in your Notes calendar and a ⚡ meeting room in your sidebar. Cancelling frees the slot and expires the visitor's link — they'll see the meeting is no longer valid."
  >
    {#if bookings.length === 0}
      <p class="empty">Nothing booked yet.</p>
    {:else}
      {#each bookings as b (b.eventId)}
        <div class="bkrow">
          <span class="bktext">
            <span class="bkwhen">{whenFmt.format(new Date(b.slotUnix * 1000))}</span>
            <span class="bkwho">{b.name}{b.note ? " — " + b.note : ""}</span>
          </span>
          <button class="addr-act" onclick={() => (confirmCancel = b)} aria-label="Cancel booking">
            <Icon name="close" size={15} />
          </button>
        </div>
      {/each}
    {/if}
  </SettingGroup>
</SettingsShell>

{#if confirmCancel}
  <ConfirmDialog
    title="Cancel this booking?"
    body={`${confirmCancel.name}'s slot (${whenFmt.format(new Date(confirmCancel.slotUnix * 1000))}) opens up again and their meeting link stops working.`}
    confirmLabel="Cancel booking"
    onConfirm={() => cancelBooking(confirmCancel)}
    onClose={() => (confirmCancel = null)}
  />
{/if}

<style>
  .linkrow,
  .winrow,
  .bkrow,
  .fieldrow {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 10px 14px;
  }
  .linkrow,
  .winrow,
  .bkrow {
    border-top: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  .fieldrow {
    flex-direction: column;
    align-items: stretch;
    gap: 5px;
    border-top: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  .flab {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .fin {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 8px 10px;
    color: var(--text);
    font-size: var(--fs-compact);
    min-height: var(--tap-min, 34px);
  }
  .fin:focus {
    outline: none;
    border-color: color-mix(in srgb, var(--accent) 60%, transparent);
  }
  .addr {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-small);
    color: var(--text-muted);
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 8px 10px;
  }
  .addr-act {
    flex: none;
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
  }
  .addr-act:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .dash {
    color: var(--text-faint);
  }
  /* A Select brings its own well; this wrapper is only here to size it. */
  .fsel {
    display: block;
    min-width: 0;
  }
  /* An office-hours row is a weekday, two clocks and a bin, and at the panel's
     real width it does not fit on one line. Flex resolved that by crushing the
     ONLY item that would shrink — the weekday select, whose 84px collapsed to
     28 — while its "Mon ⌄" contents kept their size and painted straight over
     the start time, which read as ")0 PM". So the row is allowed to wrap
     instead: the weekday holds its width, the two clocks share what is left
     down to a floor that still shows "03:00 PM", and the bin drops to a second
     line rather than eating a control. */
  .day {
    width: 84px;
    flex: none;
  }
  .winrow {
    flex-wrap: wrap;
  }
  .time {
    flex: 1 1 118px;
    min-width: 118px;
  }
  /* When the bin is what wraps, it belongs at the end of the row it fell off,
     not adrift at the left margin where it reads as a stray button. */
  .winrow .addr-act {
    margin-left: auto;
  }
  .ghostbtn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: 1px dashed var(--border);
    border-radius: var(--radius-md);
    padding: 8px 14px;
    color: var(--text-muted);
    font-size: var(--fs-compact);
  }
  .ghostbtn:hover {
    color: var(--text);
    background: var(--bg-3);
  }
  .savebtn {
    width: 100%;
    background: var(--accent);
    color: var(--accent-fg);
    border: none;
    border-radius: var(--radius-md);
    padding: 10px;
    font-weight: 600;
    font-size: var(--fs-compact);
  }
  .savebtn:hover {
    background: var(--accent-hover);
  }
  .opts {
    flex-wrap: wrap;
  }
  .opts .flab {
    margin-left: 2px;
  }
  .empty {
    margin: 0;
    padding: 14px;
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .bktext {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .bkwhen {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .bkwho {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* Phone: time controls get room; rows may wrap rather than shrink taps. */
  @media (pointer: coarse), (max-width: 768px) {
    .winrow {
      flex-wrap: wrap;
    }
    .addr-act {
      width: 40px;
      height: 40px;
    }
  }
</style>
