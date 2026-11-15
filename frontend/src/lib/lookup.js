// lookup.js — id-to-object tables that are safe to index with any string.
//
// Plenty of tables in this app are indexed with an id that came from
// somewhere else: a profile, a guild record, a message. A plain object
// literal answers for more names than the ones written in it, so these
// tables are built without a prototype, and the few that are not (large
// literals in hot modules) are read through `own`.

// table copies an object's own entries onto a prototype-less object.
export const table = (o) => Object.assign(Object.create(null), o);

// own reads an own property of `o`, or undefined. Accepts only string keys.
export const own = (o, key) => (o && typeof key === "string" && Object.hasOwn(o, key) ? o[key] : undefined);

// at reads a list entry by an index that must be a non-negative integer.
export const at = (list, i) => (Number.isInteger(i) && i >= 0 && i < list.length ? list[i] : undefined);
