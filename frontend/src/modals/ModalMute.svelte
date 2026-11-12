<script>
  // Mute was exactly ten minutes, forever, with the 10 hard-coded at the call
  // site — no picker, no custom value, and nothing anywhere recording why.
  // This is the picker, and the reason field the moderation log now carries.
  import Modal from "./Modal.svelte";
  import Select from "../Select.svelte";
  import { S } from "../lib/state.svelte.js";
  import { MUTE_DURATIONS, muteMember } from "../lib/moderation.svelte.js";

  let { onClose } = $props();

  // Snapshotted at setup: the dialog is driven off S.modal and clearing that
  // before the action runs would leave the component reading null.
  const mem = S.modal?.member;

  let minutes = $state(10);
  let custom = $state(false);
  let customValue = $state(30);
  let customUnit = $state(60); // minutes per unit
  let reason = $state("");

  const chosen = $derived(custom ? Math.max(1, Math.round(customValue * customUnit)) : minutes);
  // A week is the ceiling the service clamps to as well, so the dialog cannot
  // promise something the op would quietly shorten.
  const tooLong = $derived(chosen > 10080);

  async function apply() {
    if (!mem || tooLong) return;
    onClose();
    await muteMember(mem, chosen, reason.trim());
  }
</script>

<Modal title={`Mute ${mem?.name || "member"}`} {onClose}>
  <p class="muted">
    While muted, honest clients drop their messages in this guild. Like slow mode, it is a rule
    every member's app keeps — there is no server to enforce it.
  </p>

  <div class="fld">
    <span class="lbl">For how long</span>
    <Select
      label="Mute duration"
      value={custom ? "custom" : minutes}
      onPick={(v) => (v === "custom" ? (custom = true) : ((custom = false), (minutes = Number(v))))}
      options={[
        ...MUTE_DURATIONS.map((d) => ({ value: d.minutes, label: d.label })),
        { value: "custom", label: "Custom…" },
      ]}
    />
  </div>

  {#if custom}
    <div class="fld custom">
      <label class="c-num">
        <span class="lbl">Amount</span>
        <input type="number" min="1" max="10080" bind:value={customValue} />
      </label>
      <div class="c-num">
        <span class="lbl">Unit</span>
        <Select
          label="Unit"
          value={customUnit}
          onPick={(v) => (customUnit = v)}
          options={[
            { value: 1, label: "minutes" },
            { value: 60, label: "hours" },
            { value: 1440, label: "days" },
          ]}
        />
      </div>
    </div>
    {#if tooLong}
      <p class="warn-line" role="status">A mute tops out at one week — past that, kick instead.</p>
    {/if}
  {/if}

  <label class="fld">
    <span class="lbl">Reason <span class="opt">optional</span></span>
    <input bind:value={reason} maxlength="160" placeholder="For the moderation log" />
  </label>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button class="primary" onclick={apply} disabled={tooLong}>Mute</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
    color: var(--text-muted);
    font-size: var(--fs-compact);
    line-height: 1.5;
  }
  .warn-line {
    color: var(--warn-text);
  }
  .fld {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .lbl {
    font-size: var(--fs-compact);
    font-weight: 500;
    color: var(--text);
  }
  .opt {
    font-weight: 500;
    color: var(--text-faint);
  }
  .custom {
    flex-direction: row;
    gap: var(--sp-2);
  }
  .c-num {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
</style>
