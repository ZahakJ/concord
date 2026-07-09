// Zero-dependency test for the markdown renderer (`npm test`). The renderer's
// safety rests on one invariant — user input is HTML-escaped before any tags
// are added — so this suite feeds it hostile input and asserts the output can
// only contain our whitelisted tags with safe attributes.
import { renderMarkdown, containsMention } from "./markdown.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// Unsafe = a real tag outside our whitelist, an event handler, or a non-https/
// non-image URI inside a real tag. Escaped text (&lt;script&gt;) is safe.
const ALLOWED = /^<\/?(strong|em|code|pre|a|img|ul|ol|li|blockquote|span)(\s|>|\/)/;
function unsafe(html) {
  for (const m of html.matchAll(/<[^>]*>/g)) {
    const tag = m[0];
    if (!ALLOWED.test(tag)) return `unexpected tag ${tag}`;
    if (/on[a-z]+\s*=/i.test(tag)) return `event handler in ${tag}`;
    if (/(href|src)\s*=\s*"(?!https?:|data:image\/)/i.test(tag)) return `bad uri in ${tag}`;
  }
  return null;
}

const hostile = [
  "<script>alert(1)</script>",
  "<img src=x onerror=alert(1)>",
  "[x](javascript:alert(1))",
  "![image](javascript:alert(1))",
  "![image](data:text/html;base64,PHNjcmlwdD4=)",
  "`<b>`**<i>**",
  "> <script>x</script>",
  "- <svg onload=x>",
  "**<a href=x>**",
  "```\n</code></pre><script>x</script>\n```",
  '@euclid" onmouseover="alert(1)',
];
for (const h of hostile) {
  const bad = unsafe(renderMarkdown(h, ["euclid"]));
  assert(!bad, `${bad} for input: ${h}`);
}

// Feature coverage.
let out = renderMarkdown("**b** *i* `c` https://x.dev");
assert(out.includes("<strong>b</strong>"), `bold: ${out}`);
assert(out.includes("<em>i</em>"), `italic: ${out}`);
assert(out.includes("<code>c</code>"), `code: ${out}`);
assert(out.includes('<a href="https://x.dev"'), `link: ${out}`);

out = renderMarkdown("```go\nx < y\n```");
// Code fences carry a language label (data-lang) when one is given.
assert(out.includes('<pre data-lang="go"><code>x &lt; y</code></pre>'), `fence: ${out}`);

out = renderMarkdown("- a\n- b\n1. c");
assert(out.includes("<ul><li>a</li><li>b</li></ul><ol><li>c</li></ol>"), `lists: ${out}`);

assert(renderMarkdown("> quoted").includes("<blockquote>quoted</blockquote>"), "quote");
// Forwarded messages render as an attribution blockquote followed by the body.
let fwd = renderMarkdown("> ↪ Forwarded from axioms #general\nthe *point* stands");
// The ↪ arrow is an emoji, so it's wrapped for sizing (emoji-wrapping feature).
assert(
  fwd.includes('<blockquote><span class="emoji">↪</span> Forwarded from axioms #general</blockquote>'),
  `forward attribution: ${fwd}`,
);
assert(fwd.includes("<em>point</em>"), `forward body renders markdown: ${fwd}`);
let mentionOut = renderMarkdown("hey @euclid look", ["euclid"]);
assert(mentionOut.includes('class="mention"'), "mention render (string form)");
assert(mentionOut.includes('data-mention="euclid"'), "mention carries data-mention");
assert(mentionOut.includes(">@euclid</span>"), "mention keeps @name text");
assert(
  renderMarkdown("yo @me", [{ name: "me", self: true }]).includes("mention-self"),
  "self mention gets self class",
);
assert(renderMarkdown("code `*not bold*` end").includes("<code>*not bold*</code>"), "code shields md");
assert(
  renderMarkdown("unclosed ```\nfence body").includes("<pre><code>fence body</code></pre>"),
  "unclosed fence runs to end",
);

// Custom emoji: :name: in the map renders as an <img>; unknown names stay text.
const cmap = { partyblob: "data:image/png;base64,AAAA" };
out = renderMarkdown("hi :partyblob: and :unknown:", [], cmap);
assert(out.includes('<img class="cemoji" src="data:image/png;base64,AAAA"'), `custom emoji img: ${out}`);
assert(out.includes(":unknown:"), "unknown emoji left as literal text");
// A custom-emoji image src can't be a vector for injection (name charset only).
assert(!/<img[^>]*src="(?!data:image\/)/.test(out), "no non-image emoji src");
// Defense in depth: even a malformed emoji URL that reaches the renderer must be
// escaped so it can't break out of the src attribute and inject an event handler.
const evil = { evilblob: 'data:image/png;base64,A" onerror="alert(1)' };
out = renderMarkdown("boom :evilblob:", [], evil);
assert(!out.includes('onerror="alert(1)"'), `emoji src must be escaped: ${out}`);
assert(out.includes("&quot;"), "emoji src quote is entity-escaped");

assert(containsMention("yo @euclid", ["euclid"]), "containsMention positive");
assert(!containsMention("yo @euclidian", ["euclid"]), "containsMention word boundary");

// Skin-tone helpers (picker): modifier appended, VS16 dropped on toned forms,
// and the reverse name lookup survives both directions.
const { applyTone, emojiName, TONABLE, EMOJI } = await import("./emoji.js");
assert(applyTone("👍", "\u{1F3FE}") === "👍🏾", "applyTone appends modifier");
assert(applyTone("✌️", "\u{1F3FB}") === "✌🏻", "applyTone drops VS16 on toned form");
assert(applyTone("✌️", "") === "✌️", "empty tone is a no-op");
assert(emojiName("👍🏾") === "thumbsup", "emojiName strips tone");
assert(emojiName("✌🏻") === "v", "emojiName restores VS16 base");
assert(emojiName(EMOJI.fire) === "fire", "emojiName direct hit");
assert([...TONABLE].every((n) => EMOJI[n]), "every TONABLE name exists in EMOJI");

if (failures) {
  console.error(`${failures} failure(s)`);
  process.exit(1);
}
console.log("markdown: all tests pass");
