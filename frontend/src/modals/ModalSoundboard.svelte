<script>
  // The sound studio: a shelf of sounds, and a set of sliders for making more.
  //
  // Every sound in here is a recipe — a couple of dozen numbers — never a file.
  // Which is why the shelf can be shared by pressing a button: sending one to a
  // channel sends the sound itself, and keeping one costs nothing because the
  // message already carried it.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EmptyState from "../EmptyState.svelte";
  import { S, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { haptic } from "../lib/touch.js";
  import { tooltip } from "../lib/tooltip.js";
  import { plural } from "../lib/plural.js";
  import { playRecipe, soundboardEnabled, setSoundboardEnabled } from "../lib/sounds.js";
  import {
    SFX_FIELDS,
    SFX_WAVES,
    SFX_GLYPHS,
    MAX_NAME_BYTES,
    MAX_TOTAL_MS,
    FLAG_EXP,
    FLAG_SWELL,
    STARTER_SHELF,
    encodeRecipe,
    encodeSound,
    recipeTotalMs,
    recipeGlyph,
    soundLength,
  } from "../lib/sfxrecipe.js";
  import { shelfSounds, keepSound, dropSound, shelfFull, MAX_SHELF } from "../lib/soundshelf.svelte.js";

  // onPick is set when the studio is opened from a voice room: there the
  // outcome is a sound to press, not a message to send.
  let { onClose, onPick = null } = $props();

  let tab = $state("shelf"); // shelf | make
  let boardOn = $state(soundboardEnabled());
  let busy = $state(false);

  // The sound on the bench. Seeded from the first starter preset so the sliders
  // open on something that makes a noise rather than on silence.
  let draft = $state({ ...STARTER_SHELF[0], name: "My sound" });

  const sounds = $derived(shelfSounds());
  const draftPayload = $derived(encodeRecipe(draft));
  const totalMs = $derived(recipeTotalMs(draft));
  const ok = $derived(!!draftPayload);

  // The sliders, in the order they make sense to turn: what it is, then how
  // long, then how much of it.
  const DIALS = ["f0", "f1", "dur", "attack", "gain", "noise", "noiseHz", "noiseQ", "reps", "gap", "step", "detune", "room"];

  function preview(recipe) {
    // force: the studio's preview is a deliberate request for a sound, so it
    // is not swallowed by the mute or by the anti-overlap gate.
    playRecipe(recipe, { force: true });
  }

  function loadOntoBench(recipe) {
    draft = { ...recipe };
    tab = "make";
  }

  function keep() {
    if (!ok) return;
    if (shelfFull()) return flash(`Your shelf holds ${MAX_SHELF} sounds — remove one first`);
    keepSound(draftPayload);
    haptic("light");
    flash(`"${draft.name}" is on your shelf`, "success");
    tab = "shelf";
  }

  async function sendToChannel(payload) {
    if (busy) return;
    const chId = S.activeChannelId;
    if (!chId) return flash("Open a channel first");
    busy = true;
    try {
      await api.sendMessage(chId, encodeSound(payload), "");
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }

  function pick(payload, recipe) {
    if (onPick) {
      onPick(payload, recipe);
      onClose();
      return;
    }
    sendToChannel(payload);
  }

  function toggleBoard() {
    boardOn = !boardOn;
    setSoundboardEnabled(boardOn);
  }
</script>

<Modal title="Sounds" {onClose} wide>
  <div class="tabs" role="tablist" aria-label="Sounds">
    <button
      type="button"
      role="tab"
      class="tab"
      class:on={tab === "shelf"}
      aria-selected={tab === "shelf"}
      onclick={() => (tab = "shelf")}
    >Shelf</button>
    <button
      type="button"
      role="tab"
      class="tab"
      class:on={tab === "make"}
      aria-selected={tab === "make"}
      onclick={() => (tab = "make")}
    >Make one</button>
  </div>

  {#if tab === "shelf"}
    {#if sounds.length}
      <p class="cap">
        {plural(sounds.length, "sound")} on this device.
        {onPick ? "Pick one to play it for the room." : "Send one and anybody who hears it can keep it."}
      </p>
      <div class="grid">
        {#each sounds as s (s.payload)}
          <div class="card">
            <button type="button" class="face" onclick={() => preview(s.recipe)} aria-label={`Hear "${s.recipe.name}"`} use:tooltip>
              <span class="glyph" aria-hidden="true">{recipeGlyph(s.recipe)}</span>
              <span class="cname">{s.recipe.name}</span>
              <span class="clen">{soundLength(s.recipe)}</span>
            </button>
            <div class="cacts">
              <button type="button" class="mini" onclick={() => pick(s.payload, s.recipe)} disabled={busy} aria-label={onPick ? "Play for the room" : "Send to this channel"} use:tooltip>
                <Icon name={onPick ? "megaphone" : "send"} size={13} />
              </button>
              <button type="button" class="mini" onclick={() => loadOntoBench(s.recipe)} aria-label="Open in the editor" use:tooltip>
                <Icon name="edit" size={13} />
              </button>
              <button type="button" class="mini" onclick={() => dropSound(s.payload)} aria-label="Remove from the shelf" use:tooltip>
                <Icon name="trash" size={13} />
              </button>
            </div>
          </div>
        {/each}
      </div>
    {:else}
      <EmptyState icon="speaker" headline="No sounds yet" sub="Sounds here are recipes — a few dozen bytes of oscillator settings, never a file. Build one and it travels by being heard.">
        {#snippet actions()}
          <button class="primary" onclick={() => (tab = "make")}>
            <Icon name="plus" size={13} /> Make one
          </button>
        {/snippet}
      </EmptyState>
    {/if}

    <button type="button" class="mute" role="switch" aria-checked={boardOn} onclick={toggleBoard}>
      <span class="switch" class:on={boardOn}><span class="knob"></span></span>
      <span>
        Hear sound effects
        <span class="sub">Off silences the soundboard and every sound chip, and leaves mentions and ringtones alone.</span>
      </span>
    </button>
  {:else}
    <div class="bench">
      <div class="row name-row">
        <label class="field grow">
          <span class="lbl">Name</span>
          <input bind:value={draft.name} maxlength={MAX_NAME_BYTES} placeholder="Airhorn of my own" />
        </label>
        <div class="field">
          <span class="lbl">Face</span>
          <div class="glyphs" role="radiogroup" aria-label="Face">
            {#each SFX_GLYPHS as g, i (g)}
              <button
                type="button"
                class="gpick"
                class:on={draft.glyph === i}
                role="radio"
                aria-checked={draft.glyph === i}
                aria-label={`Face ${i + 1}`}
                onclick={() => (draft.glyph = i)}
              >{g}</button>
            {/each}
          </div>
        </div>
      </div>

      <div class="field">
        <span class="lbl">Waveform</span>
        <div class="seg" role="radiogroup" aria-label="Waveform">
          {#each SFX_WAVES as w, i (w)}
            <button
              type="button"
              class="seg-btn"
              class:on={draft.wave === i}
              role="radio"
              aria-checked={draft.wave === i}
              onclick={() => (draft.wave = i)}
            >{w[0].toUpperCase() + w.slice(1)}</button>
          {/each}
        </div>
      </div>

      <!-- One row per parameter, generated from the same table the decoder
           validates against — so the editor physically cannot build a sound
           that would be refused on arrival. -->
      <div class="dials">
        {#each DIALS as k (k)}
          <label class="dial">
            <span class="dlbl">{SFX_FIELDS[k].label}</span>
            <input
              type="range"
              min={SFX_FIELDS[k].min}
              max={SFX_FIELDS[k].max}
              step={SFX_FIELDS[k].step || 1}
              bind:value={draft[k]}
            />
            <span class="dval">{draft[k]}{SFX_FIELDS[k].unit ? ` ${SFX_FIELDS[k].unit}` : ""}</span>
          </label>
        {/each}
      </div>

      <div class="flags">
        <button
          type="button"
          class="flag"
          class:on={(draft.flags & FLAG_EXP) !== 0}
          aria-pressed={(draft.flags & FLAG_EXP) !== 0}
          onclick={() => (draft.flags ^= FLAG_EXP)}
        >Curved pitch sweep</button>
        <button
          type="button"
          class="flag"
          class:on={(draft.flags & FLAG_SWELL) !== 0}
          aria-pressed={(draft.flags & FLAG_SWELL) !== 0}
          onclick={() => (draft.flags ^= FLAG_SWELL)}
        >Build across the hits</button>
      </div>

      <p class="cap" class:bad={!ok} aria-live="polite">
        {#if totalMs > MAX_TOTAL_MS}
          {(totalMs / 1000).toFixed(1)}s is too long — a sound has to fit in {MAX_TOTAL_MS / 1000}s including its repeats.
        {:else if !ok}
          Not a sound this app will play. Adjust something.
        {:else}
          {(totalMs / 1000).toFixed(1)}s, {new TextEncoder().encode(draftPayload).length} bytes on the wire.
        {/if}
      </p>
    </div>

    <div class="actions">
      <button class="ghost" onclick={() => preview(draft)} disabled={!ok}>
        <Icon name="play" size={13} /> Hear it
      </button>
      <button class="ghost" onclick={keep} disabled={!ok}>Keep on my shelf</button>
      <button onclick={() => pick(draftPayload, draft)} disabled={!ok || busy}>
        {onPick ? "Play for the room" : "Send to this channel"}
      </button>
    </div>
  {/if}
</Modal>

<style>
  .tabs {
    display: flex;
    gap: var(--sp-1);
    margin-bottom: var(--sp-3);
    border-bottom: 1px solid var(--border);
  }
  .tab {
    padding: var(--sp-2) var(--sp-3);
    border: none;
    border-bottom: 2px solid transparent;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-ui);
    font-weight: 600;
    cursor: pointer;
  }
  .tab.on {
    color: var(--text);
    border-bottom-color: var(--accent);
  }

  .cap {
    margin: 0 0 var(--sp-3);
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .cap.bad {
    color: var(--warn-text);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(132px, 1fr));
    gap: var(--sp-2);
  }
  .card {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-1);
    overflow: hidden;
  }
  .face {
    display: grid;
    justify-items: center;
    gap: var(--sp-1);
    width: 100%;
    padding: var(--sp-3) var(--sp-2);
    border: none;
    background: transparent;
    color: var(--text);
    cursor: pointer;
  }
  .face:hover {
    background: var(--bg-3);
  }
  .glyph {
    font-size: var(--fs-display);
    line-height: 1;
  }
  .cname {
    font-size: var(--fs-compact);
    font-weight: 600;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .clen {
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }
  .cacts {
    display: flex;
    border-top: 1px solid var(--border);
  }
  .mini {
    flex: 1;
    display: grid;
    place-items: center;
    padding: var(--sp-1) 0;
    min-height: var(--tap-min);
    border: none;
    background: var(--bg-2);
    color: var(--text-muted);
    cursor: pointer;
  }
  .mini:hover:not(:disabled) {
    color: var(--text);
    background: var(--bg-3);
  }
  .mini:disabled {
    opacity: 0.4;
  }

  .mute {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
    width: 100%;
    margin-top: var(--sp-4);
    padding: var(--sp-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-1);
    color: var(--text);
    font-size: var(--fs-ui);
    text-align: left;
    cursor: pointer;
  }
  .mute .sub {
    display: block;
    margin-top: var(--sp-1);
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  /* Same geometry as every other switch in the app (see ModalPoll): a toggle
     that is a few pixels off its neighbours reads as a different control. */
  .switch {
    flex: none;
    width: 34px;
    height: 20px;
    margin-top: 2px;
    border-radius: var(--radius-md);
    background: var(--bg-3);
    position: relative;
    transition: background var(--dur-standard) ease;
  }
  .switch.on {
    background: var(--accent);
  }
  .knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #fff;
    transition: transform var(--dur-standard) ease;
  }
  .switch.on .knob {
    transform: translateX(14px);
  }

  .bench {
    display: grid;
    gap: var(--sp-3);
  }
  .row {
    display: flex;
    gap: var(--sp-3);
    flex-wrap: wrap;
  }
  .field {
    display: grid;
    gap: var(--sp-1);
  }
  .grow {
    flex: 1;
    min-width: 180px;
  }
  .lbl {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .glyphs {
    display: flex;
    flex-wrap: wrap;
    gap: 2px;
    max-width: 340px;
  }
  .gpick {
    width: 27px;
    height: 27px;
    padding: 0;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    background: transparent;
    cursor: pointer;
    line-height: 1;
  }
  .gpick.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }

  .seg {
    display: flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .seg-btn {
    flex: 1;
    padding: var(--sp-2) 0;
    border: none;
    background: var(--bg-2);
    color: var(--text-muted);
    font-size: var(--fs-compact);
    cursor: pointer;
  }
  .seg-btn.on {
    background: var(--accent-soft);
    color: var(--text);
  }

  .dials {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
    gap: var(--sp-1) var(--sp-4);
  }
  /* accent-color is how every other slider in the app is themed (the profile
     crop zoom, the UI scale). A native range control here would be the one
     blue thing on the screen. */
  .dial input[type="range"] {
    accent-color: var(--accent);
    min-width: 0;
  }
  .dial {
    display: grid;
    grid-template-columns: 5.4rem 1fr 4.4rem;
    align-items: center;
    gap: var(--sp-2);
  }
  .dlbl {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .dval {
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  .flags {
    display: flex;
    gap: var(--sp-2);
    flex-wrap: wrap;
  }
  .flag {
    padding: var(--sp-1) var(--sp-3);
    min-height: var(--tap-min);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-2);
    color: var(--text-muted);
    font-size: var(--fs-compact);
    cursor: pointer;
  }
  .flag.on {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    margin-top: var(--sp-4);
    flex-wrap: wrap;
  }
</style>
