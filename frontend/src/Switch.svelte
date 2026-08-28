<script>
  // The switch. One track, one knob, one size — the pixels only, with no
  // opinion about what wraps them.
  //
  // Six surfaces had drawn their own before this: four track sizes (40×24,
  // 36×20, 34×20 twice) and two knob class names, one of which — the poll
  // dialog's — had lost its track border entirely and read as a bare white dot
  // on a dark ground, which is the visual language of *on* for something that
  // was off. ModalSoundboard's comment says outright that it copied ModalPoll's
  // geometry, so the drift was already propagating.
  //
  // Deliberately NOT interactive: the accessible role belongs to the element
  // the pointer actually hits. SettingRow is a <button role="switch">, the
  // events dialog wraps a real checkbox (a form control inside a form), and a
  // component that insisted on being the button could serve neither. It takes
  // `children` so the checkbox case can put its input inside the track.
  let { on = false, children } = $props();
</script>

<span class="switch" class:on aria-hidden={children ? undefined : "true"}>
  {#if children}{@render children()}{/if}
  <span class="knob"></span>
</span>

<style>
  .switch {
    position: relative;
    flex: none;
    width: 40px;
    height: 24px;
    border-radius: 12px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    transition:
      background var(--dur-standard) ease,
      border-color var(--dur-standard) ease;
  }
  .switch.on {
    background: var(--accent);
    border-color: var(--accent);
    box-shadow: 0 0 10px color-mix(in srgb, var(--accent) 35%, transparent);
  }
  .knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #fff;
    box-shadow: 0 1px 2px rgb(0 0 0 / 0.35);
    transition: transform var(--dur-standard) var(--ease-out);
  }
  .switch.on .knob {
    transform: translateX(16px);
  }
  /* A checkbox the caller put inside the track: it stays the real control for
     the keyboard and the form, and covers the whole track so the hit area is
     the switch you can see. */
  .switch :global(input[type="checkbox"]) {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    opacity: 0;
    z-index: 1;
  }
  @media (prefers-reduced-motion: reduce) {
    .switch,
    .knob {
      transition: none;
    }
  }
</style>
