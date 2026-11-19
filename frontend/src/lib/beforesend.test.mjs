// Every way of putting a message into a channel tells the outbox first
// (api.js onBeforeSend), so a link message waiting for its card is handed to
// the core before it and nothing overtakes it (lib/cardgate.js). The GIF sends
// once skipped it: a GIF picked straight after a link was stored first
// (link-cards-2 review LC2-02).
const order = [];
globalThis.fetch = async (_url, opts) => {
  const { method } = JSON.parse(opts.body);
  order.push(`rpc:${method}`);
  return { ok: true, json: async () => ({ result: null }) };
};
const { api, onBeforeSend } = await import("./api.js");
onBeforeSend((ch) => order.push(`before:${ch}`));

let failures = 0;

const sends = {
  SendMessage: () => api.sendMessage("ch", "hi"),
  SendCallNotice: () => api.sendCallNotice("ch", "started", ""),
  SendAttachment: () => api.sendAttachment("ch", "data:image/png;base64,AA", 1, 1),
  SendFile: () => api.sendFile("ch", "data:text/plain;base64,AA", "a.txt"),
  SendGuildGif: () => api.sendGuildGif("ch", "gif1"),
  SendSearchedGif: () => api.sendSearchedGif("ch", "ref1"),
  // The held-file row's two choices and the recorder's voice clip
  // (lib/mediastrip.js, Composer.svelte).
  SendFileAsIs: () => api.sendFileAsIs("ch", "data:video/mp4;base64,AA", "a.mp4"),
  SendFileWithoutText: () => api.sendFileWithoutText("ch", "data:video/mp4;base64,AA", "a.mp4"),
  SendVoiceClip: () => api.sendVoiceClip("ch", "data:audio/ogg;base64,AA", "voice.ogg"),
};

for (const [method, send] of Object.entries(sends)) {
  order.length = 0;
  await send();
  const want = ["before:ch", `rpc:${method}`];
  if (JSON.stringify(order) !== JSON.stringify(want)) {
    console.error(`  FAIL: ${method}: got ${JSON.stringify(order)}, want ${JSON.stringify(want)}`);
    failures++;
  }
}

if (failures) {
  console.error(`beforesend: ${failures} failure(s)`);
  process.exit(1);
}
console.log("beforesend: all tests passed");
