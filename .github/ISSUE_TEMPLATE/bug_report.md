---
name: Bug report
about: Something behaves wrong, crashes, or does not do what it says
labels: bug
---

<!-- Found a security problem? Do not file it here. See SECURITY.md. -->

## What happened

## What you expected instead

## Steps to reproduce

1.
2.
3.

## Version and platform

- Concord version (bottom of Settings: click the version stamp to copy it):
- Build: web binary / native desktop / Android / iOS
- OS and version:
- Browser, if you are running the web build:

## Does it reproduce with a fresh identity?

<!-- A lot of bugs only one person can see turn out to live in an old local
     database, so this answer saves a round trip. Point CONCORD_HOME at an empty
     directory and you get a new identity and an empty store without touching
     your real ones:

       CONCORD_HOME=/tmp/concord-fresh ./concord-<your-build>

     From a checkout, `make peers` does the same thing, and `rm -rf .dev/peer2`
     resets a single peer. -->

Yes / No / Not tried

## Logs

<!-- `.dev/peer*.log` for a dev peer, or whatever the app printed to the
     terminal. Redact invite codes and fingerprints if you would rather not
     publish them. -->
