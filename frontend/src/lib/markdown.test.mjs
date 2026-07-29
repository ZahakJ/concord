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
    // The only style we ever emit is a validated text colour; anything else in a
    // style attribute (url(), expression, extra declarations) is an injection.
    const st = /style\s*=\s*"([^"]*)"/i.exec(tag);
    if (st && !/^color:#[0-9a-fA-F]{3,6}$/.test(st[1])) return `bad style in ${tag}`;
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
  // Colour syntax must never let a payload reach the inline style.
  "{red;background:url(//evil)|x}",
  "{#fff;position:fixed|x}",
  '{red|"><img src=x onerror=alert(1)>}',
  "{expression(alert(1))|x}",
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

// Colored text: named colors map to a fixed hex; #hex passes through; a bad
// name is left as literal text.
out = renderMarkdown("{red|hot} {#00ff00|lime} {bogus|plain}");
assert(out.includes('<span style="color:#e0555b">hot</span>'), `named color: ${out}`);
assert(out.includes('<span style="color:#00ff00">lime</span>'), `hex color: ${out}`);
assert(out.includes("{bogus|plain}"), `unknown color name stays literal: ${out}`);
assert(renderMarkdown("{red|**bold**}").includes("<strong>bold</strong>"), "nested md inside color");

out = renderMarkdown("```go\nx < y\n```");
// Code fences carry a language label (data-lang) when one is given.
assert(out.includes('<pre data-lang="go"><code>x &lt; y</code></pre>'), `fence: ${out}`);

out = renderMarkdown("- a\n- b\n1. c");
assert(out.includes("<ul><li>a</li><li>b</li></ul><ol><li>c</li></ol>"), `lists: ${out}`);

assert(renderMarkdown("> quoted").includes("<blockquote>quoted</blockquote>"), "quote");
// Forwarded messages render as an attribution blockquote followed by the body.
let fwd = renderMarkdown("> ↪ Forwarded from axioms #general\nthe *point* stands");
// The ↪ arrow is an emoji, so it renders as a Twemoji image (uniform sizing).
assert(
  fwd.includes('<blockquote><img class="emoji" draggable="false" src="/twemoji/21aa.svg" alt="\u21aa" onerror="this.replaceWith(this.alt)" /> Forwarded from axioms #general</blockquote>'),
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

// ---- @mentions, @roles and #channels ----

// A shorter name must not match INSIDE the span a longer one just produced.
// Before mentions were stashed, "@Ann Lee" came back with a nested "@Ann"
// mention whose click target was the wrong person.
out = renderMarkdown("hi @Ann Lee and @Ann", [{ name: "Ann Lee" }, { name: "Ann" }]);
assert(
  out === 'hi <span class="mention" data-mention="Ann Lee">@Ann Lee</span> and <span class="mention" data-mention="Ann">@Ann</span>',
  `longest name wins and never nests: ${out}`,
);
// The same guarantee for a URL that happens to contain an @name.
out = renderMarkdown("see https://x.com/@Ann", [{ name: "Ann" }]);
assert(!out.includes("mention"), `a mention must not be made inside an href: ${out}`);

const refs = {
  roles: [{ name: "movie-night", color: "#ff8800", self: true }, { name: "plain" }],
  channels: [{ id: "ch_1", name: "general" }],
};
out = renderMarkdown("@movie-night in #general", [], null, refs);
assert(out.includes('data-role="movie-night"'), `role mention rendered: ${out}`);
assert(out.includes('style="color:#ff8800"'), "a role's colour tints its pill");
assert(out.includes("mention-self"), "a role you hold is marked self");
// The channel id, not its name — so a rename doesn't break the link.
assert(out.includes('data-channel="ch_1"'), `channel ref carries the id: ${out}`);
assert(out.includes(">#general<"), "channel ref shows the name");

// A person beats a role of the same name: a ping aimed at one person must
// never quietly widen into a broadcast.
out = renderMarkdown("@dave", [{ name: "dave" }], null, { roles: [{ name: "dave" }] });
assert(out.includes('data-mention="dave"') && !out.includes("data-role"), `people win ties: ${out}`);

// A role colour is the one user-supplied string near a style attribute, so it
// goes through the same strict hex gate the rest of the file relies on.
out = renderMarkdown("@bad", [], null, { roles: [{ name: "bad", color: 'red;background:url(x)"' }] });
assert(!out.includes("style="), `a non-hex role colour is dropped, not emitted: ${out}`);

// A role name with regex metacharacters must be matched literally, not compiled.
out = renderMarkdown("@a.b", [], null, { roles: [{ name: "a.b" }] });
assert(out.includes('data-role="a.b"'), `role names are regex-escaped: ${out}`);
assert(!renderMarkdown("@axb", [], null, { roles: [{ name: "a.b" }] }).includes("data-role"), "the dot is literal");

// Unknown names stay plain text — nothing invents a reference.
out = renderMarkdown("@nobody #nowhere", [], null, refs);
assert(!out.includes("mention"), `unknown @ and # stay text: ${out}`);

// A markdown header must not be eaten by the #channel pass.
out = renderMarkdown("# general", [], null, refs);
assert(out.includes("md-h") && !out.includes("mention-channel"), `headers still render: ${out}`);

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
