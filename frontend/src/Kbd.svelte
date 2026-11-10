<script>
  // One key, drawn the one way: a small outlined chip in faint ink. A chord is
  // several of these with "+" between them, which the caller writes, so the
  // notation stays the caller's ("Ctrl", "⌘", "Shift") and this stays dumb.
  //
  //   <Kbd>R</Kbd>   <Kbd>Ctrl</Kbd>+<Kbd>K</Kbd>
  //
  // `strong` is for a list of keys you are there to read (the shortcut sheet)
  // rather than a hint in passing at the end of a row: full ink, not faint.
  // `typed` is text you type rather than a key you press ("> ", "/shrug"):
  // monospaced, with its spaces kept, or the sheet teaches the wrong thing.
  let { strong = false, typed = false, children } = $props();
</script>

<kbd class:strong class:typed>{@render children?.()}</kbd>

<style>
  kbd {
    display: inline-block;
    min-width: 16px;
    padding: 0 5px;
    border: 1px solid var(--border);
    border-radius: 4px;
    font-family: inherit;
    font-size: var(--fs-micro);
    font-weight: 500;
    line-height: 16px;
    text-align: center;
    color: var(--text-faint);
    background: transparent;
    white-space: nowrap;
  }
  kbd.strong {
    min-width: 22px;
    padding: 0 6px;
    color: var(--text);
    border-color: var(--border-strong);
    font-size: var(--fs-small);
    line-height: 20px;
  }
  kbd.typed {
    font-family: var(--mono-font);
    white-space: pre;
  }
</style>
