// What a link card looks like on a client that predates it.
//
//   node scripts/linkcard-oldrender.mjs [tag ...]     (default: v0.60.0 … v0.60.3)
//
// A link card (frontend/src/lib/linkcard.js) is ordinary markdown plus an
// ordinary attachment token, chosen so that an older client draws something
// sensible without knowing what a card is. This script holds that claim to the
// SHIPPED code: it extracts each release's own frontend/src/lib from git,
// feeds it the exact bodies a card produces, and prints what that release's
// renderer and token parser make of them — the body HTML, the attachments it
// would draw (with captions), and whether its own YouTube block would appear.
// It also renders a hand-typed disguised link the way those releases do, and,
// for contrast and without counting it, the retired "[lp](concord://lp/v1/…)"
// rider that a development build of main appended (zyads review ZL-02).
//
// Nothing here touches the network or a Concord peer; it is pure rendering.
// Exit status is nonzero if any release draws a card body as raw text (a
// visible "[", "](" or "concord://" in the output), drops the title, or shows
// no domain: a v0.60 reader draws the title as a link and would otherwise see
// nothing that says where it goes (link-cards review LC-07), so the card's
// masked line carries " · <domain>" in plain text.
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

const repo = path.resolve(path.dirname(new URL(import.meta.url).pathname), "..");
const tags = process.argv.slice(2).length ? process.argv.slice(2) : ["v0.60.0", "v0.60.1", "v0.60.2", "v0.60.3"];

const BLOB = "3f".repeat(32);
const KEYS = "k".repeat(75);
const b64 = (s) => Buffer.from(s, "utf8").toString("base64url");
const thumb = (desc) => `![image](concord://attach/v2/${BLOB}/${KEYS}/jpeg/480x270/2//${b64(desc)})`;
const YT = "https://www.youtube.com/watch?v=dQw4w9WgXcQ";

// [name, body, the domain the reader must be able to see]
const samples = [
  ["link-only YouTube card", `[Never Gonna Give You Up](${YT}) · youtube.com\n${thumb("Rick Astley")}`, "youtube.com"],
  [
    "link in a sentence",
    `look at this ${YT} lol\n[Never Gonna Give You Up](${YT}) · youtube.com\n${thumb("Rick Astley")}`,
    "youtube.com",
  ],
  [
    "page card with duration",
    `[A talk about \\*things\\*](https://example.org/talk) · example.org\n${thumb("Example Conf · 42:10")}`,
    "example.org",
  ],
  ["title-only card", "[Some page title](https://example.org/a) · example.org", "example.org"],
  [
    "an honest sender's card for a phishing page",
    `[PayPal - Sign in to your account](https://paypa1-secure.evil.example/signin) · evil.example\n${thumb("PayPal")}`,
    "evil.example",
  ],
  ["hand-typed disguised link", "[paypal.com](https://evil.example/login)", ""],
];

let bad = 0;
const scratch = mkdtempSync(path.join(process.env.TMPDIR || tmpdir(), "linkcard-old-"));
try {
  for (const tag of tags) {
    const dir = path.join(scratch, tag);
    mkdirSync(dir, { recursive: true });
    const tar = execFileSync("git", ["-C", repo, "archive", tag, "frontend/src/lib"], { maxBuffer: 256 << 20 });
    execFileSync("tar", ["-x", "-C", dir], { input: tar });
    const lib = path.join(dir, "frontend/src/lib");
    const md = await import(pathToFileURL(path.join(lib, "markdown.js")).href);
    const at = await import(pathToFileURL(path.join(lib, "attachments.js")).href);
    const em = await import(pathToFileURL(path.join(lib, "embeds.js")).href);
    console.log(`\n=== ${tag} ===`);
    for (const [name, body, domain] of samples) {
      const toks = at.parseAttachTokens(body);
      const text = toks.length ? at.stripAttachTokens(body) : body;
      const html = md.renderMarkdown(text);
      const yt = em.extractLinks(body).map(em.youtubeID).find(Boolean) || null;
      console.log(`-- ${name}`);
      console.log(`   body html : ${html}`);
      for (const t of toks)
        console.log(`   picture   : ${t.w}x${t.h} spoiler=${t.spoiler} caption=${JSON.stringify(t.desc)}`);
      console.log(`   old YouTube block: ${yt ? `yes (${yt})` : "no"}`);
      if (domain) {
        const visible = html.replace(/<[^>]*>/g, "");
        // The domain must be visible TEXT, outside the link's own words.
        const outsideLinks = html.replace(/<a [^>]*>[\s\S]*?<\/a>/g, "").replace(/<[^>]*>/g, "");
        console.log(`   domain    : ${outsideLinks.includes(domain) ? `shown (${domain})` : "MISSING"}`);
        if (/\[|\]\(|concord:\/\//.test(visible) || !/<a href="https?:\/\//.test(html)) {
          console.log("   !! raw markdown or a missing title link");
          bad++;
        }
        if (!outsideLinks.includes(domain)) {
          console.log(`   !! the reader sees no domain (${domain})`);
          bad++;
        }
      }
    }
    const lp = `look ${YT}\n[lp](concord://lp/v1/${b64(JSON.stringify([{ u: YT, t: "Never Gonna Give You Up", s: "YouTube" }]))})`;
    console.log("-- (contrast, not counted) the retired lp/v1 rider");
    console.log(`   body html : ${md.renderMarkdown(lp)}`);
  }
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
process.exit(bad ? 1 : 0);
