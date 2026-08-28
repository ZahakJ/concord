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
  import Icon from "./Icon.svelte";
  import { S, activeGuild, flash } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import {
    dismissGuildSetup,
    isGuildSetupArmed,
    setupWelcomeDone,
    markSetupWelcome,
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

  // The welcome step latches on a message THIS account wrote in the channel on
  // screen. Counting the guild's messages was the obvious version and it was
  // wrong: a templated guild is born holding one "created this channel" system
  // row per channel, so the box ticked itself before anything was said.
  $effect(() => {
    const id = g?.id;
    if (!id || !armed || welcomeDone) return;
    const mine = S.messages.some((m) => m.kind === "" && m.sender === S.identity?.fingerprint);
    if (mine && markSetupWelcome(id)) latched++;
  });

  const steps = $derived.by(() => {
    if (!g) return [];
    return [
      {
        id: "icon",
        label: "Give it an icon",
        sub: "Two letters in the rail is a placeholder, not a face.",
        icon: "camera",
        done: !!g.icon,
        go: () => (S.modal = { kind: "guildSettings" }),
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
        // Nothing local can prove an invite was ACCEPTED by the person you
        // meant, so this ticks on the only honest signal: somebody else is in
        // the guild now. Pending invitees count — the code has left the
        // building, which is what the row asked for.
        done: S.members.some((m) => !m.isSelf),
        go: invite,
      },
    ];
  });

  const left = $derived(steps.filter((s) => !s.done).length);

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
  }
</script>

<!-- The card retires itself when the last row ticks: a checklist with nothing
     left on it is clutter, and a dismiss button is for people who want out
     early, not the reward for finishing. -->
{#if armed && left > 0}
  <div class="setup" role="region" aria-label="Set up this guild">
    <div class="head">
      <span class="hd-text">
        <strong>Get {g.name} started</strong>
        <span class="muted">{left} of {steps.length} left</span>
      </span>
      <button class="x" onclick={dismiss} aria-label="Dismiss the setup checklist">
        <Icon name="close" size={14} />
      </button>
    </div>
    <ul>
      {#each steps as s (s.id)}
        <li class:done={s.done}>
          <span class="tick" aria-hidden="true">
            {#if s.done}<Icon name="check" size={12} />{:else}<Icon name={s.icon} size={13} />{/if}
          </span>
          <span class="txt">
            <span class="lbl">{s.label}</span>
            <span class="sub">{s.sub}</span>
          </span>
          {#if !s.done}
            <button class="go" onclick={s.go}>Do it</button>
          {:else}
            <span class="did">Done</span>
          {/if}
        </li>
      {/each}
    </ul>
  </div>
{/if}

<style>
  .setup {
    margin: var(--sp-3) var(--sp-4) 0;
    border: 1px solid color-mix(in srgb, var(--accent) 35%, var(--border));
    background: color-mix(in srgb, var(--accent) 6%, var(--bg-1));
    border-radius: var(--radius-lg);
    overflow: hidden;
    animation: setup-in 0.32s var(--ease-out) both;
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 10px var(--sp-3);
    border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  }
  .hd-text {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: var(--sp-2);
    flex-wrap: wrap;
  }
  .hd-text strong {
    font-size: var(--fs-ui);
  }
  .hd-text .muted {
    font-size: var(--fs-small);
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
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: 9px var(--sp-3);
  }
  li + li {
    border-top: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
  }
  .tick {
    flex: none;
    width: 26px;
    height: 26px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: var(--bg-3);
    color: var(--text-muted);
  }
  li.done .tick {
    background: color-mix(in srgb, var(--ok, var(--accent)) 22%, transparent);
    color: var(--accent-hover);
  }
  .txt {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .lbl {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  li.done .lbl {
    color: var(--text-muted);
    text-decoration: line-through;
    text-decoration-color: var(--text-faint);
  }
  .sub {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  li.done .sub {
    display: none;
  }
  .go {
    flex: none;
    padding: 5px 11px;
    font-size: var(--fs-small);
    font-weight: 600;
  }
  .did {
    flex: none;
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  @keyframes setup-in {
    from {
      opacity: 0;
      transform: translateY(-6px);
    }
  }
  @media (max-width: 768px) {
    .setup {
      margin: var(--sp-2) var(--sp-3) 0;
    }
    .sub {
      display: none;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .setup {
      animation: none;
    }
  }
</style>
