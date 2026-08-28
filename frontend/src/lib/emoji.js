// emoji.js — Concord's emoji system: a curated shortcode map powering
// :colon: autocompletion in the composer, the emoji picker, and reactions.

export const EMOJI = {
  // smileys
  smile: "😄", grin: "😁", joy: "😂", rofl: "🤣", sweat_smile: "😅",
  laughing: "😆", wink: "😉", blush: "😊", innocent: "😇", slight_smile: "🙂",
  upside_down: "🙃", relieved: "😌", heart_eyes: "😍", star_struck: "🤩",
  kissing_heart: "😘", yum: "😋", tongue: "😛", zany: "🤪", raised_eyebrow: "🤨",
  neutral: "😐", expressionless: "😑", smirk: "😏", unamused: "😒",
  rolling_eyes: "🙄", grimacing: "😬", lying: "🤥", pensive: "😔",
  sleepy: "😪", sleeping: "😴", mask: "😷", thermometer_face: "🤒",
  dizzy_face: "😵", exploding_head: "🤯", cowboy: "🤠", sunglasses: "😎",
  nerd: "🤓", monocle: "🧐", confused: "😕", worried: "😟", frown: "🙁",
  open_mouth: "😮", astonished: "😲", flushed: "😳", pleading: "🥺",
  cry: "😢", sob: "😭", scream: "😱", angry: "😠", rage: "😡",
  skull: "💀", clown: "🤡", ghost: "👻", alien: "👽", robot: "🤖",
  poop: "💩", thinking: "🤔", shush: "🤫", melting: "🫠", saluting: "🫡",

  // gestures & people
  wave: "👋", raised_hand: "✋", ok_hand: "👌", pinch: "🤏", v: "✌️",
  crossed_fingers: "🤞", love_you: "🤟", metal: "🤘", call_me: "🤙",
  point_left: "👈", point_right: "👉", point_up: "👆", point_down: "👇",
  thumbsup: "👍", "+1": "👍", thumbsdown: "👎", "-1": "👎", fist: "✊",
  punch: "👊", clap: "👏", raised_hands: "🙌", open_hands: "👐",
  handshake: "🤝", pray: "🙏", muscle: "💪", writing: "✍️", eyes: "👀",
  eye: "👁️", brain: "🧠", ear: "👂", nose: "👃", shrug: "🤷",
  facepalm: "🤦", bow: "🙇", dancer: "💃", running: "🏃", wizard: "🧙",

  // hearts
  heart: "❤️", orange_heart: "🧡", yellow_heart: "💛", green_heart: "💚",
  blue_heart: "💙", purple_heart: "💜", black_heart: "🖤", white_heart: "🤍",
  broken_heart: "💔", two_hearts: "💕", sparkling_heart: "💖",
  heartpulse: "💗", cupid: "💘", gift_heart: "💝",

  // animals & nature
  dog: "🐶", cat: "🐱", mouse: "🐭", hamster: "🐹", rabbit: "🐰",
  fox: "🦊", bear: "🐻", panda: "🐼", koala: "🐨", tiger: "🐯",
  lion: "🦁", cow: "🐮", pig: "🐷", frog: "🐸", monkey: "🐵",
  chicken: "🐔", penguin: "🐧", bird: "🐦", eagle: "🦅", duck: "🦆",
  owl: "🦉", bat: "🦇", wolf: "🐺", unicorn: "🦄", bee: "🐝",
  bug: "🐛", butterfly: "🦋", snail: "🐌", turtle: "🐢", snake: "🐍",
  octopus: "🐙", squid: "🦑", shrimp: "🦐", crab: "🦀", whale: "🐳",
  dolphin: "🐬", fish: "🐟", shark: "🦈", crocodile: "🐊", dragon: "🐉",
  cactus: "🌵", christmas_tree: "🎄", evergreen: "🌲", palm: "🌴",
  seedling: "🌱", herb: "🌿", clover: "🍀", maple_leaf: "🍁",
  mushroom: "🍄", rose: "🌹", sunflower: "🌻", blossom: "🌼",
  tulip: "🌷", cherry_blossom: "🌸",

  // sky & weather
  sun: "☀️", moon: "🌙", full_moon: "🌕", star: "⭐", star2: "🌟",
  sparkles: "✨", zap: "⚡", fire: "🔥", boom: "💥", comet: "☄️",
  rainbow: "🌈", cloud: "☁️", rain: "🌧️", snow: "❄️", snowman: "⛄",
  tornado: "🌪️", ocean: "🌊", droplet: "💧", earth: "🌍",

  // food & drink
  apple: "🍎", pear: "🍐", orange: "🍊", lemon: "🍋", banana: "🍌",
  watermelon: "🍉", grapes: "🍇", strawberry: "🍓", cherries: "🍒",
  peach: "🍑", mango: "🥭", pineapple: "🍍", coconut: "🥥", kiwi: "🥝",
  tomato: "🍅", avocado: "🥑", eggplant: "🍆", potato: "🥔", corn: "🌽",
  hot_pepper: "🌶️", bread: "🍞", cheese: "🧀", egg: "🥚", bacon: "🥓",
  pancakes: "🥞", fries: "🍟", pizza: "🍕", burger: "🍔", hotdog: "🌭",
  sandwich: "🥪", taco: "🌮", burrito: "🌯", salad: "🥗", ramen: "🍜",
  spaghetti: "🍝", sushi: "🍣", bento: "🍱", curry: "🍛", dumpling: "🥟",
  cookie: "🍪", cake: "🍰", birthday: "🎂", cupcake: "🧁", pie: "🥧",
  chocolate: "🍫", candy: "🍬", lollipop: "🍭", donut: "🍩", icecream: "🍦",
  coffee: "☕", tea: "🍵", milk: "🥛", beer: "🍺", beers: "🍻",
  wine: "🍷", cocktail: "🍸", tropical_drink: "🍹", champagne: "🥂",

  // activities & objects
  soccer: "⚽", basketball: "🏀", football: "🏈", baseball: "⚾",
  tennis: "🎾", volleyball: "🏐", pingpong: "🏓", bowling: "🎳",
  goal: "🥅", golf: "⛳", trophy: "🏆", medal: "🏅", first_place: "🥇",
  video_game: "🎮", joystick: "🕹️", dice: "🎲", chess: "♟️",
  dart: "🎯", slot_machine: "🎰", puzzle: "🧩", teddy: "🧸",
  art: "🎨", clapper: "🎬", mic: "🎤", headphones: "🎧", guitar: "🎸",
  piano: "🎹", trumpet: "🎺", violin: "🎻", drum: "🥁", saxophone: "🎷",
  rocket: "🚀", airplane: "✈️", car: "🚗", taxi: "🚕", bus: "🚌",
  train: "🚆", ship: "🚢", bike: "🚲", motorcycle: "🏍️", helicopter: "🚁",
  house: "🏠", office: "🏢", castle: "🏰", tent: "⛺", island: "🏝️",
  mountain: "⛰️", volcano: "🌋", camping: "🏕️",
  phone: "📱", computer: "💻", keyboard: "⌨️", printer: "🖨️", tv: "📺",
  camera: "📷", video_camera: "📹", movie_camera: "🎥", telescope: "🔭",
  microscope: "🔬", bulb: "💡", flashlight: "🔦", candle: "🕯️",
  book: "📖", books: "📚", notebook: "📓", memo: "📝", pencil: "✏️",
  paperclip: "📎", scissors: "✂️", lock: "🔒", unlock: "🔓", key: "🔑",
  hammer: "🔨", wrench: "🔧", gear: "⚙️", link: "🔗", shield: "🛡️",
  package: "📦", gift: "🎁", balloon: "🎈", tada: "🎉", confetti: "🎊",
  crown: "👑", gem: "💎", ring: "💍", money: "💰", dollar: "💵",
  credit_card: "💳", chart_up: "📈", chart_down: "📉", email: "📧",
  inbox: "📥", outbox: "📤", bell: "🔔", megaphone: "📣", hourglass: "⏳",
  watch: "⌚", alarm: "⏰", pill: "💊", syringe: "💉",

  // symbols
  check: "✅", x: "❌", warning: "⚠️", question: "❓", exclamation: "❗",
  no_entry: "⛔", 100: "💯", infinity: "♾️", recycle: "♻️",
  arrow_up: "⬆️", arrow_down: "⬇️", arrow_left: "⬅️", arrow_right: "➡️",
  play: "▶️", pause: "⏸️", stop: "⏹️", music: "🎵", notes: "🎶",
  peace: "☮️", yin_yang: "☯️", om: "🕉️", atom: "⚛️", radioactive: "☢️",
  wheelchair: "♿", anchor: "⚓", fleur: "⚜️", diamond: "🔶", circle: "🔵",
  red_circle: "🔴", green_circle: "🟢", white_check: "✔️", zzz: "💤",
  speech: "💬", thought: "💭", eye_speech: "👁️‍🗨️", flag_white: "🏳️",
  flag_checkered: "🏁", pirate: "🏴‍☠️",
};

const NAMES = Object.keys(EMOJI);

// ── Skin tones ───────────────────────────────────────────────────────────────
// The five Fitzpatrick modifiers ("" = default yellow). Applied only to the
// curated TONABLE set below — human/hand emoji whose single-modifier form is
// universally supported. Everything else renders unchanged.
export const SKIN_TONES = [
  { key: "", label: "Default" },
  { key: "\u{1F3FB}", label: "Light" },
  { key: "\u{1F3FC}", label: "Medium-light" },
  { key: "\u{1F3FD}", label: "Medium" },
  { key: "\u{1F3FE}", label: "Medium-dark" },
  { key: "\u{1F3FF}", label: "Dark" },
];

export const TONABLE = new Set([
  "wave", "raised_hand", "ok_hand", "pinch", "v", "crossed_fingers",
  "love_you", "metal", "call_me", "point_left", "point_right", "point_up",
  "point_down", "thumbsup", "+1", "thumbsdown", "-1", "fist", "punch",
  "clap", "raised_hands", "open_hands", "pray", "muscle", "writing",
  "ear", "nose", "shrug", "facepalm", "bow", "dancer", "running", "wizard",
]);

// applyTone renders an emoji char with a Fitzpatrick modifier. Toned emoji
// drop the VS16 (U+FE0F) — a modifier already forces emoji presentation.
export function applyTone(char, tone) {
  if (!tone) return char;
  return char.replace(/\uFE0F/gu, "") + tone;
}

// The user's preferred tone (a modifier char or ""), persisted locally.
const TONE_KEY = "concord.emojiTone";
export function emojiTone() {
  try {
    const t = localStorage.getItem(TONE_KEY) || "";
    return SKIN_TONES.some((s) => s.key === t) ? t : "";
  } catch {
    return "";
  }
}
export function setEmojiTone(tone) {
  try {
    localStorage.setItem(TONE_KEY, tone);
  } catch {
    /* storage blocked */
  }
}

// emojiName reverse-maps a char back to its shortcode (tone modifiers are
// stripped first, so 👍🏾 -> "thumbsup"). Returns "" for unknown chars.
const NAME_OF = new Map();
for (const n of NAMES) if (!NAME_OF.has(EMOJI[n])) NAME_OF.set(EMOJI[n], n);
export function emojiName(char) {
  const base = char.replace(/[\u{1F3FB}-\u{1F3FF}]/gu, "");
  const look = (c) => NAME_OF.get(c) || FULL?.nameOf.get(c);
  return look(char) || look(base) || look(base + "\uFE0F") || "";
}

// Category tabs for the picker, derived from EMOJI's section order (no
// re-listing of names). Hearts + symbols share the Symbols tab.
const idx = (n) => NAMES.indexOf(n);
const range = (a, b) => NAMES.slice(idx(a), idx(b) + 1);
export const CATEGORIES = [
  { key: "people", label: "Smileys & People", icon: "😀", names: range("smile", "wizard") },
  { key: "nature", label: "Animals & Nature", icon: "🐻", names: range("dog", "earth") },
  { key: "food", label: "Food & Drink", icon: "🍔", names: range("apple", "champagne") },
  { key: "activity", label: "Activities & Objects", icon: "⚽", names: range("soccer", "syringe") },
  { key: "symbols", label: "Symbols", icon: "✅", names: [...range("heart", "gift_heart"), ...range("check", "pirate")] },
];

// Recently-used emoji (chars), most-recent first, persisted locally.
const RECENT_KEY = "concord.emojiRecent";
export function recentEmoji() {
  try {
    return JSON.parse(localStorage.getItem(RECENT_KEY)) || [];
  } catch {
    return [];
  }
}
export function pushRecentEmoji(e) {
  if (!e) return;
  try {
    const next = [e, ...recentEmoji().filter((x) => x !== e)].slice(0, 24);
    localStorage.setItem(RECENT_KEY, JSON.stringify(next));
  } catch {
    /* storage blocked */
  }
}

// ── The full Unicode set ─────────────────────────────────────────────────────
// Everything above is the 379 hand-curated shortcodes, and it stays: they are
// written into messages people have already sent, into their recents, and into
// the quick-reaction bars, so their meaning may never move. The generated table
// (lib/emojitable.js, ~1700 emoji in Unicode's own nine groups) is layered
// UNDER it — a generated name is only taken if the curated set has not claimed
// it, and a generated char only names itself if no curated shortcode already
// does. Adopting a name whose curated meaning differed would silently change
// what an old `:tongue:` renders as.
//
// It is installed rather than imported because it arrives through a dynamic
// import (lib/emojifull.svelte.js) and this module is on the send path, which
// is synchronous. Everything below degrades to the curated table until then;
// nothing waits on it.
let FULL = null;

// "flag: Saudi Arabia" -> "flag_saudi_arabia". Digits survive, which is what
// keeps the ten keycaps apart; a name that slugs to nothing or to something
// already taken gets a numeric suffix rather than being dropped, because the
// grid still has to be able to name every cell it draws.
function slug(name) {
  return name
    .toLowerCase()
    .replace(/&/g, " and ")
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}

export function installFullEmoji(raw) {
  if (FULL) return FULL;
  const groups = [];
  const items = [];
  const byName = new Map(Object.entries(EMOJI));
  const nameOf = new Map(NAME_OF);
  const tonable = new Set();
  let g = null;
  for (const line of raw.split("\n")) {
    if (line.charCodeAt(0) === 9) {
      const [, key, label, icon] = line.split("\t");
      g = { key, label, icon, names: [] };
      groups.push(g);
      continue;
    }
    const [char, label, tone] = line.split("\t");
    if (!char || !g) continue;
    let name = slug(label) || "emoji";
    if (byName.has(name) && byName.get(name) !== char) {
      let i = 2;
      while (byName.has(`${name}_${i}`)) i++;
      name = `${name}_${i}`;
    }
    byName.set(name, char);
    if (!nameOf.has(char)) nameOf.set(char, name);
    if (tone) tonable.add(name);
    items.push({ n: name, c: char, l: label.toLowerCase() });
    g.names.push(name);
  }
  FULL = { groups, items, byName, nameOf, tonable };
  return FULL;
}

// Is the full table in yet? Callers that want to re-render when it lands read
// this through lib/emojifull.svelte.js, which boxes it in $state.
export const fullEmojiReady = () => !!FULL;

// The picker's tabs: Unicode's nine groups once the table is in, the five
// curated ones until then.
export function emojiCategories() {
  return FULL ? FULL.groups : CATEGORIES;
}

// The char for a shortcode, curated first.
export function emojiChar(name) {
  return EMOJI[name] || FULL?.byName.get(name) || "";
}

// Whether a skin tone applies. The curated list is a hand-picked set of hands
// and people; the generated one is Unicode's own answer, which is strictly
// better and covers 249 entries instead of 33.
export function emojiTonable(name) {
  return FULL ? FULL.tonable.has(name) || TONABLE.has(name) : TONABLE.has(name);
}

// searchEmoji returns up to `limit` [name, emoji] pairs. Shortcode prefix hits
// rank first, then shortcode substrings, then the CLDR name — which is what
// makes "saudi" find the flag and "birthday" find the cake without a separate
// keyword list to keep in sync.
export function searchEmoji(query, limit = 8) {
  const q = query.toLowerCase();
  if (!FULL) {
    if (!q) return NAMES.slice(0, limit).map((n) => [n, EMOJI[n]]);
    const starts = [];
    const contains = [];
    for (const n of NAMES) {
      if (n.startsWith(q)) starts.push(n);
      else if (n.includes(q)) contains.push(n);
      if (starts.length >= limit) break;
    }
    return [...starts, ...contains].slice(0, limit).map((n) => [n, EMOJI[n]]);
  }
  if (!q) return FULL.items.slice(0, limit).map((e) => [e.n, e.c]);
  const starts = [];
  const contains = [];
  const labelled = [];
  for (const e of FULL.items) {
    if (e.n.startsWith(q)) starts.push(e);
    else if (e.n.includes(q)) contains.push(e);
    else if (e.l.includes(q)) labelled.push(e);
    if (starts.length >= limit) break;
  }
  return [...starts, ...contains, ...labelled].slice(0, limit).map((e) => [e.n, e.c]);
}

// replaceShortcodes converts every :name: in the text to its emoji (unknown
// names are left untouched).
export function replaceShortcodes(text) {
  return text.replace(
    /:([a-z0-9_+-]+):/gi,
    (match, name) => EMOJI[name.toLowerCase()] || FULL?.byName.get(name.toLowerCase()) || match,
  );
}

// activeShortcode inspects a text + caret position and returns the partial
// shortcode being typed (e.g. ":fir" -> {start, query:"fir"}), or null.
export function activeShortcode(text, caret) {
  const upto = text.slice(0, caret);
  const m = upto.match(/(?:^|\s):([a-z0-9_+-]{1,30})$/i);
  if (!m) return null;
  return { start: caret - m[1].length - 1, query: m[1].toLowerCase() };
}
