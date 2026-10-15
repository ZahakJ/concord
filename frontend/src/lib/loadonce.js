// loadOnce / want: the fetch-once-and-retry-on-failure rule for lazily loaded
// chunks (lib/islands.svelte.js), kept out of the runes file so it can be
// tested in node.
//
// loadOnce(load, onLoaded) returns a loader that fetches at most once at a
// time, hands the module to onLoaded when it lands, and after that resolves
// immediately. A failure clears the in-flight promise, so the next ask fetches
// again instead of returning the same rejection forever.
export function loadOnce(load, onLoaded) {
  let pending = null;
  let done = false;
  return () => {
    if (done) return Promise.resolve();
    if (!pending) {
      pending = load().then(
        (m) => {
          done = true;
          onLoaded(m);
        },
        (err) => {
          pending = null;
          throw err;
        },
      );
    }
    return pending;
  };
}

// want(loader, onFail) is how a SURFACE asks for its chunk: from an $effect,
// where nobody awaits the promise. It never rejects — a rejection there is an
// unhandled one, logged and otherwise ignored, while the surface that wanted
// the code simply never appears. onFail gets the error instead, so the caller
// can say so and put back the state that asked, which is what lets the next
// ask (the next tap) be the retry. Resolves true when the chunk is there.
export function want(loader, onFail) {
  return loader().then(
    () => true,
    (err) => {
      onFail?.(err);
      return false;
    },
  );
}
