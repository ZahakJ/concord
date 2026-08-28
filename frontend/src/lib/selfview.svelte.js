// selfview.svelte.js — is your own camera already on screen at a size worth
// calling a preview?
//
// You can turn your camera on from four places and end up looking at none of
// them: a screen share auto-focuses theater mode, and theater draws exactly one
// big thing plus a strip of 34px circles, so "Turn on camera" lit the button,
// lit the header pill, and showed your face nowhere. You were broadcasting
// with no way to see what you were broadcasting.
//
// The rule the self-view follows is one sentence: a preview exists whenever
// your camera is on and you cannot already see it. Which needs one fact that
// only the call stage knows — whether your own tile is currently drawn — and
// this is where the stage leaves it. Nothing else in the app has any business
// reading it, hence a module of its own rather than another field on S.

let covered = $state(false);

// Called by the stage whenever its layout changes, and with false on unmount:
// a panel that is gone is a panel that is showing you nothing.
export function setSelfViewCovered(v) {
  covered = !!v;
}

export function selfViewCovered() {
  return covered;
}
