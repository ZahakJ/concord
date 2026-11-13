// Which ICE servers a call uses, held in place (`npm test` runs it).
import { callIce, hasOwnStun, usesThirdParty, PUBLIC_STUN } from "./callice.js";

let failures = 0;
const check = (name, ok) => {
  if (!ok) {
    failures++;
    console.error("FAIL:", name);
  }
};
const urls = (list) => list.flatMap((s) => [].concat(s.urls));
const turn = { urls: ["turn:rv.example:3478?transport=tcp"], username: "u", credential: "p" };
const stun = { urls: ["stun:rv.example:3478"] };

check("no rendezvous config: the public server, so two home networks still connect", urls(callIce(undefined)).join() === PUBLIC_STUN);
check("an empty list is the same", urls(callIce([])).join() === PUBLIC_STUN);
check("a relay but no STUN of its own: the public server is added, the relay kept",
  urls(callIce([turn])).join() === [PUBLIC_STUN, turn.urls[0]].join());
check("the relay's credentials are passed through untouched", callIce([turn])[1] === turn);
check("the rendezvous has its own STUN: nothing is added", urls(callIce([stun, turn])).join() === [stun.urls[0], turn.urls[0]].join());
check("urls given as a string count too", !usesThirdParty([{ urls: "stun:rv.example:3478" }]));
check("a turn: URL is not mistaken for a STUN server", !hasOwnStun([turn]) && !hasOwnStun([{ urls: "turns:stun.example:5349" }]));

check("no third party, with nothing from the rendezvous: no server at all", callIce(undefined, { noThirdParty: true }).length === 0);
check("no third party, with a relay: the relay alone", urls(callIce([turn], { noThirdParty: true })).join() === turn.urls[0]);
check("no third party: never contacts the public server, whatever the rendezvous sent",
  [undefined, null, [], [turn], [stun, turn], [{}], [{ urls: [] }]].every((l) => !usesThirdParty(l, { noThirdParty: true })));
check("no third party, a rendezvous answer that lists the public server itself does not get it through",
  callIce([{ urls: [PUBLIC_STUN] }], { noThirdParty: true }).length === 0 &&
    callIce([{ urls: PUBLIC_STUN }, turn], { noThirdParty: true }).length === 1 &&
    urls(callIce([{ urls: ["stun:stun2.l.google.com:19302", "STUN:stun.l.google.com", "stun:stun.l.google.com:19302?transport=udp", stun.urls[0]] }, turn], { noThirdParty: true })).join() ===
      [stun.urls[0], turn.urls[0]].join());
check("a server with no URL is dropped rather than handed to the browser", callIce([{}, turn], { noThirdParty: true }).length === 1);
check("usesThirdParty agrees with callIce", usesThirdParty([turn]) && !usesThirdParty([stun, turn]));

if (failures) process.exit(1);
console.log("callice.test.mjs: ok");
