<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { onMount, onDestroy } from "svelte";
  import { api } from "../lib/api.js";
  import { soundsEnabled, setSoundsEnabled } from "../lib/sounds.js";
  import { bioEnrolled } from "../lib/biometric.js";
  import { S, setPref, flash } from "../lib/state.svelte.js";

  let { onClose, onSaved } = $props();
  let bootstrap = $state("");
  let sounds = $state(soundsEnabled());
  let phrase = $state("");
  let copiedPhrase = $state(false);
  let phraseOpen = $state(false);

  function toggleSounds() {
    sounds = !sounds;
    setSoundsEnabled(sounds);
  }

  // "Stay connected" defaults on; toggling flips the pref and the Android
  // foreground service that keeps the P2P node alive in the background.
  let stayConnected = $state(S.prefs.stayConnected !== false);
  function toggleStayConnected() {
    stayConnected = !stayConnected;
    setPref("stayConnected", stayConnected);
    const core = window.Capacitor?.Plugins?.ConcordCore;
    if (stayConnected) core?.startBackground?.().catch(() => {});
    else core?.stopBackground?.().catch(() => {});
  }

  let richPresence = $state(false);
  // Linux (and the Linux web build) can read MPRIS now-playing today.
  const richPresenceSupported = /linux|x11/i.test(navigator.userAgent);
  async function toggleRichPresence() {
    richPresence = !richPresence;
    try {
      await api.setRichPresence(richPresence);
    } catch (err) {
      richPresence = !richPresence; // revert on failure
      flash(err);
    }
  }

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
    }
    phraseOpen = !phraseOpen;
  }
  function copyPhrase() {
    navigator.clipboard?.writeText(phrase);
    copiedPhrase = true;
    setTimeout(() => (copiedPhrase = false), 1600);
  }

  // App version for the subtle footer ("dev" for unstamped local builds).
  let appVersion = $state("");
  let copiedVersion = $state(false);
  function copyVersion() {
    navigator.clipboard?.writeText(`Concord ${appVersion}`);
    copiedVersion = true;
    setTimeout(() => (copiedVersion = false), 1400);
  }

  // ---- software update (full in-place self-update) ----
  const isMobileApp = !!window.Capacitor; // app stores own mobile updates
  let updInfo = $state(null); // CheckForUpdate view, once the user checks
  let checking = $state(false);
  let canSelf = $state(false); // this install can swap its own binary
  let upd = $state({ phase: "idle", percent: 0 }); // backend UpdateProgress
  let restarting = $state(false);
  let pollTimer;

  async function checkNow() {
    checking = true;
    try {
      updInfo = await api.checkForUpdate();
    } catch (err) {
      flash(err);
    } finally {
      checking = false;
    }
  }
  async function updateNow() {
    try {
      await api.applyUpdate();
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
  // Restart the backend into the new binary, wait for it to come back, reload.
  async function restartNow() {
    restarting = true;
    try {
      await api.restartApp();
    } catch {
      /* the connection may drop mid-response — that IS the restart */
    }
    const t0 = Date.now();
    const timer = setInterval(async () => {
      try {
        await api.session();
        clearInterval(timer);
        location.reload();
      } catch {
        if (Date.now() - t0 > 30000) {
          clearInterval(timer);
          location.reload();
        }
      }
    }, 700);
  }

  onMount(async () => {
    try {
      bootstrap = ((await api.getBootstrap()) || []).join("\n");
    } catch {
      /* ignore */
    }
    try {
      richPresence = await api.richPresenceEnabled();
    } catch {
      /* ignore */
    }
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
  });

  onDestroy(() => clearInterval(pollTimer));

  // Rendezvous is display-first: the address shows as a copyable chip; editing
  // (a once-ever action for self-hosters) hides behind the pencil.
  let editingBootstrap = $state(false);
  let bootstrapDraft = $state("");
  let copiedAddr = $state(false);

  function startEditBootstrap() {
    bootstrapDraft = bootstrap;
    editingBootstrap = true;
  }
  function copyAddr() {
    navigator.clipboard?.writeText(bootstrap);
    copiedAddr = true;
    setTimeout(() => (copiedAddr = false), 1600);
  }
  async function saveBootstrap() {
    try {
      await api.setBootstrapLive(bootstrapDraft);
      bootstrap = bootstrapDraft;
      editingBootstrap = false;
      onSaved?.(); // parent toasts the confirmation
    } catch (err) {
      flash(err);
    }
  }

  // Live shape-check while editing: every non-blank line should be a
  // /…/p2p/<PeerID> multiaddr.
  const draftLines = $derived(bootstrapDraft.split("\n").filter((l) => l.trim()));
  const draftOk = $derived(
    draftLines.length > 0 && draftLines.every((l) => l.trim().startsWith("/") && l.includes("/p2p/")),
  );
  const draftBad = $derived(draftLines.length > 0 && !draftOk);
</script>

<Modal title="Settings" wide {onClose}>
  <!-- ACCOUNT -->
  <section class="grp">
    <div class="sec-label">Account</div>
    <div class="card">
      <button class="row" onclick={() => (S.modal = { kind: "profile" })}>
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
      <button class="row" onclick={() => (S.modal = { kind: "appearance" })}>
        <span class="appearance-chip" aria-hidden="true"></span>
        <span class="row-text">
          <span class="row-title">Appearance</span>
          <span class="row-sub">Theme, accent color &amp; message density</span>
        </span>
        <span class="chev">›</span>
      </button>
      <button class="row" onclick={() => (S.modal = { kind: "linkDevice" })}>
        <span class="chip"><Icon name="devices" size={17} /></span>
        <span class="row-text">
          <span class="row-title">Link a device</span>
          <span class="row-sub">Add your phone or another computer</span>
        </span>
        <span class="chev">›</span>
      </button>
    </div>
  </section>

  <!-- SOFTWARE UPDATE (not on mobile — app stores own updates there) -->
  {#if !isMobileApp}
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

  <!-- CONNECTION -->
  <section class="grp">
    <div class="sec-label">Connection</div>
    <div class="card pad">
      <div class="conn-head">
        <span class="chip"><Icon name="link" size={16} /></span>
        <span class="row-text">
          <span class="row-title">Rendezvous server</span>
          <span class="row-sub">
            The tiny relay that lets friends on other networks find you. Only
            needed if <em>you</em> host it — friends get it from your invite code.
          </span>
        </span>
      </div>
      {#if editingBootstrap}
        <div class="code-wrap" class:ok={draftOk} class:bad={draftBad}>
          <textarea
            class="code-box"
            rows="3"
            placeholder="/dns/your-app.fly.dev/tcp/4001/p2p/12D3Koo…"
            bind:value={bootstrapDraft}
          ></textarea>
          {#if draftOk}
            <span class="code-state"><Icon name="check" size={13} /> address looks good</span>
          {:else if draftBad}
            <span class="code-state">should start with /dns or /ip4 and contain /p2p/…</span>
          {/if}
        </div>
        <div class="conn-foot">
          <span class="row-sub">Blank = same-Wi-Fi only. Applies live to new connections.</span>
          <button class="ghost cancel-btn" onclick={() => (editingBootstrap = false)}>Cancel</button>
          <button class="save-btn" disabled={draftBad} onclick={saveBootstrap}>Save</button>
        </div>
      {:else if bootstrap.trim()}
        <div class="addr-row">
          <code class="addr" title={bootstrap}>{bootstrap.trim()}</code>
          <button class="addr-act" onclick={copyAddr} aria-label="Copy address">
            {#if copiedAddr}<Icon name="check" size={15} />{:else}<Icon name="copy" size={15} />{/if}
          </button>
          <button class="addr-act" onclick={startEditBootstrap} aria-label="Edit address">
            <Icon name="edit" size={15} />
          </button>
        </div>
        <span class="row-sub">Friends get this automatically from your invite codes.</span>
      {:else}
        <div class="addr-row empty">
          <span class="row-sub">Not set — you can only reach friends on the same Wi-Fi.</span>
          <button class="save-btn" onclick={startEditBootstrap}>Set address</button>
        </div>
      {/if}
    </div>
  </section>

  <!-- PREFERENCES -->
  <section class="grp">
    <div class="sec-label">Preferences</div>
    <div class="card">
      {#if S.isMobile}
        <button class="row" onclick={toggleStayConnected} role="switch" aria-checked={stayConnected}>
          <span class="chip"><Icon name="bell" size={16} /></span>
          <span class="row-text">
            <span class="row-title">Stay connected</span>
            <span class="row-sub">Receive messages in the background (quiet notification)</span>
          </span>
          <span class="switch" class:on={stayConnected}><span class="knob"></span></span>
        </button>
      {/if}
      {#if S.isMobile}
        <!-- Always visible on mobile so the feature is discoverable; disabled
             (with a hint) until biometric unlock is enrolled. -->
        <button
          class="row"
          onclick={() => bioEnrolled() && setPref("appLock", !(S.prefs.appLock === true))}
          role="switch"
          aria-checked={bioEnrolled() && S.prefs.appLock === true}
          disabled={!bioEnrolled()}
        >
          <span class="chip"><Icon name="lock" size={16} /></span>
          <span class="row-text">
            <span class="row-title">App lock</span>
            <span class="row-sub">
              {bioEnrolled()
                ? "Require fingerprint when opening the app"
                : "Enable biometric unlock first"}
            </span>
          </span>
          <span class="switch" class:on={bioEnrolled() && S.prefs.appLock === true}
            ><span class="knob"></span></span
          >
        </button>
      {/if}
      <button class="row" onclick={toggleSounds} role="switch" aria-checked={sounds}>
        <span class="chip"><Icon name="speaker" size={16} /></span>
        <span class="row-text">
          <span class="row-title">Sounds</span>
          <span class="row-sub">Voice join/leave chimes and @mention pings</span>
        </span>
        <span class="switch" class:on={sounds}><span class="knob"></span></span>
      </button>
      <button
        class="row"
        onclick={() => setPref("linkPreviews", !S.prefs.linkPreviews)}
        role="switch"
        aria-checked={S.prefs.linkPreviews}
      >
        <span class="chip"><Icon name="screen" size={16} /></span>
        <span class="row-text">
          <span class="row-title">Link previews</span>
          <span class="row-sub">
            Off by default: loading a preview reveals your IP to the link's
            host. Turn on only among people you trust.
          </span>
        </span>
        <span class="switch" class:on={S.prefs.linkPreviews}><span class="knob"></span></span>
      </button>
      <button class="row" onclick={toggleRichPresence} role="switch" aria-checked={richPresence}>
        <span class="chip"><Icon name="spark" size={16} /></span>
        <span class="row-text">
          <span class="row-title">Rich presence</span>
          <span class="row-sub">
            Show what you're listening to as your status ("🎵 Artist — Title"),
            read on-device from your media player.
            {richPresenceSupported ? "" : "(Not supported on this platform yet.)"}
          </span>
        </span>
        <span class="switch" class:on={richPresence}><span class="knob"></span></span>
      </button>
    </div>
  </section>

  <!-- SECURITY -->
  <section class="grp">
    <div class="sec-label">Security</div>
    <div class="card warn">
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
    font-size: 12.5px;
    padding: 7px 14px;
    border-radius: 999px;
    border: none;
    background: var(--accent);
    color: #fff;
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
    color: var(--accent);
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
    font-size: 11px;
    letter-spacing: 0.03em;
    color: var(--text-faint);
    cursor: pointer;
    border-radius: 999px;
    transition: color 0.15s ease;
  }
  .about:hover {
    color: var(--text-muted);
  }
  .sec-label {
    font-size: 11px;
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
    font-size: 14px;
    font-weight: 600;
    line-height: 1.3;
  }
  .row-sub {
    font-size: 11.5px;
    line-height: 1.45;
    color: var(--text-muted);
  }
  .chev {
    flex-shrink: 0;
    font-size: 20px;
    line-height: 1;
    color: var(--text-faint);
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
    color: var(--accent);
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
  .card.pad {
    padding: 12px 14px;
    gap: 10px;
  }
  .conn-head {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .conn-head .chip {
    margin-top: 2px;
  }
  /* Display state: the address as a copyable chip with quiet icon actions. */
  .addr-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .addr {
    flex: 1;
    min-width: 0;
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 12px;
    line-height: 1.4;
    padding: 10px 12px;
    border-radius: 10px;
    background: color-mix(in srgb, var(--bg-0) 42%, var(--bg-3));
    border: 1px solid color-mix(in srgb, var(--border) 62%, transparent);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .addr-act {
    flex-shrink: 0;
    width: 36px;
    height: 36px;
    padding: 0;
    display: grid;
    place-items: center;
    border-radius: 10px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
  .addr-act:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .addr-row.empty {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }
  .cancel-btn {
    padding: 7px 14px;
    font-size: 13px;
  }
  .code-wrap {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .code-box {
    width: 100%;
    box-sizing: border-box;
    min-height: 84px;
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 12.5px;
    line-height: 1.65;
    letter-spacing: 0.01em;
    white-space: pre-wrap;
    word-break: break-all;
    resize: none; /* it's an address, not an essay */
    border-radius: 12px;
    padding: 12px 14px;
  }
  /* The field answers back while you type: accent ring when the address
     parses, warm hint when it doesn't. */
  .code-wrap.ok .code-box {
    border-color: color-mix(in srgb, var(--ok) 55%, transparent);
  }
  .code-wrap.ok .code-box:focus {
    border-color: var(--ok);
    box-shadow:
      inset 0 1px 2px rgb(0 0 0 / 0.08),
      0 0 0 3px color-mix(in srgb, var(--ok) 18%, transparent);
  }
  .code-wrap.bad .code-box {
    border-color: color-mix(in srgb, var(--danger) 45%, transparent);
  }
  .code-state {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 11.5px;
    color: var(--text-muted);
    animation: state-in 0.18s ease both;
  }
  .code-wrap.ok .code-state {
    color: var(--ok);
  }
  .code-wrap.bad .code-state {
    color: color-mix(in srgb, var(--danger) 80%, var(--text));
  }
  @keyframes state-in {
    from {
      opacity: 0;
      transform: translateY(-2px);
    }
  }
  .conn-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .conn-foot .row-sub {
    flex: 1;
    min-width: 0;
  }
  .save-btn {
    flex-shrink: 0;
    padding: 7px 18px;
    font-size: 13px;
    font-weight: 600;
  }

  /* Switches: same mechanism, smoother travel + a soft accent glow when on. */
  .switch {
    flex-shrink: 0;
    width: 40px;
    height: 24px;
    border-radius: 12px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    display: block;
    position: relative;
    transition:
      background 0.18s ease,
      border-color 0.18s ease,
      box-shadow 0.18s ease;
  }
  .switch.on {
    background: var(--accent);
    border-color: var(--accent);
    box-shadow: 0 0 10px color-mix(in srgb, var(--accent) 40%, transparent);
  }
  .knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: white;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.35);
    transition: transform 0.18s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  .switch.on .knob {
    transform: translateX(16px);
  }

  /* Recovery phrase: warning-tinted card + disclosure. */
  .card.warn {
    border-color: color-mix(in srgb, var(--danger) 30%, var(--border));
    background: color-mix(in srgb, var(--danger) 4%, var(--bg-0));
  }
  .warn-chip {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    color: var(--danger);
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
    font-size: 12px;
  }
  .num {
    color: var(--text-faint);
    font-size: 10px;
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
    font-size: 12px;
    padding: 5px 14px;
  }
  .small-btn.done {
    color: var(--ok);
    border-color: var(--ok);
  }

  .signout {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    margin-top: 2px;
    padding: 10px 14px;
    font-size: 13px;
    font-weight: 600;
    background: transparent;
    border: 1px solid color-mix(in srgb, var(--danger) 35%, transparent);
    border-radius: var(--radius-md);
    color: var(--danger);
  }
  .signout:hover {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
  }

  /* Phone: rows get a touch more height; the word grid drops to two columns
     so long mnemonic words never collide; sign-out goes full-width. */
  @media (pointer: coarse), (max-width: 700px) {
    .row {
      min-height: 56px;
    }
    .row-sub {
      font-size: 12px;
    }
    .phrase {
      grid-template-columns: repeat(2, 1fr);
      gap: 7px;
    }
    .word {
      font-size: 13px;
    }
    .conn-foot {
      flex-direction: column;
      align-items: stretch;
    }
    .save-btn {
      width: 100%;
      min-height: 44px;
    }
    .signout {
      width: 100%;
      min-height: 48px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .switch,
    .knob,
    .disclose {
      transition: none;
    }
  }
</style>
