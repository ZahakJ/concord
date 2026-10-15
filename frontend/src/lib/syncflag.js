// raisedFor: run `fn` with a flag raised for its SYNCHRONOUS part only.
//
// Some flags are a message to the first few lines of a call — "this selection
// is a browse, do not push the page" — and are read before the call's first
// await. Holding one up until the whole async call resolves leaks it into
// everything else that runs in the meantime: a second, unrelated call that
// starts while the first is still awaiting the network reads the flag as its
// own. The flag goes up, `fn` starts, the flag comes down — before anything
// else can run — and then the caller awaits the returned promise.
export function raisedFor(set, value, fn) {
  set(value);
  let out;
  try {
    out = fn();
  } finally {
    set(false);
  }
  return out;
}
