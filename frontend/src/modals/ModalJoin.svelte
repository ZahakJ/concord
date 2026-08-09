<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S } from "../lib/state.svelte.js";
  let { onSubmit, onClose, error } = $props();
  let code = $state("");
  let busy = $state(false);
  let submitted = $state(""); // so a stale backend error hides once the code is edited

  async function join() {
    if (!code.trim() || busy) return;
    submitted = code.trim();
    busy = true;
    await onSubmit(code.trim());
    busy = false; // if the modal is still open, the parent set an error
  }

  async function paste() {
    try {
      code = (await navigator.clipboard.readText()).trim();
    } catch {
      /* clipboard blocked — user can paste manually */
    }
  }

  function b64url(s) {
    const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (s.length % 4)) % 4));
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  }
  function uvarint(b, i) {
    let x = 0,
      s = 1;
    for (;;) {
      if (i >= b.length) throw new Error("truncated");
      const c = b[i++];
      x += (c & 0x7f) * s;
      if (c < 0x80) return [x, i];
      s *= 128;
    }
  }

  // Read the guild id + name straight out of the pasted code, so the modal can
  // show WHERE the invite leads before anything touches the network. Mirrors
  // internal/app/invitecode.go: "CI1" + base64url(flag, gid, name, …) for
  // compact codes, base64url(JSON{g,n,p,…}) for legacy ones. We only peek at
  // the first two fields; the server remains the authority on validity — a
  // decode failure here never blocks joining, it just means no preview.
  function peek(t) {
    try {
      if (t.startsWith("CI1")) {
        const b = b64url(t.slice(3));
        const flag = b[0];
        if (flag !== 0 && flag !== 1) return null;
        let [len, i] = uvarint(b, 1);
        if (i + len > b.length) return null;
        const gid =
          flag === 1
            ? Array.from(b.slice(i, i + len), (x) => x.toString(16).padStart(2, "0")).join("")
            : new TextDecoder().decode(b.slice(i, i + len));
        i += len;
        let nlen;
        [nlen, i] = uvarint(b, i);
        if (i + nlen > b.length) return null;
        return { id: gid, name: new TextDecoder().decode(b.slice(i, i + nlen)) };
      }
      if (t.length > 80 && !/\s/.test(t)) {
        const j = JSON.parse(new TextDecoder().decode(b64url(t)));
        if (j && typeof j.g === "string" && j.g && j.p) return { id: j.g, name: j.n || "" };
      }
    } catch {
      /* not a decodable code */
    }
    return null;
  }

  const preview = $derived.by(() => peek(code.trim()));
  // Editing the code after a rejection withdraws the error immediately —
  // otherwise "bad invite code" sits accusing a fresh, unsent paste.
  const showError = $derived(!!error && code.trim() === submitted);
  const ready = $derived(!!preview && !showError);

  // Same hash and initials the rail uses, so the bubble previewed here is
  // pixel-for-pixel the one that lands in the rail after joining.
  function tint(id) {
    let h = 0;
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
    const hue = h % 360;
    return `background:linear-gradient(135deg, hsl(${hue} 42% 34%), hsl(${(hue + 45) % 360} 48% 25%));color:#fff`;
  }
  const initials = $derived(
    (preview?.name || "")
      .trim()
      .split(/\s+/)
      .map((w) => w[0])
      .join("")
      .slice(0, 2),
  );
</script>

<Modal title="Join a guild" {onClose}>
  <!-- The doorway: an empty dashed bubble that becomes the guild's actual rail
       bubble the moment a real code lands. aria-live so the reveal is spoken. -->
  <div class="hero" aria-live="polite">
    <span class="bubble" class:ready class:knock={busy} class:bad={showError} style={ready ? tint(preview.id) : ""}>
      {#if ready && initials}
        {initials}
      {:else}
        <Icon name="door" size={24} />
      {/if}
    </span>
    {#if ready}
      <span class="dest">{preview.name || "A guild awaits"}</span>
      <span class="sub arrived">{busy ? "Opening the door…" : "You're invited — step in."}</span>
    {:else}
      <span class="dest ph">Somewhere new</span>
      <span class="sub">
        {#if showError}
          <!-- The code was whole — the guild refused it (expired, or the owner
               is unreachable). Don't tell them to re-copy what they copied fine. -->
          That code didn't open the door. Ask your friend for a fresh one.
        {:else if code.trim().length > 8}
          That doesn't look like a whole code — check you copied all of it.
        {:else}
          Paste the invite code a friend sent you.
        {/if}
      </span>
    {/if}
  </div>

  <div class="input-well" class:err={showError} class:ok={ready}>
    <!-- Not autofocused on a phone: the soft keyboard would open while the
         sheet is still sliding up and make the panel jump (see ModalCreate). -->
    <textarea
      rows="4"
      placeholder="Paste invite code here…"
      bind:value={code}
      autofocus={!S.isMobile}
      onkeydown={(e) => e.key === "Enter" && !e.shiftKey && (e.preventDefault(), join())}
    ></textarea>
    <button class="paste" title="Paste from clipboard" aria-label="Paste from clipboard" onclick={paste}>
      <Icon name="download" size={13} /> Paste
    </button>
  </div>

  {#if showError}<div class="error shake"><Icon name="close" size={12} /> {error}</div>{/if}

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <!-- The decoder is a preview, not a gate: an undecodable code can still be
         submitted and judged by the server, so a peek bug can't lock anyone out. -->
    <button class:go={ready && !busy} onclick={join} disabled={!code.trim() || busy}>
      {busy ? "Joining…" : "Join"}
    </button>
  </div>
</Modal>

<style>
  .hero {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-1) 0 0;
    text-align: center;
  }
  .bubble {
    width: 64px;
    height: 64px;
    border-radius: 20px;
    display: grid;
    place-items: center;
    font-size: var(--fs-display);
    font-weight: 700;
    color: var(--accent-hover);
    background: var(--accent-soft);
    border: 1px dashed color-mix(in srgb, var(--accent) 45%, transparent);
    transition:
      color 0.25s ease,
      border-color 0.25s ease,
      box-shadow 0.25s ease;
  }
  /* The reveal: dashes solidify, the guild's own tint fills in (inline style),
     and it gets the same glow a named bubble earns in ModalCreate. */
  .bubble.ready {
    border: 1px solid transparent;
    box-shadow: var(--accent-glow);
    animation: arrive 0.35s cubic-bezier(0.34, 1.5, 0.5, 1);
  }
  /* Joining can take a few seconds of dialing — the bubble breathes so the
     wait reads as knocking, not a hang. */
  .bubble.knock {
    animation: knock 1.1s ease-in-out infinite;
  }
  .bubble.bad {
    border-color: color-mix(in srgb, var(--danger) 55%, transparent);
    color: var(--danger-text);
    box-shadow: none;
  }
  @keyframes arrive {
    from {
      transform: scale(0.85);
    }
  }
  @keyframes knock {
    50% {
      transform: scale(1.05);
      box-shadow: 0 0 34px color-mix(in srgb, var(--accent) 50%, transparent);
    }
  }
  .dest {
    font-size: var(--fs-title);
    font-weight: 700;
    max-width: 280px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    animation: rise 0.25s ease both;
  }
  .dest.ph {
    color: var(--text-faint);
    font-weight: 500;
    font-size: var(--fs-ui);
    animation: none;
  }
  .sub {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    max-width: 300px;
  }
  .sub.arrived {
    color: var(--ok-text);
    animation: rise 0.25s ease both;
  }
  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(3px);
    }
  }
  .input-well {
    position: relative;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-0);
    overflow: hidden;
    transition: border-color 0.2s ease;
  }
  .input-well:focus-within {
    border-color: var(--accent);
  }
  .input-well.err {
    border-color: var(--danger);
  }
  .input-well.ok {
    border-color: color-mix(in srgb, var(--ok) 60%, transparent);
  }
  /* A quick left-right shudder when a join fails — reads as "nope" without a
     modal-on-modal. */
  .shake {
    animation: shake 0.35s cubic-bezier(0.36, 0.07, 0.19, 0.97);
  }
  @keyframes shake {
    10%,
    90% {
      transform: translateX(-1px);
    }
    20%,
    80% {
      transform: translateX(2px);
    }
    30%,
    70% {
      transform: translateX(-3px);
    }
    50% {
      transform: translateX(3px);
    }
  }
  textarea {
    border: none;
    background: transparent;
    resize: vertical;
    font-family: var(--mono-font);
    font-size: var(--fs-compact);
    min-height: 80px;
  }
  textarea:focus {
    border: none;
  }
  .paste {
    position: absolute;
    top: 8px;
    right: 8px;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-compact);
    padding: 4px 10px;
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .paste:hover,
  .paste:active {
    background: var(--border);
    color: var(--text);
  }
  .error {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  /* Once the destination is known the commit button carries the glow too —
     the whole dialog points at Join. */
  .actions button.go {
    box-shadow: var(--accent-glow);
  }
  @media (prefers-reduced-motion: reduce) {
    .bubble,
    .bubble.ready,
    .bubble.knock,
    .dest,
    .sub.arrived,
    .shake {
      animation: none;
      transition: none;
    }
  }
</style>
