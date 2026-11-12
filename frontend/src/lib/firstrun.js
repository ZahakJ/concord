// firstrun.js — where an account with nothing in it yet should land.
//
// A brand-new desktop account has nothing but its Notes, and opening Notes put
// it in an empty scratchpad with nothing on screen about guilds or where an
// invite code goes — even though the login card had just promised "unlock then
// paste their invite code". The welcome (create a guild / join with a code /
// open Notes) only shows while no channel is open, so the way to show it is to
// open nothing.

// landsOnWelcome: with nothing remembered, a guild list holding no guild and no
// conversation with anybody — only Notes, or nothing at all — opens on the
// welcome rather than inside Notes.
export function landsOnWelcome(guilds) {
  return !(guilds || []).some((g) => g.kind !== "dm" || !g.dmNotes);
}
