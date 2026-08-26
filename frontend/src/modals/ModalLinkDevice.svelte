<script>
  // "Link a device": the issuer side. Shows a QR (and the raw code as a
  // fallback) that a new device scans/pastes to join this account. The code
  // carries a single-use secret + how to reach us; it expires in ~2 minutes.
  import { onMount, onDestroy } from "svelte";
  import { api } from "../lib/api.js";
  import { linkURLFor } from "../lib/deeplink.js";
  import { flash } from "../lib/state.svelte.js";
  import Modal from "./Modal.svelte";

  let { onClose } = $props();

  let code = $state("");
  let qr = $state("");
  let error = $state("");
  let copied = $state(false);

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

<Modal title="Link a device" {onClose}>
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
    <button class="ghost-sm copy" onclick={copyCode}>
      {copied ? "Copied ✓" : "Copy code instead"}
    </button>
  {:else}
    <p class="muted">Generating code…</p>
  {/if}

  <p class="muted tiny warn">
    Anyone who scans this within the next couple of minutes can add a device to
    your account — only use it on a device you own.
  </p>
</Modal>

<style>
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
