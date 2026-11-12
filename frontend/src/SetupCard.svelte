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
  import Nudge from "./Nudge.svelte";

  // hidden: another nudge holds the one slot right now; the checklist keeps
  // watching (its steps tick from the feed) and draws nothing. wants: whether
  // it has something to say, for the queue that decides who holds the slot.
  let { hidden = false, wants = $bindable(false) } = $props();
  let open = $state(false);
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
  $effect(() => {
    wants = armed && (left > 0 || celebrating);
  });
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

<!-- The owner's checklist is a nudge (§5.14): one row in the sidebar's nudge
     slot, never a card pinned over the feed. "Show steps" opens the four
     steps under it, in the sidebar. The row retires itself once the last step
     ticks — after four seconds of saying so. ✕ is "hide", because there is
     nothing here to fail at. -->
{#if armed && (left > 0 || celebrating) && !hidden}
  <div class="setup" class:won={celebrating} role="region" aria-label="Set up this guild">
    <Nudge
      icon={celebrating ? "check" : "spark"}
      action={celebrating ? "" : open ? "Hide steps" : "Show steps"}
      onAction={() => (open = !open)}
      dismissLabel="Hide the setup steps"
      onDismiss={dismiss}
    >
      {#if celebrating}
        <strong>All set.</strong> <bdi>{g.name}</bdi> is ready for people.
      {:else}
        <strong>Set up <bdi>{g.name}</bdi></strong> · {done} of {steps.length} done
      {/if}
    </Nudge>
    {#if open && !celebrating}
      <ul>
        {#each steps as s (s.id)}
          <li class:done={s.done}>
            {#if s.done}
              <span class="row static">
                <span class="tick"><Icon name="check" size={12} /></span>
                <span class="lbl">{s.label}</span>
              </span>
            {:else}
              <button class="row" onclick={s.go}>
                <span class="tick"><Icon name={s.icon} size={13} /></span>
                <span class="lbl">{s.label}</span>
              </button>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}

<style>
  .setup {
    padding: var(--sp-1) 0 6px;
  }
  ul {
    list-style: none;
    margin: var(--sp-1) 0 0;
    padding: 0;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 32px;
    padding: 0 var(--sp-2) 0 10px;
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--fs-ui);
    text-align: start;
  }
  @media (pointer: fine) {
    button.row:hover {
      background: var(--bg-3);
    }
  }
  .row:focus-visible {
    outline: var(--focus-ring);
    outline-offset: -2px;
  }
  .tick {
    display: grid;
    place-items: center;
    width: 18px;
    color: var(--text-muted);
  }
  .done .row {
    color: var(--text-faint);
  }
  .done .lbl {
    text-decoration: line-through;
  }
  @media (max-width: 768px) {
    .row {
      min-height: var(--tap-min);
    }
  }
</style>
