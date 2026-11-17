<script>
  // Verify a contact by safety number. "Show my code" renders a QR holding both
  // account fingerprints; "Scan theirs" reads the other person's. A match marks
  // them verified through the same call the read-aloud button uses; a mismatch
  // only warns. The read-aloud number stays below as the fallback.
  import Modal from "./Modal.svelte";
  import QRScanner from "../QRScanner.svelte";
  import { api } from "../lib/api.js";
  import { S, flash, memberByFpr, refreshRightPanel } from "../lib/state.svelte.js";
  import { encodeSafetyCode, checkSafetyCode } from "../lib/safetycode.js";

  let { fingerprint, name = "", onClose } = $props();

  const who = $derived(name || memberByFpr(fingerprint)?.name || "them");
  const mine = $derived(S.identity?.fingerprint || "");
  const readAloud = $derived(
    String(fingerprint || "").replace(/\s+/g, "").replace(/(.{4})/g, "$1 ").trim(),
  );

  let verified = $state(!!memberByFpr(fingerprint)?.verified);
  let qr = $state("");
  let scanning = $state(false);
  let result = $state(""); // "" | "mismatch" | "invalid" | "failed"

  const canScan = typeof navigator !== "undefined" && !!navigator.mediaDevices?.getUserMedia;

  $effect(() => {
    let live = true;
    const raw = encodeSafetyCode(mine, fingerprint);
    if (!raw) return;
    import("qrcode")
      .then(({ default: QRCode }) =>
        QRCode.toDataURL(raw, { margin: 2, width: 320, errorCorrectionLevel: "M" }),
      )
      .then((url) => {
        if (live) qr = url;
      })
      .catch(() => {});
    return () => (live = false);
  });

  async function mark() {
    try {
      await api.verifyFingerprint(fingerprint);
      verified = true;
      await refreshRightPanel();
      flash(`${who} verified`, "success");
    } catch (err) {
      flash(err);
    }
  }

  async function onScanned(text) {
    scanning = false;
    const verdict = checkSafetyCode(text, mine, fingerprint);
    if (verdict !== "match") {
      result = verdict;
      return;
    }
    result = "";
    await mark();
  }
</script>

<Modal title="Verify {who}" {onClose}>
  {#if verified}
    <p class="ok">{who} is verified. Their messages are from the person you checked.</p>
  {/if}

  {#if result === "mismatch"}
    <p class="warn" role="alert">
      That code does not match you and {who}. Nothing was marked. Don't trust this person until you
      have compared again in person.
    </p>
  {:else if result === "invalid"}
    <p class="warn" role="alert">That isn't a Concord safety code. Nothing was marked.</p>
  {/if}

  <p class="hint">Show your code to {who} and scan theirs. Both phones should agree.</p>
  {#if qr}
    <img class="qr" src={qr} alt="Your safety code for {who}" />
  {/if}
  {#if canScan}
    <button class="primary" onclick={() => ((result = ""), (scanning = true))}>Scan theirs</button>
  {:else}
    <p class="hint">This device has no camera to scan with. Compare the number below instead.</p>
  {/if}

  <details class="alt">
    <summary>Compare the number out loud instead</summary>
    <p class="hint">Read this to {who} on a call. If it is the same on their screen, verify.</p>
    <code class="fpr">{readAloud}</code>
    {#if !verified}
      <button class="ghost" onclick={mark}>It matches, verify</button>
    {/if}
  </details>
</Modal>

{#if scanning}
  <QRScanner onScan={onScanned} onClose={() => (scanning = false)} />
{/if}

<style>
  p {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.5;
    color: var(--text-muted);
  }
  .ok {
    color: var(--text);
  }
  .warn {
    color: var(--danger-text);
  }
  .qr {
    width: 220px;
    max-width: 70%;
    align-self: center;
    border-radius: var(--radius-md);
    background: #fff;
  }
  .alt summary {
    cursor: pointer;
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .fpr {
    display: block;
    margin: var(--sp-2) 0;
    font-family: var(--font-mono, monospace);
    word-break: break-word;
  }
</style>
