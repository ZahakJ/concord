<script>
  // "Link a device": the issuer side. Shows a QR (and the raw code as a
  // fallback) that a new device scans/pastes to join this account. The code
  // carries a single-use secret + how to reach us; it expires in ~2 minutes.
  import { onMount, onDestroy } from "svelte";
  import { api } from "../lib/api.js";
  import { linkURLFor } from "../lib/deeplink.js";
  import { flash } from "../lib/state.svelte.js";
  import { newlyLinked } from "../lib/linkwatch.js";
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

  function fmtAgo(sec) {
    if (!sec) return "never";
    const s = Math.max(0, Math.floor(Date.now() / 1000) - sec);
    if (s < 45) return "just now";
    if (s < 3600) return `${Math.round(s / 60)}m ago`;
    if (s < 86400) return `${Math.round(s / 3600)}h ago`;
    return `${Math.round(s / 86400)}d ago`;
  }

  let confirming = $state(null);
  async function unlink(dev) {
    confirming = null;
    try {
      await api.unlinkDevice(dev.key);
      flash(
        `${dev.name || "That device"} unlinked — it loses access to your guilds now, and erases itself the next time it comes online`,
        "success",
      );
    } catch (err) {
      flash(err);
    }
    refresh();
  }

  onMount(async () => {
    try {
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
    } catch (e) {
      error = String(e?.message || e);
    }
  });

  onDestroy(() => {
    clearInterval(watch);
    // Burn the offer server-side when the dialog closes so it can't be redeemed
    // after the user walks away.
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

  <h3 class="sub">Link a new device</h3>
  {#if linked}
    <!-- The code is spent the moment a device redeems it. Showing it on after
         that, under "anyone who scans this can add a device", said the opposite
         of what had happened. -->
    <div class="linked" role="status">
      <strong>{linked.name || "Your new device"} is linked.</strong>
      It is joining your guilds now. This code has been used and no longer works.
    </div>
  {:else}
  <p class="muted">
    On your other device, choose <strong>“Link to an existing account”</strong> and
    scan this code (or paste it). It works for a few minutes, then expires.
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

  <p class="muted tiny warn">
    Anyone who scans this within the next couple of minutes can add a device to
    your account — only use it on a device you own.
  </p>
  {/if}
</RailShell>

{#if confirming}
  <ConfirmDialog
    title="Unlink {confirming.name || 'this device'}?"
    body={"It stops receiving anything new from your guilds straight away — that part is immediate and cannot be undone by whoever holds it.\n\nIt erases its own copy of your messages only when it next comes online with Concord running. A device that stays off keeps what it already downloaded, and this cannot reach it. If the device is in someone else's hands, treat everything on it as already read."}
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
  .tiny {
    font-size: var(--fs-compact);
  }
  .warn {
    color: var(--danger-text);
  }
</style>
