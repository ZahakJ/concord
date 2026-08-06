package app.concord.mobile;

import android.content.Context;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

import concord.Node;

/**
 * Native message notifications for the states where no JavaScript runs: the
 * WebView torn down, or the whole process restarted by the sticky service.
 * While the activity is visible the JS gate (lib/notify.js + per-channel
 * notification levels) owns the decision entirely — this class stands down.
 *
 * Scope is deliberately DMs only. Guild traffic is governed by per-channel
 * notification levels that live in the WebView's storage, which this side
 * can't read; guessing would notify for channels the user muted. A DM is the
 * one conversation kind where "someone wrote to you personally" is always
 * worth a tray line. (Guild @mention wakes are a separate, planned feature.)
 *
 * Both this path and the JS path post under tag=channelId with the same
 * notification id, so a race between them replaces rather than duplicates.
 */
final class NativeNotifier implements concord.EventSink {
    private final Context app;
    private final Node node;
    // Events arrive on Go threads; JSON work and the occasional bridge
    // round-trip hop here so the core is never blocked on notification policy.
    private final ExecutorService exec = Executors.newSingleThreadExecutor();

    // channelId -> guild name, DM-kind guilds only. Dropped on guild-updated;
    // rebuilt lazily from the bridge on the next message.
    private volatile Map<String, String> dmChannels = null;
    private volatile String selfFpr = "";

    private NativeNotifier(Context app, Node node) {
        this.app = app;
        this.node = node;
    }

    static void attach(Context app, Node node) {
        node.setEventSink(new NativeNotifier(app, node));
    }

    @Override
    public void onEvent(String name, String data) {
        if ("guild-updated".equals(name)) {
            dmChannels = null; // membership or channels moved; relearn lazily
            return;
        }
        if (!"message".equals(name)) return;
        if (MainActivity.isVisible()) return; // the JS gate owns it while the app is up
        exec.execute(() -> handleMessage(data));
    }

    private void handleMessage(String data) {
        try {
            JSONObject m = new JSONObject(data);
            // Same first rules as notify.js: never for system/app rows, never
            // for deletions, never for your own words echoing back.
            if (!m.optString("kind", "").isEmpty()) return;
            if (m.optBoolean("deleted", false)) return;
            String sender = m.optString("sender", "");
            if (sender.isEmpty() || sender.equals(self())) return;

            String chId = m.optString("channelId", "");
            Map<String, String> dms = dmChannelMap();
            if (dms == null || !dms.containsKey(chId)) return;

            String title = m.optString("senderName", "");
            if (title.isEmpty()) title = sender.length() > 9 ? sender.substring(0, 9) : sender;
            ConcordCorePlugin.postMessageNotification(app, title, preview(m.optString("content", "")), chId);
        } catch (Exception ignored) {
            // A malformed event must never take the sink down with it.
        }
    }

    // The plain-words body: content tokens ([image](concord://...)) collapse to
    // their labels, mirroring what previewText does for the JS notifications.
    private static String preview(String content) {
        String s = content.replaceAll("\\[([^\\]]*)\\]\\(concord://[^)]*\\)", "$1").trim();
        if (s.length() > 300) s = s.substring(0, 300) + "…";
        return s.isEmpty() ? "Sent you something" : s;
    }

    private String self() {
        String fpr = selfFpr;
        if (!fpr.isEmpty()) return fpr;
        try {
            JSONObject r = new JSONObject(node.dispatchJSON("Identity", "[]"));
            fpr = r.optJSONObject("result") != null
                ? r.getJSONObject("result").optString("fingerprint", "")
                : "";
        } catch (Exception e) {
            fpr = ""; // locked or booting: retry on the next message
        }
        selfFpr = fpr;
        return fpr;
    }

    private Map<String, String> dmChannelMap() {
        Map<String, String> dms = dmChannels;
        if (dms != null) return dms;
        try {
            JSONObject r = new JSONObject(node.dispatchJSON("Guilds", "[]"));
            JSONArray guilds = r.optJSONArray("result");
            if (guilds == null) return null; // identity locked: stay silent
            Map<String, String> next = new HashMap<>();
            for (int i = 0; i < guilds.length(); i++) {
                JSONObject g = guilds.getJSONObject(i);
                if (!"dm".equals(g.optString("kind", ""))) continue;
                JSONArray chans = g.optJSONArray("channels");
                if (chans == null) continue;
                for (int j = 0; j < chans.length(); j++) {
                    next.put(chans.getJSONObject(j).optString("id", ""), g.optString("name", ""));
                }
            }
            dmChannels = next;
            return next;
        } catch (Exception e) {
            return null;
        }
    }
}
