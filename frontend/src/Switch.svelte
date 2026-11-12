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
  /* 36×20 with a 16px knob. Off is a recessed track with a control-strength
     edge (--border-strong, 3:1 — the edge is the only thing that says this is
     a switch) and a muted knob; on is the accent with a white knob. No glow:
     the fill is the state. */
  .switch {
    position: relative;
    flex: none;
    width: 36px;
    height: 20px;
    border-radius: 999px;
    background: var(--bg-3);
    box-shadow: inset 0 0 0 1px var(--border-strong);
    transition: background var(--dur-quick) var(--ease-out);
  }
  .switch.on {
    background: var(--accent);
    box-shadow: none;
  }
  .knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: var(--text-muted);
    transition:
      transform var(--dur-move) var(--ease-out),
      background var(--dur-quick) ease;
  }
  .switch.on .knob {
    background: #fff;
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
  :global(:root[data-motion="reduced"]) .switch,
  :global(:root[data-motion="reduced"]) .knob {
    transition: none;
  }
</style>
