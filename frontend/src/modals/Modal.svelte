<script>
  let { title, onClose, children } = $props();
</script>

<div
  class="overlay"
  onclick={onClose}
  onkeydown={(e) => e.key === "Escape" && onClose()}
  role="presentation"
>
  <div class="dialog" onclick={(e) => e.stopPropagation()} role="presentation">
    <div class="head">
      <h3>{title}</h3>
      <button class="x" onclick={onClose} aria-label="Close">✕</button>
    </div>
    {@render children()}
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    display: grid;
    place-items: center;
    z-index: 100;
  }
  .dialog {
    width: 380px;
    max-width: 90vw;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .head {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  h3 {
    margin: 0;
    font-size: 16px;
  }
  .x {
    background: transparent;
    color: var(--text-muted);
    padding: 4px 8px;
  }
  .x:hover {
    color: var(--text);
    background: var(--bg-input);
  }
</style>
