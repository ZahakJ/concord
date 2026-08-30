<script>
  // The first sixty seconds of owning a guild.
  //
  // A brand-new guild used to be inert: one #general, one member, an empty
  // feed and no suggestion of what to do next. The owner's next click was a
  // guess. This is four rows, each deep-linking to the panel that does the
  // thing, each ticking itself off from live state.
  //
  // ARMED AT CREATION, not inferred. `armGuildSetup` writes a flag when THIS
  // device creates a guild, and the card exists only for guilds carrying it.
  // The alternative — "show it while the guild looks new" — would put a setup
  // checklist on a three-year-old community that never bothered with an icon,
  // and would put it there on every device its owner ever signs into. Per
  // guild, per device, exactly as the flag is stored.
  import { onDestroy } from "svelte";
  import Icon from "./Icon.svelte";
  import { S, activeGuild, flash, openGuildHub } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import {
    dismissGuildSetup,
    isGuildSetupArmed,
    setupWelcomeDone,
    markSetupWelcome,
    setupInviteDone,
    markSetupInvite,
  } from "./lib/setup.js";

  const g = $derived(activeGuild());

  // $state so a latch or a dismissal re-renders — localStorage is not
  // reactive, so the components that read it need a version counter to
  // depend on.
  let dismissed = $state(0);
  let latched = $state(0);

  const armed = $derived(!!g && !dismissed && g.kind !== "dm" && g.isOwner && isGuildSetupArmed(g.id));
  const welcomeDone = $derived.by(() => {
    latched; // the dependency that makes the latch below visible here
    return !!g && setupWelcomeDone(g.id);
  });
  const inviteDone = $derived.by(() => {
    latched;
    return !!g && setupInviteDone(g.id);
  });

  // The welcome step latches on a message THIS account wrote in the channel on
  // screen. Counting the guild's messages was the obvious version and it was
  // wrong: a templated guild is born holding one "created this channel" system
  // row per channel, so the box ticked itself before anything was said.
  $effect(() => {
    const id = g?.id;
    if (!id || !armed || welcomeDone) return;
    const mine = S.messages.some((m) => m.kind === "" && m.sender === S.identity?.fingerprint);
    if (mine && markSetupWelcome(id)) {
      latched++;
      S.setupRev++;
    }
  });

  // The invite step latches the same way. Counting live members was honest in
  // the moment and a bug over time: kick someone and the box unticks, let
  // them back in and it ticks again. Once somebody else has been here, the
  // errand is done.
  $effect(() => {
    const id = g?.id;
    if (!id || !armed || inviteDone) return;
    if (S.members.some((m) => !m.isSelf) && markSetupInvite(id)) {
      latched++;
      S.setupRev++;
    }
  });

  // An import is real activity. The card sitting over 1,981 archived messages
  // asking you to "get started" is the first screen of a community that
  // already has a past.
  $effect(() => {
    if (!armed || !g) return;
    if (S.chronicle?.id) {
      dismissGuildSetup(g.id);
      dismissed++;
      S.setupRev++;
    }
  });

  const steps = $derived.by(() => {
    if (!g) return [];
    return [
      {
        id: "icon",
        label: "Give it an icon",
        sub: "Two letters in the rail is a placeholder, not a face.",
        icon: "image",
        done: !!g.icon,
        go: () => openGuildHub(),
      },
      {
        id: "channels",
        label: "Make your channels",
        sub: "One room is a chat. A few rooms is a community.",
        icon: "hash",
        // Whatever CreateGuild seeded plus at least one more. Threads and
        // forum posts are channels too, so top-level rows only.
        done: (g.channels || []).filter((c) => !c.parent).length > 1,
        go: () => (S.modal = { kind: "channel" }),
      },
      {
        id: "welcome",
        label: "Write a welcome message",
        sub: "The first thing anyone who arrives will read.",
        icon: "edit",
        done: welcomeDone,
        go: () => document.querySelector("textarea.draft")?.focus(),
      },
      {
        id: "invite",
        label: "Invite people",
        sub: "Share a code with the people you want in here.",
        icon: "members",
        // Latched: see the effect above. Pending invitees still count as the
        // first observation — the code has left the building.
        done: inviteDone,
        go: invite,
      },
    ];
  });

  const left = $derived(steps.filter((s) => !s.done).length);
  const done = $derived(steps.length - left);

  // Finishing should feel like finishing.
  //
  // The card used to vanish the instant the last row ticked — correct in that a
  // checklist with nothing on it is clutter, and a small theft in that the one
  // moment worth acknowledging was the one moment nothing was said. So it holds
  // for four seconds on a filled bar and a sentence, then retires itself for
  // good.
  //
  // Only on a transition WATCHED from incomplete to complete. Opening a guild
  // whose steps were all done days ago must retire the card silently rather
  // than congratulate somebody for something they do not remember doing.
  // `sawIncomplete` and `winTimer` are deliberately NOT $state. An effect that
  // reads the flag it also writes re-runs itself, and a cleanup that clears the
  // retirement timer on that re-run means the timer never fires: the card
  // congratulated correctly and then sat there for ever. Measured — five
  // seconds after the last step, still on screen.
  let sawIncomplete = false;
  let winTimer;
  let celebrating = $state(false);
  $effect(() => {
    if (!armed) return;
    if (left > 0) {
      sawIncomplete = true;
      return;
    }
    if (!sawIncomplete) {
      // Finished, but not in front of us — this guild was set up in an earlier
      // session. Retire the record quietly rather than leaving an armed flag in
      // storage for a card that can never draw again.
      dismissGuildSetup(g.id);
      dismissed++;
      S.setupRev++;
      return;
    }
    if (winTimer) return;
    celebrating = true;
    winTimer = setTimeout(() => {
      dismissGuildSetup(g.id);
      dismissed++;
      S.setupRev++;
    }, 4000);
  });
  onDestroy(() => clearTimeout(winTimer));

  async function invite() {
    try {
      S.modal = { kind: "invite", code: await api.inviteCode(g.id) };
    } catch (err) {
      flash(err);
    }
  }

  function dismiss() {
    dismissGuildSetup(g.id);
    dismissed++;
    S.setupRev++;
  }
</script>

<!-- The card retires itself once the last row ticks — after four seconds of
     saying so. A checklist with nothing left on it is clutter; a checklist that
     disappears mid-click is a reward withheld. The ✕ is for people who want out
     early, and it says "hide", because there is nothing here to fail at. -->
{#if armed && (left > 0 || celebrating)}
  <div class="setup" class:won={celebrating} role="region" aria-label="Set up this guild">
    <div class="head">
      <span class="hd-text">
        <strong>{celebrating ? "All set." : "Get started"}</strong>
        <span class="muted">
          {celebrating ? `${g.name} is ready for people.` : `${done} of ${steps.length} done`}
        </span>
      </span>
      <button class="x" onclick={dismiss} aria-label="Hide the setup steps" title="Hide">
        <Icon name="close" size={14} />
      </button>
    </div>
    <!-- Progress, as a thing that moves. A count on its own ("3 of 4 left")
         is arithmetic; a bar that grows under it is the errand shrinking. -->
    <div class="bar" role="presentation">
      <span style="width:{(done / steps.length) * 100}%"></span>
    </div>
    <ul>
      {#each steps as s (s.id)}
        <li class:done={s.done}>
          {#if s.done}
            <span class="row static">
              <span class="tick"><Icon name="check" size={12} /></span>
              <span class="txt">
                <span class="lbl">{s.label}</span>
                <span class="sub">{s.sub}</span>
              </span>
            </span>
          {:else}
            <button class="row" onclick={s.go}>
              <span class="tick"><Icon name={s.icon} size={13} /></span>
              <span class="txt">
                <span class="lbl">{s.label}</span>
                <span class="sub">{s.sub}</span>
              </span>
              <span class="chev" aria-hidden="true">›</span>
            </button>
          {/if}
        </li>
      {/each}
    </ul>
  </div>
{/if}

<style>
  .setup {
    /* Capped and centred. It used to run the full width of the pane — 1200px of
       card holding four short rows and three identical buttons, sitting above an
       empty-channel hero that was also asking for attention. A card that is the
       width of a card competes with nothing. */
    width: min(560px, 100%);
    margin: var(--sp-4) auto 0;
    border: 1px solid color-mix(in srgb, var(--accent) 32%, var(--border));
    background: linear-gradient(
      160deg,
      color-mix(in srgb, var(--accent) 9%, var(--bg-1)),
      var(--bg-1) 62%
    );
    border-radius: var(--radius-lg);
    overflow: hidden;
    box-shadow: var(--shadow-pop);
    animation: setup-in 0.34s var(--ease-spring) both;
  }
  .head {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: 13px var(--sp-3) 10px;
  }
  .hd-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .hd-text strong {
    font-size: var(--fs-body);
    font-weight: 700;
    line-height: 1.2;
  }
  .hd-text .muted {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .x {
    flex: none;
    width: 26px;
    height: 26px;
    display: grid;
    place-items: center;
    background: transparent;
    color: var(--text-faint);
    border-radius: var(--radius-sm);
  }
  .x:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  /* ---- progress ----------------------------------------------------------- */
  .bar {
    height: 4px;
    margin: 0 var(--sp-3) 12px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--border) 70%, transparent);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    border-radius: inherit;
    background: linear-gradient(90deg, var(--accent), var(--accent-hover));
    box-shadow: var(--accent-glow);
    /* A checklist filling up is a small reward, and it is watched. Travel,
       not a state change — off the token scale on purpose. */
    transition: width 0.45s var(--ease-spring);
  }
  /* Finished: the bar is full, the frame warms, and the whole card lifts once. */
  .setup.won {
    border-color: var(--accent);
    animation: setup-won 0.5s var(--ease-spring) both;
  }
  @keyframes setup-won {
    0% {
      transform: scale(1);
    }
    45% {
      transform: scale(1.012);
    }
    100% {
      transform: scale(1);
    }
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0 var(--sp-2) var(--sp-2);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  /* The whole row is the target. Three identically-labelled "Do it" buttons in
     a column said the same thing three times and made the rows themselves inert
     — you could not click the sentence describing what you wanted to do. */
  .row {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    width: 100%;
    padding: 9px 10px;
    border-radius: var(--radius-md);
    background: transparent;
    color: var(--text);
    text-align: left;
    transition:
      background var(--dur-quick) ease,
      transform var(--dur-quick) var(--ease-out);
  }
  .row.static {
    cursor: default;
  }
  button.row:hover {
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    transform: translateX(2px);
  }
  .tick {
    flex: none;
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: var(--bg-3);
    color: var(--text-muted);
    transition:
      background var(--dur-quick) ease,
      color var(--dur-quick) ease;
  }
  button.row:hover .tick {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  /* Done is a filled tick, not a line through the words. A checklist that
     crosses out what you achieved reads as a list of chores. */
  li.done .tick {
    background: var(--accent);
    color: var(--accent-fg);
    animation: tick-in 0.3s var(--ease-spring) both;
  }
  @keyframes tick-in {
    from {
      transform: scale(0.4);
    }
  }
  .txt {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .lbl {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  li.done .lbl {
    color: var(--text-muted);
    font-weight: 500;
  }
  .sub {
    font-size: var(--fs-small);
    line-height: 1.4;
    color: var(--text-muted);
  }
  .chev {
    flex: none;
    font-size: 19px;
    line-height: 1;
    color: var(--text-faint);
    transition: transform var(--dur-quick) ease;
  }
  button.row:hover .chev {
    color: var(--accent-hover);
    transform: translateX(2px);
  }
  @keyframes setup-in {
    from {
      opacity: 0;
      transform: translateY(-8px);
    }
  }
  @media (max-width: 768px) {
    .setup {
      margin: var(--sp-3) var(--sp-3) 0;
      width: auto;
    }
    .row {
      min-height: 52px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .setup,
    .setup.won,
    .row,
    .chev,
    li.done .tick {
      animation: none;
      transition: none;
    }
    .bar span {
      transition: none;
    }
  }
</style>
