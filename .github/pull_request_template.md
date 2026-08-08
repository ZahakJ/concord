## What this changes

<!-- One or two sentences, from the user's side. -->

## Why

<!-- The problem it solves, or a link to the issue. -->

## How you verified it

<!-- Commands you ran, and what you did in the app. For example: "`make race`
     passed; then two peers, created a guild, invited from peer 1, messaged
     both ways, restarted peer 2 and the history came back." -->

## Checklist

<!-- Tick what applies. The last two often will not; say so rather than
     leaving them blank. -->


- [ ] One concern. No unrelated refactor riding along.
- [ ] `make test` and `cd frontend && npm run lint && npm test` pass locally.
- [ ] Commits signed off (`git commit -s`).
- [ ] Comments explain constraints, not what the next line does.
- [ ] Touches the wire protocol, storage schema, or governance rules? Describe
      above what happens when an older peer meets a newer one.
- [ ] Changes what an attacker can do? The threat model in `docs/DESIGN.md` is
      updated in this pull request.

`CONTRIBUTING.md` in the repository root covers all of the above.
