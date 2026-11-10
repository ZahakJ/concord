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
const ALLOWED =
  /^<\/?(strong|em|s|u|code|pre|a|img|ul|ol|li|blockquote|span|div|button|h[3-5])(\s|>|\/)/;
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
  // The underscore emphasis pass runs over already-escaped text, like the rest.
  '_<img src=x onerror=alert(1)>_',
  "_`<b>`_",
  // The fence now emits a wrapper and a button around the code. Neither may be
  // reachable from inside the fence.
  '```\n</code></pre></div><button onclick=alert(1)>x</button>\n```',
  '```js"><button onclick=alert(1)>\ncode\n```',
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

// Both spellings of italic, and the boundaries that keep the underscore form
// from eating things that are not emphasis.
out = renderMarkdown("_i_ and *j*");
assert(out.includes("<em>i</em>") && out.includes("<em>j</em>"), `both italics: ${out}`);
assert(renderMarkdown("__u__") === "<u>u</u>", `underline still wins: ${renderMarkdown("__u__")}`);
assert(renderMarkdown("~~x~~") === "<s>x</s>", `strike: ${renderMarkdown("~~x~~")}`);
out = renderMarkdown("call read_file_sync now");
assert(!out.includes("<em>"), `snake_case is not emphasis: ${out}`);
out = renderMarkdown("`_x_`");
assert(out === "<code>_x_</code>", `code span is literal: ${out}`);
// A URL is parked for the emphasis passes: without that, the path here becomes
// <em> tags and the link that follows is built around the wreckage.
out = renderMarkdown("see https://ex.dev/_draft_/notes ok");
assert(out.includes('href="https://ex.dev/_draft_/notes"'), `url survives emphasis: ${out}`);
assert(!out.includes("<em>"), `no emphasis inside a url: ${out}`);
out = renderMarkdown("[the _plan_](https://ex.dev/a_b)");
assert(out.includes("<em>plan</em>"), `emphasis in link text still renders: ${out}`);
assert(out.includes('href="https://ex.dev/a_b"'), `masked href untouched: ${out}`);

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


// ---- links that end where they should --------------------------------------
// <https://x.org> is the standard way to write a bare link; the brackets are
// punctuation and must not end up inside the href.
out = renderMarkdown("and <https://example.org>");
assert(out.includes('href="https://example.org"') && !out.includes("&gt;\""), `autolink brackets dropped: ${out}`);
assert(!/&lt;<a/.test(out) && !/<\/a>&gt;/.test(out), `autolink brackets not shown: ${out}`);
out = renderMarkdown("src=https://x.com></iframe>");
assert(out.includes('href="https://x.com"'), `a link stops at an escaped >: ${out}`);
assert(!unsafe(out), `still inert: ${out}`);
out = renderMarkdown('see https://x.org/a"onmouseover="alert(1)');
assert(out.includes('href="https://x.org/a"') && !unsafe(out), `a link stops at a quote: ${out}`);
// The sentence's punctuation is not part of the address.
out = renderMarkdown("go to https://example.org/path.");
assert(out.includes('href="https://example.org/path"') && out.endsWith("</a>."), `trailing period left out: ${out}`);
out = renderMarkdown("(see https://en.wikipedia.org/wiki/Foo_(bar)) ok");
assert(out.includes('href="https://en.wikipedia.org/wiki/Foo_(bar)"'), `balanced brackets kept: ${out}`);
out = renderMarkdown("(https://x.org)");
assert(out.includes('href="https://x.org"') && out.endsWith("</a>)"), `an enclosing bracket is not part of it: ${out}`);
// Bare www. addresses link, with https.
out = renderMarkdown("Visit www.example.org.");
assert(out.includes('href="https://www.example.org"') && out.includes(">www.example.org</a>."), `www. links: ${out}`);
assert(!renderMarkdown("wwwx.org and www.a").includes("<a "), "not every www is a link");
assert(!unsafe(renderMarkdown('www.x.org/"><img src=x onerror=alert(1)>')), "www links stay inert");
assert(!unsafe(renderMarkdown("www.javascript:alert(1)")), "no scheme smuggled into a www link");

// ---- backslash escapes -----------------------------------------------------
out = renderMarkdown("\\*escaped\\*");
assert(out === "*escaped*", `\\* is a literal asterisk: ${out}`);
out = renderMarkdown("\\_\\_init\\_\\_ and __init__");
assert(out.startsWith("__init__ and <u>init</u>"), `escaped underscores stay: ${out}`);
assert(renderMarkdown("\\# not a header") === "# not a header", "an escaped # is not a header");
assert(renderMarkdown("a \\\\ b") === "a \\ b", "a doubled backslash is one backslash");
assert(renderMarkdown("C:\\Users\\me") === "C:\\Users\\me", "a backslash before a letter is just a backslash");
out = renderMarkdown("`\\*code\\*`");
assert(out === "<code>\\*code\\*</code>", `no escapes inside code: ${out}`);
assert(!unsafe(renderMarkdown("\\<script>alert(1)\\</script>")), "an escaped < stays escaped");
out = renderMarkdown("\\<b\\>");
assert(out === "&lt;b&gt;", `escaped angle brackets: ${out}`);
out = renderMarkdown("\\@everyone \\#general", ["everyone"], null, { channels: [{ name: "general", id: "c1" }] });
assert(!out.includes("mention"), `escaped @ and # are text: ${out}`);

// ---- the shrug -------------------------------------------------------------
out = renderMarkdown("hi ¯\\_(ツ)_/¯");
assert(out === "hi ¯\\_(ツ)_/¯", `the shrug keeps its arms: ${out}`);
out = renderMarkdown("¯\\_(ツ)_/¯ and _this_");
assert(out === "¯\\_(ツ)_/¯ and <em>this</em>", `and emphasis still works beside it: ${out}`);

// ---- emphasis inside emphasis ----------------------------------------------
out = renderMarkdown("**bold *nested italic* bold**");
assert(out === "<strong>bold <em>nested italic</em> bold</strong>", `italic inside bold: ${out}`);
out = renderMarkdown("***bold italic***");
assert(out === "<strong><em>bold italic</em></strong>", `triple stars: ${out}`);
out = renderMarkdown("**a** and **b**");
assert(out === "<strong>a</strong> and <strong>b</strong>", `two bold runs stay apart: ${out}`);
out = renderMarkdown("unclosed **bold mixed ~~strike* end");
assert(!/<strong>|<s>/.test(out) && !unsafe(out), `unbalanced markers stay literal: ${out}`);
assert(!unsafe(renderMarkdown("**<img src=x onerror=alert(1)> *x* **")), "nested emphasis stays inert");

// ---- nested lists ----------------------------------------------------------
out = renderMarkdown("- bullet a\n- bullet b\n  - nested c\n- d");
assert(
  out === "<ul><li>bullet a</li><li>bullet b<ul><li>nested c</li></ul></li><li>d</li></ul>",
  `an indented item nests: ${out}`,
);
out = renderMarkdown("1. one\n2. two\n   - sub\n3. three");
assert(out === "<ol><li>one</li><li>two<ul><li>sub</li></ul></li><li>three</li></ol>", `bullets under a number: ${out}`);
out = renderMarkdown("- a\n    - deep\n  - mid\n- top");
assert(out === "<ul><li>a<ul><li>deep</li><li>mid</li></ul></li><li>top</li></ul>", `loose indents: ${out}`);
assert(renderMarkdown("  - a\n- b") === "<ul><li>a</li><li>b</li></ul>", "an indented first item is still one list");
assert(renderMarkdown("- a\n1. b") === "<ul><li>a</li></ul><ol><li>b</li></ol>", "a different kind starts a new list");
assert(renderMarkdown("- a\ntext") === "<ul><li>a</li></ul>text", "text after a list closes it");
assert(!unsafe(renderMarkdown("- a\n  - <svg onload=x>\n    - <script>")), "nested items are escaped");


// ---- message links ---------------------------------------------------------
out = renderMarkdown("see concord://msg/abcd1234/ef567890 ok", [], null, { channels: [{ id: "abcd1234", name: "gen<eral" }] });
assert(out.includes('class="msg-link"') && out.includes('data-jump-channel="abcd1234"') && out.includes('data-jump-msg="ef567890"'), `a message link becomes a jump chip: ${out}`);
assert(out.includes("message in #gen&lt;eral"), `the chip names the channel, escaped: ${out}`);
assert(renderMarkdown("concord://msg/aaaa1111/bbbb2222").includes("linked message"), "an unknown channel still gets a chip");
assert(!renderMarkdown('concord://msg/x"><img src=x onerror=1>/y').includes("msg-link"), "nothing outside the id alphabet becomes a chip");
assert(!unsafe(renderMarkdown('concord://msg/abcd1234/ef567890"onclick="alert(1)')), "a chip stays inert");

if (failures) {
  console.error(`${failures} failure(s)`);
  process.exit(1);
}
console.log("markdown: all tests pass");
