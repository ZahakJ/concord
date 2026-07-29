// Tests for animated-image detection, against REAL encoded files rather than
// hand-written byte arrays — a hand-built header would only prove the code
// agrees with my idea of the format, which is the thing most likely to be wrong.
// The fixtures below are generated at test time by re-encoding tiny images.
import { execFileSync } from "node:child_process";
import { readFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { isAnimated } from "./animated.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// ImageMagick isn't a build dependency, only a convenience for this test, so a
// machine without it skips rather than fails.
let magick = true;
try {
  execFileSync("magick", ["-version"], { stdio: "ignore" });
} catch {
  magick = false;
}

if (!magick) {
  console.log("animated.js: skipped (ImageMagick not installed)");
} else {
  const dir = mkdtempSync(join(tmpdir(), "emoji-"));
  const p = (n) => join(dir, n);
  const run = (args) => execFileSync("magick", args, { stdio: "ignore" });
  try {
    run(["-size", "32x32", "xc:red", p("a.png")]);
    run(["-size", "32x32", "xc:blue", p("b.png")]);
    const cases = [
      ["animated gif", ["-delay", "20", p("a.png"), p("b.png"), p("anim.gif")], "anim.gif", true],
      ["still gif", [p("a.png"), p("still.gif")], "still.gif", false],
      ["still png", [p("a.png"), p("still.png")], "still.png", false],
      ["animated webp", ["-delay", "20", p("a.png"), p("b.png"), p("anim.webp")], "anim.webp", true],
      ["still webp", [p("a.png"), p("still.webp")], "still.webp", false],
      ["still jpeg", [p("a.png"), p("still.jpg")], "still.jpg", false],
    ];
    for (const [label, args, file, want] of cases) {
      run(args);
      const got = isAnimated(readFileSync(p(file)));
      assert(got === want, `${label}: expected isAnimated=${want}, got ${got}`);
    }
    // A transparent still GIF carries one Graphic Control Extension, which is
    // why the check counts them rather than merely spotting one.
    run(["-size", "32x32", "xc:none", p("trans.gif")]);
    assert(!isAnimated(readFileSync(p("trans.gif"))), "a transparent still gif is not animated");

    // Garbage in, false out — never a throw, since this runs on user input.
    assert(!isAnimated(new Uint8Array([1, 2, 3])), "a truncated file is not animated");
    assert(!isAnimated(new Uint8Array(0)), "an empty file is not animated");
    assert(!isAnimated(null), "no file at all is not animated");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }

  if (failures) {
    console.error(`\n${failures} animated test(s) failed`);
    process.exit(1);
  }
  console.log("animated.js: all tests passed");
}
