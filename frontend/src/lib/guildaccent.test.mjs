// The user's accent is clamped so the "you" signals stay legible in both
// themes, and a guild's colour is decoration that never becomes --accent.
import { clampAccent, accentHolds, contrast, tileColor, TEAL } from "./guildaccent.js";

const fail = (msg) => {
  console.error("  FAIL", msg);
  process.exitCode = 1;
};
const rgbOf = (s) => (s.startsWith("#") ? [1, 3, 5].map((i) => parseInt(s.slice(i, i + 2), 16)) : s.match(/\d+/g).map(Number));

// The brand defaults already hold, and pass through untouched.
if (clampAccent(TEAL.dark, "dark") !== TEAL.dark) fail("dark teal must pass through");
if (clampAccent(TEAL.light, "light") !== TEAL.light) fail("light teal must pass through");

// Every accent preset in Appearance, plus deliberately weak colours, holds in
// both themes after clamping.
const presets = ["#14a394", "#5865f2", "#e5636a", "#f5a524", "#a78bfa", "#f472b6", "#7bb87b", "#7c8798", "#2dd4bf", "#38bdf8"];
const weak = ["#777777", "#9a9a9a", "#3a3a3a", "#ffe066", "#e8e8e8", "#202020", "#bfa8ff"];
for (const theme of ["dark", "light"]) {
  for (const c of [...presets, ...weak]) {
    const out = clampAccent(c, theme);
    if (!accentHolds(rgbOf(out), theme)) fail(`${c} in ${theme} still fails after clamp (${out})`);
  }
}
// An unreadable value falls back to the brand teal.
if (clampAccent("not a colour", "light") !== TEAL.light) fail("garbage falls back to teal");
// The clamp keeps the hue's direction: a blue stays bluer than it is red.
{
  const [r, , b] = rgbOf(clampAccent("#5865f2", "light"));
  if (!(b > r)) fail("clamping must not change a blue into something else");
}
if (contrast([0, 0, 0], [255, 255, 255]) < 20.9) fail("contrast maths");
// Tile colours are solid and stable per guild.
if (tileColor({ id: "abc" }) !== tileColor({ id: "abc" })) fail("tile colour must be stable");
if (/gradient/.test(tileColor({ id: "abc" }))) fail("tile colour must be solid");
if (!process.exitCode) console.log("guildaccent.test.mjs: ok");

// The accent is teal unless the user chose otherwise: a profile colour no
// longer becomes the accent by itself, and following it is opt-in.
{
  const { resolveAccent, profileAccent, hueOf, isAlertHue, ACCENT_PROFILE } = await import("./guildaccent.js");
  if (resolveAccent("", "#e8743b") !== "") fail("the default accent must ignore the profile colour");
  if (resolveAccent(undefined, "#e8743b") !== "") fail("an unset accent must ignore the profile colour");
  if (resolveAccent("#3d7dd6", "#e8743b") !== "#3d7dd6") fail("a chosen preset is kept");
  if (resolveAccent(ACCENT_PROFILE, "#3d7dd6") !== "#3d7dd6") fail("a cool profile colour passes through when opted in");
  // A warm profile colour, opted in, is toned so it cannot read as an alert.
  for (const c of ["#e8743b", "#ff0000", "#e5636a", "#f5a524", "#d4145a"]) {
    const out = resolveAccent(ACCENT_PROFILE, c);
    const [h, s] = hueOf(out);
    if (isAlertHue(h) && s > 0.33) fail(`${c} as accent is still a saturated alert hue (${out})`);
    const [h0] = hueOf(c);
    if (Math.abs(h - h0) > 3 && Math.abs(h - h0) < 357) fail(`${c}: toning must keep the hue (${out})`);
  }
  if (profileAccent("") !== "") fail("no profile colour gives no accent");
}
