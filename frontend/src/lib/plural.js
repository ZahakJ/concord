// plural — one place to count things in a sentence.
//
// It exists because the hand-rolled version kept getting the edges wrong. The
// two failures that shipped: `n > 1 ? "s" : ""`, which is right for 1 and wrong
// for 0 ("Published to 0 channel"), and a bare `${total} members`, which reads
// "1 members" for a guild of one. Neither is a hard bug and both are the sort
// of thing that makes an app feel machine-written.
//
//   plural(3, "channel")            → "3 channels"
//   plural(1, "channel")            → "1 channel"
//   plural(0, "channel")            → "0 channels"
//   plural(2, "guild", "guildies")  → "2 guildies"
export function plural(n, one, many = one + "s") {
  return `${n} ${n === 1 ? one : many}`;
}

// The suffix on its own, for a sentence that has already printed the number.
export const plur = (n, one = "", many = "s") => (n === 1 ? one : many);
