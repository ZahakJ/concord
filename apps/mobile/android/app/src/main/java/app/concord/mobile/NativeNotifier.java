package app.concord.mobile;

import android.content.Context;

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
 * can't read; guessing would notify for channels the user muted. For DMs the
 * page hands over its decision each time it goes to the background
 * (NotifyPolicy): Do Not Disturb, and the DMs whose level keeps them quiet.
 * (Guild @mention wakes are a separate, planned feature.)
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
            // A message deleted or expired leaves the tray entry that shows
            // it, in any conversation (the page does the same while it runs:
            // lib/notify.js retract).
            if (m.optBoolean("deleted", false)) {
                ConcordCorePlugin.retractMessageNotification(app, m.optString("channelId", ""), m.optString("id", ""));
                return;
            }
            // Same first rules as notify.js: never for system/app rows, never
            // for your own words echoing back.
            if (!m.optString("kind", "").isEmpty()) return;
            // History is not news. A catch-up after a week away, or the backlog
            // that follows a join, arrives through sync as ordinary message
            // events — and this sink runs precisely when the app is in the
            // background syncing, so every historical DM became a tray line
            // "as if just posted". The core marks those (MessageView.backfill);
            // the JS gate has always required a live row, and now so does this.
            if (m.optBoolean("backfill", false)) return;
            String sender = m.optString("sender", "");
            if (sender.isEmpty() || sender.equals(self())) return;

            String chId = m.optString("channelId", "");
            Map<String, String> dms = dmChannelMap();
            if (dms == null || !dms.containsKey(chId)) return;
            // The levels and Do Not Disturb, as the page last decided them: a
            // DM set to "Nothing" stays silent here too.
            if (!NotifyPolicy.allows(app, chId)) return;

            String title = m.optString("senderName", "");
            if (title.isEmpty()) title = sender.length() > 9 ? sender.substring(0, 9) : sender;
            ConcordCorePlugin.postMessageNotification(app, title, preview(m.optString("content", "")), chId, m.optString("id", ""));
            // Jayb's tile: one more unread here, counted without the page (partial).
            UnreadCard.bump(app, chId);
        } catch (Exception ignored) {
            // A malformed event must never take the sink down with it.
        }
    }

    // The plain-words body: content tokens ([image](concord://...)) collapse to
    // their labels, mirroring what previewText does for the JS notifications
    // (NotificationText).
    private static String preview(String content) {
        return NotificationText.preview(content);
    }

    private String self() {
        String fpr = selfFpr;
        if (!fpr.isEmpty()) return fpr;
        learn();
        return selfFpr;
    }

    private Map<String, String> dmChannelMap() {
        Map<String, String> dms = dmChannels;
        if (dms != null) return dms;
        learn();
        return dmChannels;
    }

    /**
     * Asks the core, in process, for our fingerprint and the DM channels. Not
     * through dispatchJSON: behind the App Lock gate the page's Identity and
     * Guilds are refused, and DM notifications must keep coming while the app
     * is locked (Node.notifyContextJSON answers there, and with nothing else).
     */
    private void learn() {
        try {
            NotifyContext parsed = parseContext(node.notifyContextJSON());
            if (parsed == null) return; // locked or booting: retry on the next message
            selfFpr = parsed.self;
            dmChannels = parsed.dms;
        } catch (Exception ignored) {
        }
    }

    static final class NotifyContext {
        final String self;
        final Map<String, String> dms;

        NotifyContext(String self, Map<String, String> dms) {
            this.self = self;
            this.dms = dms;
        }
    }

    /** {"self": fpr, "dms": {channelId: name}}; null for "" or anything malformed. */
    static NotifyContext parseContext(String json) {
        if (json == null || json.isEmpty()) return null;
        try {
            JSONObject o = new JSONObject(json);
            String self = o.optString("self", "");
            if (self.isEmpty()) return null;
            Map<String, String> dms = new HashMap<>();
            JSONObject m = o.optJSONObject("dms");
            if (m != null) {
                for (java.util.Iterator<String> it = m.keys(); it.hasNext(); ) {
                    String k = it.next();
                    dms.put(k, m.optString(k, ""));
                }
            }
            return new NotifyContext(self, dms);
        } catch (Exception e) {
            return null;
        }
    }
}
