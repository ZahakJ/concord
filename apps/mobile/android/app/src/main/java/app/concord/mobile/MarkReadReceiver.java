package app.concord.mobile;

import android.app.NotificationManager;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

import org.json.JSONArray;

import concord.Node;

/**
 * Tray-side actions on a message notification (tag == channelId). Two intents
 * land here:
 *
 *   ACTION_MARK_READ — the "Mark read" button: push the read cursor for the
 *       channel through the Go core (Bridge.MarkRead(channelID, unixMilli),
 *       which fans out to every session/device), then drop the notification.
 *   ACTION_DISMISSED — the notification was swiped away or auto-cancelled by
 *       a tap: forget its stacked MessagingStyle history so the next message
 *       starts a fresh stack instead of resurrecting old lines.
 */
public class MarkReadReceiver extends BroadcastReceiver {
    static final String ACTION_MARK_READ = "app.concord.mobile.action.MARK_READ";
    static final String ACTION_DISMISSED = "app.concord.mobile.action.NOTIF_DISMISSED";
    static final String EXTRA_TAG = "tag";

    @Override
    public void onReceive(Context ctx, Intent intent) {
        String tag = intent.getStringExtra(EXTRA_TAG);
        if (tag == null || tag.isEmpty()) return;
        ConcordCorePlugin.clearNotificationHistory(tag);
        if (!ACTION_MARK_READ.equals(intent.getAction())) return;

        NotificationManager nm =
            (NotificationManager) ctx.getSystemService(Context.NOTIFICATION_SERVICE);
        if (nm != null) nm.cancel(tag, ConcordCorePlugin.MSG_NOTIF_ID);

        // The bridge call can hit disk and the network, and onReceive runs on
        // the main thread — hop off it and hold the broadcast open until done.
        final PendingResult pr = goAsync();
        new Thread(() -> {
            try {
                Node n = NodeHolder.get();
                if (n != null) {
                    String args = new JSONArray().put(tag).put(System.currentTimeMillis()).toString();
                    n.dispatchJSON("MarkRead", args);
                }
            } catch (Exception ignored) {
                // Core not up (process restarted under a stale tray): the
                // notification is gone either way; read state syncs on open.
            } finally {
                pr.finish();
            }
        }, "concord-markread").start();
    }
}
