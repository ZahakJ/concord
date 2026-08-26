<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { onMount, onDestroy } from "svelte";
  import { api } from "../lib/api.js";
  import { S, flash, openPanel, setPref } from "../lib/state.svelte.js";
  import { haptic } from "../lib/touch.js";
  import { selfUpdateAllowed } from "../lib/installsource.js";

  let { onClose } = $props();
  let phrase = $state("");
  let copiedPhrase = $state(false);
  let phraseOpen = $state(false);

  // Recovery phrase disclosure: first open fetches the words, later opens just
  // collapse/expand what's already revealed.
  async function togglePhrase() {
    if (!phraseOpen && !phrase) {
      try {
        phrase = await api.revealMnemonic();
      } catch (err) {
        flash(err);
        return;
      }
      // Coming here and reading them is the thing the nudge was asking for, so
      // reaching this line is the answer — no second confirmation to click.
      if (S.prefs.backupPending) setPref("backupPending", false);
    }
    phraseOpen = !phraseOpen;
  }
  function copyPhrase() {
    navigator.clipboard?.writeText(phrase);
    haptic("light"); // the clipboard is invisible; the buzz is the receipt
    copiedPhrase = true;
    setTimeout(() => (copiedPhrase = false), 1600);
  }

  // App version for the subtle footer ("dev" for unstamped local builds).
  let appVersion = $state("");
  let copiedVersion = $state(false);
  function copyVersion() {
    navigator.clipboard?.writeText(`Concord ${appVersion}`);
    haptic("light");
    copiedVersion = true;
    setTimeout(() => (copiedVersion = false), 1400);
  }

  // ---- software update (full in-place self-update) ----
  // Desktop/web swap their own binary. A sideloaded Android build can't
  // self-swap, but the card still checks and one-taps the APK download — the OS
  // installer upgrades in place (same signing key). A build installed from Play
  // must not offer any of that, and iOS is store-only, so the card hides in
  // both cases. selfUpdateAllowed() asks the running install where it came
  // from; until it answers, the card stays down rather than flashing on and
  // off in front of someone on a store build.
  let canUpdate = $state(false);
  (async () => {
    canUpdate = await selfUpdateAllowed();
  })();
  let updInfo = $state(null); // CheckForUpdate view, once the user checks
  let peerInfo = $state(null); // CheckPeerUpdate view, when GitHub had nothing
  let checking = $state(false);
  let canSelf = $state(false); // this install can swap its own binary
  let upd = $state({ phase: "idle", percent: 0 }); // backend UpdateProgress
  let restarting = $state(false);
  let pollTimer;

  async function checkNow() {
    checking = true;
    peerInfo = null;
    try {
      updInfo = await api.checkForUpdate();
    } catch (err) {
      flash(err);
    }
    // GitHub is the canonical source, but it can be unreachable, rate-limited
    // or blocked on the day a fix ships. A peer we're already connected to may
    // hold the very same signed build; the signature is verified either way, so
    // where the bytes came from doesn't change what gets installed. Silent on
    // failure — this is a bonus source, never the reason a check "fails".
    if (!updInfo?.available) {
      try {
        peerInfo = await api.checkPeerUpdate();
      } catch {
        /* locked, or a backend without peer updates */
      }
    }
    checking = false;
  }
  async function updateNow() {
    try {
      await api.applyUpdate();
      poll();
    } catch (err) {
      flash(err);
    }
  }
  async function updateFromPeer() {
    try {
      await api.applyPeerUpdate();
      poll();
    } catch (err) {
      flash(err);
    }
  }
  function poll() {
    clearInterval(pollTimer);
    pollTimer = setInterval(async () => {
      try {
        upd = await api.updateState();
      } catch {
        return;
      }
      if (upd.phase === "ready" || upd.phase === "error" || upd.phase === "idle") {
        clearInterval(pollTimer);
      }
    }, 450);
  }
  // Restart the backend into the new binary, wait for the NEW version to be
  // the one answering, then reload. Two subtleties make this seamless:
  //  - a full-bleed curtain (S.restarting, App.svelte) goes up first, so the
  //    outgoing version's UI — update banner and all — is never visible while
  //    the old process winds down;
  //  - the poll compares AppVersion instead of merely waiting for an answer,
  //    so a reply from the not-yet-dead OLD process can't trigger a premature
  //    reload into the old frontend.
  async function restartNow() {
    restarting = true;
    const before = appVersion;
    S.restarting = true; // raise the curtain
    S.modal = null;
    try {
      await api.restartApp();
    } catch {
      /* the connection may drop mid-response — that IS the restart */
    }
    const t0 = Date.now();
    const timer = setInterval(async () => {
      try {
        const v = await api.appVersion();
        if (v && v !== before) {
          clearInterval(timer);
          location.reload();
          return;
        }
      } catch {
        /* backend mid-swap — keep waiting */
      }
      if (Date.now() - t0 > 45000) {
        clearInterval(timer);
        location.reload(); // give up gracefully; worst case the old UI returns
      }
    }, 600);
  }

  onMount(async () => {
    try {
      appVersion = (await api.appVersion()) || "";
    } catch {
      /* ignore */
    }
    try {
      canSelf = !!(await api.canSelfUpdate());
      // Resume progress display if an update is already in flight/installed.
      upd = await api.updateState();
      if (upd.phase === "downloading" || upd.phase === "verifying") poll();
    } catch {
      /* older backend without self-update — the card just offers Check */
    }
    // The boot-time check may already know an update exists; surface it.
    if (S.update?.available) updInfo = S.update;
    // Arriving from the update banner's "Update now": start installing right
    // away (when this build can self-swap) — one click, zero ceremony.
    if (S.modal?.startUpdate && updInfo?.available && canSelf && upd.phase === "idle") {
      updateNow();
    }
  });

  onDestroy(() => clearInterval(pollTimer));

</script>

<Modal title="Settings" wide {onClose}>
  <!-- ACCOUNT -->
  <section class="grp">
    <div class="sec-label">Account</div>
    <div class="card">
      <button class="row" onclick={() => (S.modal = { kind: "profile", from: "settings" })}>
        <Avatar
          name={S.displayName || S.identity.displayName || "You"}
          emoji={S.identity.emoji}
          color={S.identity.color}
          image={S.identity.avatar}
          size={36}
        />
        <span class="row-text">
          <span class="row-title">{S.displayName || S.identity.displayName || "Your profile"}</span>
          <span class="row-sub">Name, avatar, color, status &amp; bio</span>
        </span>
        <span class="chev">›</span>
      </button>
      <button class="row" onclick={() => (S.modal = { kind: "appearance", from: "settings" })}>
        <span class="appearance-chip" aria-hidden="true"></span>
        <span class="row-text">
          <span class="row-title">Appearance</span>
          <span class="row-sub">Theme, colour, shape &amp; type</span>
        </span>
        <span class="chev">›</span>
      </button>
      <button class="row" onclick={() => openPanel("devices", "settings")}>
        <span class="chip"><Icon name="mic" size={17} /></span>
        <span class="row-text">
          <span class="row-title">Voice &amp; Video</span>
          <span class="row-sub">Microphone, speaker &amp; camera</span>
        </span>
        <span class="chev">›</span>
      </button>
      <button class="row" onclick={() => openPanel("notifications", "settings")}>
        <span class="chip"><Icon name="bell" size={17} /></span>
        <span class="row-text">
          <span class="row-title">Notifications &amp; sounds</span>
          <span class="row-sub">Chimes, pings &amp; your ringtone</span>
        </span>
        <span class="chev">›</span>
      </button>
      <button class="row" onclick={() => openPanel("privacy", "settings")}>
        <span class="chip"><Icon name="lock" size={17} /></span>
        <span class="row-text">
          <span class="row-title">Privacy &amp; safety</span>
          <span class="row-sub">What leaves this device, and who can reach you</span>
        </span>
        <span class="chev">›</span>
      </button>
      <button class="row" onclick={() => openPanel("bookings", "settings")}>
        <span class="chip"><Icon name="clock" size={17} /></span>
        <span class="row-text">
          <span class="row-title">Bookings</span>
          <span class="row-sub">Office hours &amp; your public booking page</span>
        </span>
        <span class="chev">›</span>
      </button>
      <button class="row" onclick={() => openPanel("connection", "settings")}>
        <span class="chip"><Icon name="link" size={17} /></span>
        <span class="row-text">
          <span class="row-title">Connection</span>
          <span class="row-sub">Rendezvous server &amp; diagnostics</span>
        </span>
        <span class="chev">›</span>
      </button>
      <button class="row" onclick={() => openPanel("linkDevice", "settings")}>
        <span class="chip"><Icon name="devices" size={17} /></span>
        <span class="row-text">
          <span class="row-title">Link a device</span>
          <span class="row-sub">Add your phone or another computer</span>
        </span>
        <span class="chev">›</span>
      </button>
    </div>
  </section>

  <!-- SOFTWARE UPDATE (hidden wherever a store owns updates instead) -->
  {#if canUpdate}
    <section class="grp">
      <div class="sec-label">Software update</div>
      <div class="card pad upd-card">
        <div class="upd-head">
          <span class="chip upd-chip" class:spin-chip={upd.phase === "downloading" || upd.phase === "verifying"}>
            <Icon name="download" size={16} />
          </span>
          <span class="row-text">
            <span class="row-title">
              Concord {appVersion === "dev" ? "dev build" : appVersion}
            </span>
            <span class="row-sub">
              {#if upd.phase === "downloading"}
                Downloading {upd.version}… {upd.percent}%
              {:else if upd.phase === "verifying"}
                Verifying {upd.version}…
              {:else if upd.phase === "ready"}
                {upd.version} installed — restart to finish.
              {:else if upd.phase === "error"}
                {upd.error}
              {:else if updInfo?.available}
                {updInfo.latest} is available.
              {:else if peerInfo?.available}
                {peerInfo.latest} is available from
                {peerInfo.peers === 1 ? "a peer" : `${peerInfo.peers} peers`} on your network.
              {:else if updInfo}
                You're on the latest version. ✨
              {:else}
                Updates install in place — one click, no downloads to juggle.
              {/if}
            </span>
          </span>
          {#if restarting}
            <button class="upd-btn" disabled>Restarting…</button>
          {:else if upd.phase === "downloading" || upd.phase === "verifying"}
            <button class="upd-btn" disabled>{upd.percent}%</button>
          {:else if upd.phase === "ready"}
            <button class="upd-btn" onclick={restartNow}>Restart now</button>
          {:else if updInfo?.available && canSelf}
            <button class="upd-btn" onclick={updateNow}>Update now</button>
          {:else if updInfo?.available}
            <a class="upd-btn link" href={updInfo.download || updInfo.url} target="_blank" rel="noreferrer">
              Download {updInfo.latest}
            </a>
          {:else if peerInfo?.available && canSelf}
            <button class="upd-btn" onclick={updateFromPeer}>Update from peer</button>
          {:else}
            <button class="upd-btn ghosted" onclick={checkNow} disabled={checking}>
              {checking ? "Checking…" : "Check for updates"}
            </button>
          {/if}
        </div>
        {#if upd.phase === "downloading" || upd.phase === "verifying"}
          <div class="upd-bar"><span style="width:{upd.percent}%"></span></div>
        {/if}
      </div>
    </section>
  {/if}

  <!-- SECURITY -->
  <section class="grp">
    <div class="sec-label">Security</div>
    <div class="card warn">
      <button class="row" onclick={() => openPanel("backup")}>
        <span class="chip warn-chip"><Icon name="download" size={16} /></span>
        <span class="row-text">
          <span class="row-title">Backup &amp; restore</span>
          <span class="row-sub">
            History lives only on your devices. This is the copy you keep.
          </span>
        </span>
        <span class="chev">›</span>
      </button>
      <button class="row" onclick={togglePhrase} aria-expanded={phraseOpen}>
        <span class="chip warn-chip"><Icon name="alert" size={16} /></span>
        <span class="row-text">
          <span class="row-title">Recovery phrase</span>
          <span class="row-sub">
            24 words that ARE your account — anyone who has them can become you.
          </span>
        </span>
        <span class="chev disclose" class:open={phraseOpen}>›</span>
      </button>
      {#if phraseOpen && phrase}
        <div class="phrase-body">
          <div class="phrase">
            {#each phrase.split(" ") as w, i (i)}
              <span class="word"><span class="num">{i + 1}</span>{w}</span>
            {/each}
          </div>
          <div class="phrase-foot">
            <span class="row-sub">Write them down and keep them somewhere safe.</span>
            <button class="ghost small-btn" class:done={copiedPhrase} onclick={copyPhrase}>
              {copiedPhrase ? "Copied ✓" : "Copy"}
            </button>
          </div>
        </div>
      {/if}
    </div>
    <button
      class="signout"
      onclick={async () => {
        await api.logout();
        location.reload();
      }}
    >
      <Icon name="door" size={15} />
      Sign out (lock this device)
    </button>
  </section>

  <!-- ABOUT: quiet version stamp; click copies it for bug reports. -->
  {#if appVersion}
    <button class="about" onclick={copyVersion} title="Copy version">
      {copiedVersion ? "Copied ✓" : `Concord ${appVersion === "dev" ? "dev build" : appVersion}`}
    </button>
  {/if}
</Modal>

<style>
  /* Sectioned, carded settings — the same structure reads native on both the
     desktop floating dialog and the mobile bottom sheet. */
  .grp {
    display: flex;
    flex-direction: column;
    gap: 7px;
    text-align: left;
    animation: grp-in 0.3s ease both;
    /* The dialog is a flex column that scrolls. Without this these sections are
       shrinkable, so on a short window the browser squeezes them instead of
       scrolling — and .card clips its overflow, so what got squeezed out is
       simply gone. The recovery phrase, eight rows tall and at the bottom of the
       list, was the row that lost. */
    flex: none;
  }
  /* Sections cascade in — a beat apart, settled fast. */
  .grp:nth-child(2) {
    animation-delay: 0.04s;
  }
  .grp:nth-child(3) {
    animation-delay: 0.08s;
  }
  .grp:nth-child(4) {
    animation-delay: 0.12s;
  }
  @keyframes grp-in {
    from {
      opacity: 0;
      transform: translateY(6px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .grp {
      animation: none;
    }
  }
  /* A card whose body is content rather than a list of rows: rows supply their
     own padding, so a card of content needs its own. */
  .card.pad {
    padding: 12px 14px;
    gap: 10px;
  }
  /* ---- software update card ---- */
  .upd-card {
    gap: 10px;
  }
  .upd-head {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .upd-chip {
    flex: none;
  }
  .spin-chip :global(svg) {
    animation: upd-bob 1.1s ease-in-out infinite;
  }
  @keyframes upd-bob {
    0%,
    100% {
      transform: translateY(-1px);
    }
    50% {
      transform: translateY(2px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spin-chip :global(svg) {
      animation: none;
    }
  }
  .upd-btn {
    flex: none;
    font-size: var(--fs-compact);
    padding: 7px 14px;
    border-radius: 999px;
    border: none;
    background: var(--accent);
    color: var(--accent-fg);
    cursor: pointer;
    text-decoration: none;
    box-shadow: var(--accent-glow);
  }
  .upd-btn:hover {
    background: var(--accent-hover);
  }
  .upd-btn:disabled {
    opacity: 0.7;
    cursor: default;
    box-shadow: none;
  }
  .upd-btn.ghosted {
    background: color-mix(in srgb, var(--accent) 15%, transparent);
    color: var(--accent-hover);
    box-shadow: none;
  }
  .upd-btn.ghosted:hover {
    background: color-mix(in srgb, var(--accent) 26%, transparent);
  }
  .upd-btn.link {
    display: inline-flex;
    align-items: center;
  }
  .upd-bar {
    height: 6px;
    border-radius: 999px;
    background: var(--bg-3);
    overflow: hidden;
  }
  .upd-bar span {
    display: block;
    height: 100%;
    border-radius: inherit;
    background: linear-gradient(90deg, var(--accent), var(--accent-hover));
    transition: width 0.3s ease;
  }

  .about {
    align-self: center;
    background: none;
    border: none;
    padding: 2px 8px;
    margin-top: -2px;
    font-size: var(--fs-small);
    letter-spacing: 0.03em;
    color: var(--text-muted);
    cursor: pointer;
    border-radius: 999px;
    transition: color 0.15s ease;
  }
  .about:hover {
    color: var(--text);
  }
  .sec-label {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding: 0 4px;
  }
  .card {
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  /* Hairlines between rows, inset past the icon chip like iOS/Telegram. */
  .card > .row + .row {
    border-top: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 52px;
    padding: 10px 14px;
    background: transparent;
    color: var(--text);
    text-align: left;
    border-radius: 0;
  }
  .row {
    transition: background 0.14s ease;
  }
  .row:hover {
    background: var(--bg-3);
  }
  .row:active {
    background: var(--bg-3);
  }
  .row:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .row:disabled:hover,
  .row:disabled:active {
    background: transparent;
  }
  .row-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .row-title {
    font-size: var(--fs-ui);
    font-weight: 600;
    line-height: 1.3;
  }
  .row-sub {
    font-size: var(--fs-small);
    line-height: 1.45;
    color: var(--text-muted);
  }
  .chev {
    flex-shrink: 0;
    font-size: 20px;
    line-height: 1;
    color: var(--text-faint);
  }
  /* Nav chevrons drift toward the destination on hover (the disclosure caret
     rotates instead — handled below — so it's excluded). */
  .chev:not(.disclose) {
    transition:
      transform 0.15s ease,
      color 0.15s ease;
  }
  .row:hover .chev:not(.disclose) {
    color: var(--text-muted);
    transform: translateX(2px);
  }
  .disclose {
    transform: rotate(90deg);
    transition: transform 0.15s ease;
  }
  .disclose.open {
    transform: rotate(-90deg);
  }
  /* Icon chips: soft accent-tinted circles. */
  .chip {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    flex-shrink: 0;
    border-radius: 10px;
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent-hover);
  }
  .appearance-chip {
    width: 34px;
    height: 34px;
    flex-shrink: 0;
    border-radius: 10px;
    border: 1px solid var(--border);
    background: linear-gradient(135deg, var(--accent) 0% 50%, var(--bg-3) 50% 100%);
  }

  /* Connection card: header row + editable body (not a nav row). */
  /* Display state: the address as a copyable chip with quiet icon actions. */
  /* The field answers back while you type: accent ring when the address
     parses, warm hint when it doesn't. */

  /* Switches: same mechanism, smoother travel + a soft accent glow when on. */

  /* Recovery phrase: warning-tinted card + disclosure. */
  .card.warn {
    border-color: color-mix(in srgb, var(--danger) 30%, var(--border));
    background: color-mix(in srgb, var(--danger) 4%, var(--bg-0));
  }
  .warn-chip {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    color: var(--danger-text);
  }
  .phrase-body {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 0 14px 14px;
  }
  .phrase {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 5px;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 10px;
  }
  .word {
    display: flex;
    align-items: baseline;
    gap: 5px;
    font-family: ui-monospace, monospace;
    font-size: var(--fs-compact);
  }
  /* Which word this is, not decoration — you read it to check word 17. */
  .num {
    color: var(--text-muted);
    font-size: var(--fs-tiny);
    width: 16px;
    text-align: right;
  }
  .phrase-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }
  .small-btn {
    flex-shrink: 0;
    font-size: var(--fs-compact);
    padding: 5px 14px;
  }
  .small-btn.done {
    color: var(--ok-text);
    border-color: var(--ok);
  }

  .signout {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    margin-top: 2px;
    padding: 10px 14px;
    font-size: var(--fs-ui);
    font-weight: 600;
    background: transparent;
    border: 1px solid color-mix(in srgb, var(--danger) 35%, transparent);
    border-radius: var(--radius-md);
    color: var(--danger-text);
  }
  .signout:hover {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
  }

  /* Phone: rows get a touch more height; the word grid drops to two columns
     so long mnemonic words never collide; sign-out goes full-width. */
  @media (pointer: coarse), (max-width: 768px) {
    .row {
      min-height: 56px;
    }
    /* The type tokens already grow on a phone, so the hand-tuned overrides that
       used to sit here (.row-sub 12px, .word 13px) were only holding them back. */
    .phrase {
      grid-template-columns: repeat(2, 1fr);
      gap: 7px;
    }
    .signout {
      width: 100%;
      min-height: 48px;
    }
    /* Side by side, the button claimed 135px of a 320px row and the copy broke
       three ways while every other row in the list gets the full width. */
    .upd-head {
      flex-wrap: wrap;
    }
    .upd-btn {
      flex: 1 0 100%;
      min-height: 44px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .disclose,
    .chev:not(.disclose) {
      transition: none;
    }
    .row:hover .chev:not(.disclose) {
      transform: none;
    }
  }
</style>
