// keepStable: hand back the SAME object for the same content.
//
// The tables the markdown renderer takes — member names, a guild's custom
// emoji, its roles and channels — are rebuilt from state that is replaced
// wholesale on every refresh, and a refresh follows every presence flap and
// every guild-updated event. A derived that rebuilds an equal table still
// returns a new reference, and everything downstream that compares by
// reference (every visible message row's rendered body) runs again for a
// change that changed nothing. Passed through one of these, an unchanged table
// keeps its old identity and the rows stay put; a changed one is new and they
// re-render as they should.
//
// One instance per table. The key is the JSON of the table, which is cheap at
// the sizes involved (a guild's member list) and computed once per rebuild,
// not once per row.
export function keepStable() {
  let lastKey;
  let last;
  return (next) => {
    const key = JSON.stringify(next);
    if (key === lastKey) return last;
    lastKey = key;
    last = next;
    return next;
  };
}
