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

// searchEmoji returns up to `limit` [name, emoji] pairs matching a prefix or
// substring of the shortcode name (prefix matches ranked first).
export function searchEmoji(query, limit = 8) {
  const q = query.toLowerCase();
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

// replaceShortcodes converts every :name: in the text to its emoji (unknown
// names are left untouched).
export function replaceShortcodes(text) {
  return text.replace(/:([a-z0-9_+-]+):/gi, (match, name) => EMOJI[name.toLowerCase()] || match);
}

// activeShortcode inspects a text + caret position and returns the partial
// shortcode being typed (e.g. ":fir" -> {start, query:"fir"}), or null.
export function activeShortcode(text, caret) {
  const upto = text.slice(0, caret);
  const m = upto.match(/(?:^|\s):([a-z0-9_+-]{1,30})$/i);
  if (!m) return null;
  return { start: caret - m[1].length - 1, query: m[1].toLowerCase() };
}
