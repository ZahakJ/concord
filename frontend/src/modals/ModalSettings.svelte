<script>
  // Two surfaces, one file.
  //
  // On a phone this is the settings LIST — the grouped table from
  // lib/settingsnav.js, drilled into one page at a time, which is the right
  // shape for a thumb on a 390px sheet.
  //
  // On a desktop it is the ACCOUNT page: the rail beside it already shows every
  // other destination, so a second list of the same eight doors would be a menu
  // in front of a menu. What is left is the thing that has no other home — the
  // account itself: who you are on this machine, the words that ARE that
  // account, the copy you keep, the version you are running, and the way out.
  import SettingsShell from "./SettingsShell.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { onMount, onDestroy } from "svelte";
  import { api } from "../lib/api.js";
  import { S, flash, openPanel, setPref } from "../lib/state.svelte.js";
  import { SETTINGS_GROUPS } from "../lib/settingsnav.js";
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

<SettingsShell title={S.isMobile ? "Settings" : "Account"} here="settings" {onClose}>
  <!-- WHO. It leads on both surfaces: on the desktop this page has no other way
       of saying which account it is about, and on a phone the list under it is
       all the ways of changing exactly this. On a phone the whole card is the
       way into Your profile; on the desktop the rail already offers that, so
       the card carries one quiet button instead. -->
  <svelte:element
    this={S.isMobile ? "button" : "div"}
    class="who"
    onclick={S.isMobile ? () => openPanel("profile", "settings") : undefined}
  >
    <Avatar
      name={S.displayName || S.identity.displayName || "You"}
      emoji={S.identity.emoji}
      color={S.identity.color}
      image={S.identity.avatar}
      size={52}
    />
    <div class="who-text">
      <span class="who-name">{S.displayName || S.identity.displayName || "Your account"}</span>
      <span class="who-sub">
        {#if S.identity.status}{S.identity.status}{:else}This account lives on this device and nowhere else.{/if}
      </span>
    </div>
    {#if S.isMobile}
      <span class="chev">›</span>
    {:else}
      <button class="who-edit" onclick={() => openPanel("profile", "settings")}>Edit profile</button>
    {/if}
  </svelte:element>

  <!-- PHONE: the whole table, grouped, drilled one page at a time. There is no
       room for a rail on a 390px sheet and no need for one — a sheet you can
       flick through IS the overview. Only "Account" is left out, because it is
       the page you are already standing on. -->
  {#if S.isMobile}
    {#each SETTINGS_GROUPS as g (g.label)}
      {@const items = g.items.filter((i) => i.kind !== "settings")}
      {#if items.length}
        <SettingGroup label={g.label}>
          {#each items as it (it.kind)}
            <SettingRow icon={it.icon} title={it.title} sub={it.sub} to={it.kind} from="settings" />
          {/each}
        </SettingGroup>
      {/if}
    {/each}
  {/if}

  <!-- KEEPING IT. The two things that decide whether this account survives the
       machine it is on, said in that order and nowhere else in the app. -->
  <SettingGroup
    label="Your safety net"
    info="There is no server holding a copy of any of this. These two are the whole of your safety net."
  >
    <SettingRow
      icon="download"
      title="Backup & restore"
      sub="History lives only on your devices. This is the copy you keep."
      onclick={() => openPanel("backup")}
    />
    <SettingRow
      icon="alert"
      title="Recovery phrase"
      sub={phraseOpen
        ? "On screen now — anyone who reads them can become you."
        : "24 words that ARE your account. Reveal them, write them down."}
      onclick={togglePhrase}
    >
      <span class="reveal" class:open={phraseOpen}>{phraseOpen ? "Hide" : "Reveal"}</span>
    </SettingRow>
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
  </SettingGroup>

  <!-- THIS INSTALL. Version and update are one statement, not two sections. -->
  <SettingGroup label="This install">
    <div class="upd">
      <div class="upd-head">
        <span
          class="chip"
          class:spin-chip={upd.phase === "downloading" || upd.phase === "verifying"}
        >
          <Icon name="download" size={16} />
        </span>
        <span class="row-text">
          <span class="row-title">Concord {appVersion === "dev" ? "dev build" : appVersion || "…"}</span>
          <span class="row-sub">
            {#if !canUpdate}
              Updates arrive through the store this build came from.
            {:else if upd.phase === "downloading"}
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
        {#if canUpdate}
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
        {:else if appVersion}
          <button class="upd-btn ghosted" onclick={copyVersion}>
            {copiedVersion ? "Copied ✓" : "Copy version"}
          </button>
        {/if}
      </div>
      <!-- The bar is always in the layout, empty at rest: a card that grows a
           row when a download starts pushes everything under it down the page
           at the exact moment somebody is watching a number. -->
      <div class="upd-bar" class:live={upd.phase === "downloading" || upd.phase === "verifying"}>
        <span style="width:{upd.phase === 'downloading' || upd.phase === 'verifying' ? upd.percent : 0}%"></span>
      </div>
    </div>
  </SettingGroup>

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
</SettingsShell>

<style>
  /* ---- who this account is ------------------------------------------------
     The one thing on this page that is not a control. It leads because every
     other row here is about protecting or replacing exactly this. */
  .who {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-3) var(--sp-3) var(--sp-2);
    border-radius: var(--radius-md);
    background: linear-gradient(
      120deg,
      color-mix(in srgb, var(--accent) 10%, var(--bg-1)),
      var(--bg-1) 62%
    );
    border: 1px solid var(--border);
    flex: none;
  }
  .who-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .who-name {
    font-size: var(--fs-title);
    font-weight: 700;
    line-height: 1.15;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .who-sub {
    font-size: var(--fs-small);
    line-height: 1.45;
    color: var(--text-muted);
    unicode-bidi: plaintext;
  }
  /* On a phone the card is a button. Everything a button brings with it —
     centred text, the app's default fill — has to be said away. */
  button.who {
    width: 100%;
    text-align: left;
    cursor: pointer;
    /* app.css fills a bare button with the accent and paints its label
       --accent-fg; on a card with its own ground that ink is unreadable. */
    color: var(--text);
  }
  .chev {
    flex: none;
    font-size: 20px;
    line-height: 1;
    color: var(--text-faint);
  }
  .who-edit {
    flex: none;
    font-size: var(--fs-compact);
    padding: 7px 14px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text);
    border: 1px solid var(--border);
  }
  .who-edit:hover {
    background: var(--bg-2);
    border-color: var(--accent);
  }

  /* The recovery row's trailing control. A chevron would promise another page;
     this row opens in place, and the word says which way it is about to go. */
  .reveal {
    font-size: var(--fs-compact);
    font-weight: 650;
    color: var(--accent-hover);
    white-space: nowrap;
  }
  .reveal.open {
    color: var(--text-muted);
  }

  .phrase-body {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 0 14px 14px;
  }
  .phrase {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
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
  .row-sub {
    font-size: var(--fs-small);
    line-height: 1.45;
    color: var(--text-muted);
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
  .small-btn {
    flex-shrink: 0;
    font-size: var(--fs-compact);
    padding: 5px 14px;
  }
  .small-btn.done {
    color: var(--ok-text);
    border-color: var(--ok);
  }

  /* ---- this install -------------------------------------------------------- */
  .upd {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px 14px;
  }
  .upd-head {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .chip {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    flex-shrink: 0;
    border-radius: var(--radius-md);
    background: var(--accent-soft);
    color: var(--accent-hover);
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
    background: var(--accent-soft);
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
  /* Reserved, not conditional: see the note at the markup. At rest it is a
     hairline the width of the card, which reads as the floor of the section. */
  .upd-bar {
    height: 6px;
    border-radius: 999px;
    background: transparent;
    overflow: hidden;
    transition: background var(--dur-quick) ease;
  }
  .upd-bar.live {
    background: var(--bg-3);
  }
  .upd-bar span {
    display: block;
    height: 100%;
    border-radius: inherit;
    background: linear-gradient(90deg, var(--accent), var(--accent-hover));
    transition: width 0.3s ease;
  }

  .signout {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    margin-top: auto;
    padding: 10px 14px;
    font-size: var(--fs-ui);
    font-weight: 600;
    background: transparent;
    border: 1px solid color-mix(in srgb, var(--danger) 35%, transparent);
    border-radius: var(--radius-md);
    color: var(--danger-text);
    flex: none;
  }
  .signout:hover {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
  }

  /* Phone: the word grid drops to two columns so long mnemonic words never
     collide, and sign-out goes full-width. */
  @media (pointer: coarse), (max-width: 768px) {
    .phrase {
      grid-template-columns: repeat(2, 1fr);
      gap: 7px;
    }
    .signout {
      width: 100%;
      min-height: 48px;
    }
    .upd-head {
      flex-wrap: wrap;
    }
    .upd-btn {
      flex: 1 0 100%;
      min-height: 44px;
    }
  }
  /* A narrow desktop pane: three columns of mnemonic still fit, four do not. */
  @media (min-width: 769px) and (max-width: 1050px) {
    .phrase {
      grid-template-columns: repeat(3, 1fr);
    }
  }
</style>
