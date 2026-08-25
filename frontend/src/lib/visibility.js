// Telling the backend whether anybody is actually looking at this page.
//
// The Go core slows every periodic loop — DHT discovery, guild reconcile,
// own-device dials — to one shared multi-minute beat when the app is off
// screen. Android has always driven that from the native Activity lifecycle,
// and every other shell drove it from nothing at all: a minimised desktop
// window and a browser tab left open behind forty others ran the full
// foreground cadence indefinitely.
//
// A page reports itself, and the backend takes a vote across every attached
// client (see internal/bridge/visibility.go), so several tabs on one node
// behave sensibly instead of the last one to speak deciding for the rest.
//
// The asymmetry below is the interesting part. Going VISIBLE is reported
// immediately: it is the edge that costs something to get wrong, because the
// core kicks every throttled loop awake on it and the user is, by definition,
// right there watching for the app to catch up. Going HIDDEN waits, because
// alt-tabbing through four windows fires four visibilitychange pairs in a
// second and none of them means anyone stopped looking.

// hideDelay is how long a page must stay hidden before we believe it. Long
// enough to swallow a tab-switch flurry, short enough that a genuinely
// abandoned window settles well inside one background beat.
export const hideDelay = 1500;

/**
 * Builds a reporter that turns raw visibility changes into the smallest useful
 * number of backend calls.
 *
 * send(visible) is invoked only on a real change of reported state, so an
 * idle page makes no calls at all. The timer functions are injected so the
 * debounce can be tested without waiting it out.
 */
export function createVisibilityReporter({
  send,
  delay = hideDelay,
  setTimer = setTimeout,
  clearTimer = clearTimeout,
} = {}) {
  let pending = null; // the armed hide, if any
  let reported = null; // what the backend last heard; null = nothing yet

  function disarm() {
    if (pending !== null) {
      clearTimer(pending);
      pending = null;
    }
  }

  function commit(visible) {
    disarm();
    if (reported === visible) return;
    reported = visible;
    send(visible);
  }

  return {
    /** A visibilitychange (or the initial reading). */
    update(visible) {
      if (visible) {
        // Immediate, and it also cancels an armed hide — which is the whole
        // tab-flurry case: switch away and back, and the backend hears nothing.
        commit(true);
        return;
      }
      if (reported === false || pending !== null) return;
      pending = setTimer(() => {
        pending = null;
        commit(false);
      }, delay);
    },

    /**
     * pagehide: the page is being torn down or frozen. There is no flurry to
     * wait out here — whatever happens next, nobody is looking now — and
     * waiting risks the timer never firing at all.
     */
    leave() {
      commit(false);
    },

    /** Drops an armed hide, for teardown. */
    stop() {
      disarm();
    },

    /** What the backend was last told; null before the first report. */
    get reported() {
      return reported;
    },
  };
}
