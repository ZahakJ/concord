// anemoji.js — when a bundled animated emoji is allowed to actually animate.
//
// The first version of this animated every eligible emoji, always. Two things
// went wrong, and both are the same mistake seen from different angles.
//
// PERFORMANCE. These are 30-frame WebPs, ~32 KB at the median. A screen holding
// a few jumbo messages and a row of reaction pills is decoding hundreds of
// frames a second, forever, including for emoji scrolled well out of sight.
// Motion is the exception now, not the rule: an emoji rests as the static
// Twemoji it always was, and only becomes animated while it is on screen (a
// jumbo message) or under the cursor (a reaction, a picker cell).
//
// RENDER CHURN. Worse, and less obvious: the markup used to depend on whether
// the manifest had loaded. That flag flips shortly after mount, the rendered
// HTML string changes, and Svelte replaces the whole message body — which
// destroys and re-fetches EVERY image in it, animated or not. That is why even
// plain emoji became slow to appear. So the swap is imperative now: the HTML a
// message renders never changes, and these helpers reach in and retarget the
// `src` of an already-mounted <img>. Nothing re-renders.
//
// The markup contract: renderMarkdown emits the static Twemoji as `src` and,
// when an animated version exists, its URL in `data-anim`.

// Swap in the animation, remembering where to go back to.
function play(img) {
  const anim = img.dataset.anim;
  if (!anim || img.dataset.playing === "1") return;
  if (!img.dataset.still) img.dataset.still = img.getAttribute("src") || "";
  img.dataset.playing = "1";
  img.setAttribute("src", anim);
}

// Back to the still. Pointing an <img> at a different file is what actually
// stops the decoder — there is no way to pause an animated WebP from CSS.
function stop(img) {
  if (img.dataset.playing !== "1") return;
  img.dataset.playing = "0";
  if (img.dataset.still) img.setAttribute("src", img.dataset.still);
}

const targets = (node) =>
  node.matches?.("img[data-anim]") ? [node] : [...node.querySelectorAll("img[data-anim]")];

// animateInView: play while at least half the emoji is on screen. For jumbo
// messages, where the emoji is the message and motion is the point — but only
// for the handful actually being looked at, however long the history is.
export function animateInView(node, enabled = true) {
  if (typeof IntersectionObserver === "undefined") return {};
  // Inline emoji, mid-sentence at 18px, are not worth animating even when they
  // are on screen — a paragraph of looping faces is noise. Only the jumbo case
  // opts in, so the action takes a flag rather than being attached selectively
  // (the same element is a jumbo body one render and an ordinary one the next).
  if (!enabled) return {};
  const io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) (e.isIntersecting ? play : stop)(e.target);
    },
    { threshold: 0.5 },
  );
  // The body is re-rendered as one HTML blob, so re-scan when it changes rather
  // than observing images that no longer exist.
  const scan = () => {
    io.disconnect();
    for (const img of targets(node)) io.observe(img);
  };
  scan();
  const mo = new MutationObserver(scan);
  mo.observe(node, { childList: true, subtree: true });
  return {
    destroy() {
      mo.disconnect();
      io.disconnect();
      for (const img of targets(node)) stop(img);
    },
  };
}

// animateOnHover: play under the cursor, still otherwise. Right for anything
// small and repeated — reaction pills, and the picker grid, where a wall of
// simultaneously looping emoji is both unreadable and expensive. It doubles as
// the preview: point at one and it moves.
export function animateOnHover(node) {
  const over = (e) => {
    const img = e.target?.closest?.("img[data-anim]");
    if (img) play(img);
  };
  const out = (e) => {
    const img = e.target?.closest?.("img[data-anim]");
    if (img) stop(img);
  };
  // pointerover/pointerout bubble, so one pair of listeners covers every cell in
  // a grid and keeps working as cells come and go.
  node.addEventListener("pointerover", over);
  node.addEventListener("pointerout", out);
  return {
    destroy() {
      node.removeEventListener("pointerover", over);
      node.removeEventListener("pointerout", out);
    },
  };
}
