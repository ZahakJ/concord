<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { onMount } from "svelte";
  import { api } from "../lib/api.js";
  import { soundsEnabled, setSoundsEnabled } from "../lib/sounds.js";
  import { bioEnrolled } from "../lib/biometric.js";
  import { S, setPref, flash } from "../lib/state.svelte.js";

  let { onClose, onSaved } = $props();
  let bootstrap = $state("");
  let saved = $state(false);
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
  });

  async function save() {
    try {
      await api.setBootstrapLive(bootstrap);
      saved = true;
      onSaved?.();
      setTimeout(() => onClose(), 700);
    } catch (err) {
      flash(err);
    }
  }

  // Live shape-check of the rendezvous field so the box answers back while you
  // type: every non-blank line should be a /…/p2p/<PeerID> multiaddr.
  const bootstrapLines = $derived(bootstrap.split("\n").filter((l) => l.trim()));
  const bootstrapOk = $derived(
    bootstrapLines.length > 0 && bootstrapLines.every((l) => l.trim().startsWith("/") && l.includes("/p2p/")),
  );
  const bootstrapBad = $derived(bootstrapLines.length > 0 && !bootstrapOk);
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
      <div class="code-wrap" class:ok={bootstrapOk} class:bad={bootstrapBad}>
        <textarea
          class="code-box"
          rows="3"
          placeholder="/dns/your-app.fly.dev/tcp/4001/p2p/12D3Koo…"
          bind:value={bootstrap}
        ></textarea>
        {#if bootstrapOk}
          <span class="code-state"><Icon name="check" size={13} /> address looks good</span>
        {:else if bootstrapBad}
          <span class="code-state">should start with /dns or /ip4 and contain /p2p/…</span>
        {/if}
      </div>
      <div class="conn-foot">
        <span class="row-sub">Blank = same-Wi-Fi only. Applies live to new connections.</span>
        <button class="save-btn" onclick={save}>{saved ? "Saved ✓" : "Save"}</button>
      </div>
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
</Modal>

<style>
  /* Sectioned, carded settings — the same structure reads native on both the
     desktop floating dialog and the mobile bottom sheet. */
  .grp {
    display: flex;
    flex-direction: column;
    gap: 7px;
    text-align: left;
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
    resize: vertical;
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
