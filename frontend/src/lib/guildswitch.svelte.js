// guildswitch.svelte.js — whether the guild switcher is open, and what it is
// anchored to. The switcher is opened from three places that live in three
// components (the place header, the strip's overflow tile, a long-press on the
// phone's Guilds tab) and drawn once, at the app root, so it is never caged by
// a transformed ancestor (the phone's home page moves while it parallaxes).
export const GSW = $state({ open: false, rect: null });

// openGuildSwitcher(el): `el` is the thing it hangs under (the place header).
// On a phone it is a sheet and the anchor is ignored.
export function openGuildSwitcher(el) {
  const r = el?.getBoundingClientRect?.();
  GSW.rect = r ? { x: r.left, y: r.top, w: r.width, h: r.height } : null;
  GSW.open = true;
}
export function closeGuildSwitcher() {
  GSW.open = false;
}
export function toggleGuildSwitcher(el) {
  if (GSW.open) closeGuildSwitcher();
  else openGuildSwitcher(el);
}
