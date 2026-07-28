<script>
  // The just-created instant meeting. Two ways in:
  //   • the GUEST LINK — they click it and land in a browser meeting window,
  //     no install, no account (the killer demo);
  //   • the invite code — for people who have (or want) the full app, which
  //     keeps them fully end-to-end encrypted.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";

  let { code, guestLink = "", onClose } = $props();

  const DOWNLOAD = "https://github.com/ZahakJ/concord-dist/releases/latest";

  const guestBlurb = guestLink
    ? `Hop on a quick call with me ⚡\n\n${guestLink}\n\n` +
      `Just click it — it opens in your browser, no install, no account.`
    : "";

  const appBlurb =
    `Join my Concord meeting ⚡\n` +
    `\n` +
    `1. Grab Concord (free, no account needed): ${DOWNLOAD}\n` +
    `2. Open it, pick any passphrase, and paste this invite:\n` +
    `\n` +
    `${code}\n` +
    `\n` +
    `See you there! (End-to-end encrypted — no server ever sees the call.)`;

  let copied = $state("");
  function copy(what, text) {
    navigator.clipboard?.writeText(text);
    copied = what;
    setTimeout(() => (copied = ""), 1600);
  }
</script>

<Modal title="Your meeting is live" wide {onClose}>
  {#if guestLink}
    <section class="way primary-way">
      <div class="way-head">
        <span class="chip guest"><Icon name="link" size={15} /></span>
        <div class="way-text">
          <strong>Guest link — no install</strong>
          <span class="muted">They click it and they're in, right in their browser.</span>
        </div>
      </div>
      <div class="link-row">
        <code class="link">{guestLink}</code>
        <button class:done={copied === "link"} onclick={() => copy("link", guestLink)}>
          {copied === "link" ? "Copied ✓" : "Copy link"}
        </button>
      </div>
      <button class="ghost small" class:done={copied === "guest"} onclick={() => copy("guest", guestBlurb)}>
        {copied === "guest" ? "Copied ✓" : "Copy link + a friendly note"}
      </button>
      <p class="muted tiny">
        Guests are chat-only and labelled in the room — their messages pass
        through you, so they aren't end-to-end encrypted like full members.
      </p>
    </section>
  {/if}

  <section class="way">
    <div class="way-head">
      <span class="chip"><Icon name="concorde" size={15} /></span>
      <div class="way-text">
        <strong>Invite to the full app</strong>
        <span class="muted">Voice + chat, fully end-to-end encrypted. Best for people who'll stick around.</span>
      </div>
    </div>
    <div class="btn-row">
      <button class="ghost" class:done={copied === "code"} onclick={() => copy("code", code)}>
        {copied === "code" ? "Copied ✓" : "Copy invite code"}
      </button>
      <button class:done={copied === "app"} onclick={() => copy("app", appBlurb)}>
        <Icon name="copy" size={14} />
        {copied === "app" ? "Copied ✓" : "Copy invitation"}
      </button>
    </div>
  </section>

  {#if !guestLink}
    <p class="muted tiny nofoot">
      Guest links (browser, no install) need a rendezvous server — set one in
      Settings → Connection.
    </p>
  {/if}
</Modal>

<style>
  .way {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 9px;
  }
  .way + .way {
    margin-top: 10px;
  }
  .primary-way {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
    background: color-mix(in srgb, var(--accent) 6%, transparent);
  }
  .way-head {
    display: flex;
    align-items: flex-start;
    gap: 10px;
  }
  .chip {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    flex: none;
    border-radius: 8px;
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .chip.guest {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .way-text {
    display: flex;
    flex-direction: column;
    gap: 1px;
    font-size: 13px;
  }
  .way-text .muted {
    font-size: 12px;
    line-height: 1.45;
  }
  .link-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .link {
    flex: 1;
    min-width: 0;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    font-size: 11.5px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .btn-row {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .btn-row button,
  .link-row button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex: none;
  }
  .small {
    font-size: 12px;
    padding: 6px 10px;
    align-self: flex-start;
  }
  .done {
    background: var(--ok) !important;
    color: #fff;
  }
  .tiny {
    font-size: 11px;
    margin: 0;
    line-height: 1.5;
  }
  .nofoot {
    margin-top: 10px;
  }
</style>
