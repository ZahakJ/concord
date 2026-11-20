<script>
  import Caution from "./Caution.svelte";
  // "Link a device": the issuer side. Shows a QR (and the raw code as a
  // fallback) that a new device scans/pastes to join this account. The code
  // carries a single-use secret + how to reach us; it expires in 2 minutes.
  // Knowing the code is not enough to get the account: when a device asks,
  // this dialog shows its name and six digits, the new device shows the same
  // six, and nothing is sent until the owner says yes here.
  import { onMount, onDestroy } from "svelte";
  import { api } from "../lib/api.js";
  import { linkURLFor } from "../lib/deeplink.js";
  import { flash } from "../lib/state.svelte.js";
  import { humanError } from "../lib/errors.js";
  import { newlyLinked } from "../lib/linkwatch.js";
  import { unlinkReport } from "../lib/unlinkresult.js";
  import RailShell from "./RailShell.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";

  let { onClose } = $props();

  let code = $state("");
  let qr = $state("");
  let error = $state("");
  let copied = $state(false);

  // Your devices. They used to live only under Connection → Stats &
  // diagnostics, three levels down — the one place someone who lost a phone
  // would never look for Unlink. They belong with linking.
  let devices = $state(null);
  let known = null; // the list as it was when this dialog opened
  let linked = $state(null); // the device that just linked with this code
  let watch;
  async function refresh() {
    try {
      const now = await api.linkedDevices();
      if (known === null) known = now;
      else if (!linked) {
        linked = newlyLinked(known, now);
        if (linked) flash(`${linked.name || "Your new device"} is linked`, "success");
      }
      devices = now;
    } catch {
      /* locked or older core: the QR still works */
    }
  }
  refresh();
  watch = setInterval(refresh, 2000);

  // The confirm step. status is the core's LinkStatus, polled while the code
  // is up: a device waiting for a yes, or the code used, expired, or tried
  // by a device too old to link. answered is what the owner said here.
  let status = $state(null);
  let answered = $state(null); // null | {yes, name, at}
  async function pollStatus() {
    if (linked) return;
    try {
      status = await api.linkStatus();
    } catch {
      /* locked or older core */
    }
  }
  pollStatus();
  const statusWatch = setInterval(pollStatus, 500);

  async function answer(yes) {
    const p = status?.prompt;
    if (!p) return;
    answered = { yes, name: p.deviceName, at: Date.now() };
    try {
      await api.confirmLink(p.id, yes);
    } catch (e) {
      answered = null;
      flash(e);
    }
    pollStatus();
  }

  // Once yes was said, the device shows as linked within a few seconds. If it
  // never does (it went away before the account reached it), the code is
  // still spent, and the dialog offers a new one. Re-evaluated on every poll.
  const sending = $derived(!!answered?.yes && !linked && !(status?.used && Date.now() - answered.at > 20000));

  async function showCode() {
    code = await api.linkOffer();
    // Fetched here rather than imported at the top: the encoder is only ever
    // needed by this dialog, and it was riding into every cold start for the
    // sake of a screen most sessions never open. The offer round-trip above
    // covers the load.
    const { default: QRCode } = await import("qrcode");
    // The QR carries the concord:// deep-link form, so a plain OS-camera
    // scan opens the Concord app with the code already filled in. The
    // in-app scanner and the paste box accept both forms.
    qr = await QRCode.toDataURL(linkURLFor(code), { margin: 1, width: 320, errorCorrectionLevel: "L" });
  }

  async function newCode() {
    answered = null;
    status = null;
    qr = "";
    error = "";
    known = devices;
    try {
      await showCode();
    } catch (e) {
      error = humanError(e);
    }
    pollStatus();
  }

  function fmtAgo(sec) {
    if (!sec) return "never";
    const s = Math.max(0, Math.floor(Date.now() / 1000) - sec);
    if (s < 45) return "just now";
    if (s < 3600) return `${Math.round(s / 60)}m ago`;
    if (s < 86400) return `${Math.round(s / 3600)}h ago`;
    return `${Math.round(s / 86400)}d ago`;
  }

  let confirming = $state(null);
  // Where the last unlink left the device listed (see unlinkresult.js).
  let report = $state(null);
  async function unlink(dev) {
    confirming = null;
    try {
      const r = unlinkReport(dev.name || "That device", await api.unlinkDevice(dev.key));
      flash(r.success, "success");
      if (r.error) flash(r.error, "error");
      report = r.pending.length ? r : null;
    } catch (err) {
      flash(err);
    }
    refresh();
  }

  onMount(async () => {
    try {
      await showCode();
    } catch (e) {
      error = humanError(e);
    }
  });

  onDestroy(() => {
    clearInterval(watch);
    clearInterval(statusWatch);
    // Burn the offer server-side when the dialog closes so it can't be redeemed
    // after the user walks away. A device still waiting for a yes gets a no.
    api.cancelLinkOffer().catch(() => {});
  });

  function copyCode() {
    navigator.clipboard?.writeText(code);
    copied = true;
    flash("Link code copied", "success");
    setTimeout(() => (copied = false), 1600);
  }
</script>

<RailShell title="Your devices" here="linkDevice" {onClose}>
  {#if devices?.length}
    <ul class="devs">
      {#each devices as d (d.key)}
        <li class="dev" class:off={!d.online}>
          <span class="dot" class:on={d.online}></span>
          <span class="dname">{d.name || "Linked device"}</span>
          <span class="dmeta muted">
            {#if d.thisOne}this one{:else if d.revokedAt}unlinked{:else if d.online}online{:else}last seen {fmtAgo(d.lastSeen)}{/if}
          </span>
          {#if !d.thisOne && !d.revokedAt}
            <button class="unlink" onclick={() => (confirming = d)}>Unlink</button>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  {#if report}
    <div class="pending" role="status">
      <p>{report.pendingTitle}</p>
      <ul>
        {#each report.pending as p (p.key)}
          <li>{p.where}. <span class="muted">{p.who}.</span></li>
        {/each}
      </ul>
      <p class="muted">Until then it can still receive new messages there.</p>
    </div>
  {/if}

  <h3 class="sub">Link a new device</h3>
  {#if linked}
    <!-- The code is spent the moment a device redeems it. Showing it on after
         that, under "anyone who scans this can add a device", said the opposite
         of what had happened. -->
    <div class="linked" role="status">
      <strong>{linked.name || "Your new device"} is linked.</strong>
      It is joining your guilds now. This code has been used and no longer works.
    </div>
  {:else if status?.prompt}
    <section class="ask" aria-label="Confirm the new device" aria-live="assertive">
      <p class="ask-who"><strong>{status.prompt.deviceName}</strong> wants to join your account.</p>
      <p class="muted">Check that the new device shows the same code:</p>
      <div class="sas" aria-label="Code {status.prompt.sas}">{status.prompt.sas}</div>
      <div class="ask-btns">
        <button class="primary" onclick={() => answer(true)}>Yes, link it</button>
        <button class="ghost" onclick={() => answer(false)}>No</button>
      </div>
      <p class="muted small">
        {status.prompt.secondsLeft} s left. Say no if the codes differ or you did not just scan this
        code: someone else may have it.
      </p>
    </section>
  {:else if sending}
    <div class="linked" role="status">
      <span class="ls-spin"></span> Sending your account to {answered.name}…
    </div>
  {:else if answered || status?.used || status?.expired}
    <div class="spent" role="status">
      <p>
        {#if answered && !answered.yes}
          {answered.name} was turned away. This code no longer works.
        {:else if answered}
          The new device did not finish linking. This code no longer works.
        {:else if status?.used}
          This code has been used and no longer works.
        {:else}
          This code has expired.
        {/if}
      </p>
      <button class="primary" onclick={newCode}>Show a new code</button>
    </div>
  {:else}
  <p class="muted">
    On your other device, choose <strong>“Add this device to my account”</strong> and
    scan this code (or paste it). It works for two minutes. When the device connects,
    you confirm it here before anything is sent.
  </p>

  {#if error}
    <div class="error">{error}</div>
  {:else if qr}
    <div class="qr-wrap">
      <img class="qr" src={qr} alt="Device linking QR code" />
    </div>
    <button class="ghost ghost-sm copy" onclick={copyCode}>
      {copied ? "Copied ✓" : "Copy code instead"}
    </button>
  {:else}
    <p class="muted">Generating code…</p>
  {/if}

  <Caution
    text="Only scan this with a device you own."
    detail="Anyone who sees this code can ask to join. Nothing is sent until you confirm here that the six digits match the new device."
  />
  {/if}
  {#if status?.oldDevice && !linked}
    <div class="error" role="alert">
      A device with an older Concord tried this code. Update Concord on that device, then scan a new code.
    </div>
  {/if}
</RailShell>

{#if confirming}
  <ConfirmDialog
    title="Unlink {confirming.name || 'this device'}?"
    body={"It stops receiving anything new straight away in guilds and direct messages where you can remove members, and whoever holds it cannot undo that. Anywhere else it stays listed until someone there removes it.\n\nIt erases its own copy of your messages only when it next comes online with Concord running. A device that stays off keeps what it already downloaded, and this cannot reach it. If the device is in someone else's hands, treat everything on it as already read."}
    confirmLabel="Unlink"
    onConfirm={() => unlink(confirming)}
    onClose={() => (confirming = null)}
  />
{/if}

<style>
  .devs {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
  }
  .dev {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-2) var(--sp-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .dev.off .dname {
    color: var(--text-muted);
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-faint);
    flex: none;
  }
  .dot.on {
    background: var(--ok);
  }
  .dname {
    font-weight: 600;
  }
  .dmeta {
    margin-inline-end: auto;
    font-size: var(--fs-compact);
  }
  .unlink {
    font-size: var(--fs-compact);
    padding: var(--sp-1) var(--sp-3);
    background: transparent;
    border: 1px solid var(--border);
    color: var(--danger-text);
  }
  .unlink:hover {
    border-color: var(--danger);
  }
  .pending {
    padding: var(--sp-2) var(--sp-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font-size: var(--fs-compact);
  }
  .pending p,
  .pending ul {
    margin: 0 0 var(--sp-1);
  }
  .pending ul {
    padding-inline-start: var(--sp-4);
  }
  .sub {
    margin: var(--sp-3) 0 0;
    font-size: var(--fs-ui);
  }
  .linked {
    padding: var(--sp-3);
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--ok) 14%, transparent);
    border: 1px solid color-mix(in srgb, var(--ok) 45%, transparent);
  }
  .linked .ls-spin {
    display: inline-block;
    vertical-align: -2px;
    margin-inline-end: var(--sp-2);
  }
  .ls-spin {
    width: 14px;
    height: 14px;
    border: 2px solid color-mix(in srgb, var(--text-muted) 35%, transparent);
    border-top-color: var(--accent-hover);
    border-radius: 50%;
    animation: ls-spin 0.7s linear infinite;
  }
  @keyframes ls-spin {
    to {
      transform: rotate(360deg);
    }
  }
  .ask,
  .spent {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding: var(--sp-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .ask {
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
  }
  .ask p,
  .spent p {
    margin: 0;
  }
  .ask-who {
    font-size: var(--fs-ui);
  }
  .sas {
    align-self: center;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 2rem;
    font-weight: 700;
    letter-spacing: 0.12em;
    font-variant-numeric: tabular-nums;
    padding: var(--sp-1) var(--sp-3);
  }
  .ask-btns {
    display: flex;
    gap: var(--sp-2);
  }
  .ask-btns button {
    flex: 1;
  }
  .small {
    font-size: var(--fs-compact);
  }
  .spent button {
    align-self: flex-start;
  }
  .qr-wrap {
    display: flex;
    justify-content: center;
    padding: var(--sp-3);
    background: #fff;
    border-radius: var(--radius-md);
    margin: 6px 0;
  }
  .qr {
    width: 260px;
    height: 260px;
    image-rendering: pixelated;
  }
  .copy {
    align-self: center;
  }
</style>
