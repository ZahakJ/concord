<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, selectGuild } from "../lib/state.svelte.js";
  import { joinFailure } from "../lib/joinerror.js";
  let { onSubmit, onClose, error } = $props();
  let code = $state("");
  let busy = $state(false);
  let submitted = $state(""); // so a stale backend error hides once the code is edited

  async function join() {
    if (!code.trim() || busy) return;
    if (already) {
      // Joining again used to go through, and posted a second "has joined"
      // to everyone. You are there already; go there.
      onClose?.();
      selectGuild(already.id);
      return;
    }
    submitted = code.trim();
    busy = true;
    await onSubmit(code.trim(), preview?.name || "");
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
  const failure = $derived(showError ? joinFailure(error) : null);
  // A refusal that is not the code's fault keeps the preview: the person
  // should still see which guild the code is for.
  const ready = $derived(!!preview && !failure?.field);
  // A guild this device is already a member of (and not thrown out of).
  const already = $derived(preview ? S.guilds.find((g) => g.id === preview.id && !g.evicted) : null);

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
  <!-- Before a code: one line saying what to paste. Once a real code lands,
       a preview row — the guild's bubble, its name, where things stand —
       left-aligned like every dialog body. aria-live so the reveal is spoken.
       (A centred dashed door tile stood in for the guild before; it was a
       decoration in a dialog that has one job.) -->
  <div class="hero" class:ready aria-live="polite">
    {#if ready}
      <span class="bubble" class:ready class:knock={busy} class:bad={showError} style={tint(preview.id)}>
        {#if initials}{initials}{:else}<Icon name="door" size={18} />{/if}
      </span>
    {/if}
    {#if ready}
      <span class="hero-text">
      <span class="dest">{preview.name || "A guild awaits"}</span>
      <span class="sub arrived" class:warn={!!failure}>
        {#if failure}
          {failure.title}
        {:else if already}
          You're already in this guild.
        {:else}
          {busy ? "Knocking — whoever made this code lets you in…" : "You're invited — step in."}
        {/if}
      </span>
      </span>
    {:else}
      <span class="sub" class:warn={!!failure || code.trim().length > 8}>
        {#if failure}
          {failure.title}
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
      aria-label="Invite code"
      aria-invalid={failure?.field ? "true" : undefined}
      aria-describedby={failure ? "join-error" : undefined}
      placeholder="Paste invite code here…"
      bind:value={code}
      autofocus={!S.isMobile}
      onkeydown={(e) => e.key === "Enter" && !e.shiftKey && (e.preventDefault(), join())}
    ></textarea>
    <button class="paste" title="Paste from clipboard" aria-label="Paste from clipboard" onclick={paste}>
      <Icon name="download" size={13} /> Paste
    </button>
  </div>

  {#if failure}<div class="error shake" id="join-error" role="alert"><Icon name="close" size={12} /> {failure.detail}</div>{/if}

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <!-- The decoder is a preview, not a gate: an undecodable code can still be
         submitted and judged by the server, so a peek bug can't lock anyone out. -->
    <button class="primary" class:go={ready && !busy} onclick={join} disabled={!code.trim() || busy}>
      {busy ? "Joining…" : already ? "Open it" : "Join"}
    </button>
  </div>
</Modal>

<style>
  .hero {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    min-height: 20px;
  }
  .hero.ready {
    padding: var(--sp-1) 0;
  }
  .hero-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .bubble {
    flex: none;
    width: 40px;
    height: 40px;
    border-radius: var(--radius-md);
    display: grid;
    place-items: center;
    font-size: var(--fs-ui);
    font-weight: 600;
    color: var(--accent-hover);
    background: var(--accent-soft);
    border: 1px dashed color-mix(in srgb, var(--accent) 45%, transparent);
    transition:
      color var(--dur-calm) ease,
      border-color var(--dur-calm) ease,
      box-shadow var(--dur-calm) ease;
  }
  /* The reveal: dashes solidify, the guild's own tint fills in (inline style),
     and it gets the same glow a named bubble earns in ModalCreate. */
  .bubble.ready {
    border: 1px solid transparent;
    animation: arrive 0.35s var(--ease-spring);
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
    }
  }
  .dest {
    font-size: var(--fs-ui);
    font-weight: 600;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    animation: rise 0.25s ease both;
  }
  .sub {
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .sub.arrived {
    color: var(--ok-text);
    animation: rise 0.25s ease both;
  }
  .sub.warn,
  .sub.arrived.warn {
    color: var(--warn-text);
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
    transition: border-color var(--dur-calm) ease;
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
  :global(:root[data-motion="reduced"]) .bubble,
  :global(:root[data-motion="reduced"]) .bubble.ready,
  :global(:root[data-motion="reduced"]) .bubble.knock,
  :global(:root[data-motion="reduced"]) .dest,
  :global(:root[data-motion="reduced"]) .sub.arrived,
  :global(:root[data-motion="reduced"]) .shake {
    animation: none;
    transition: none;
  }
</style>
