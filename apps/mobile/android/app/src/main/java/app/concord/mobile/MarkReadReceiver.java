package app.concord.mobile;

import android.app.KeyguardManager;
import android.app.NotificationManager;
import android.app.RemoteInput;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.os.Build;
import android.os.Bundle;

import org.json.JSONArray;
import org.json.JSONObject;

import concord.Node;

/**
 * Tray-side actions on a message notification (tag == channelId). Three intents
 * land here:
 *
 *   ACTION_MARK_READ — the "Mark read" button: push the read cursor for the
 *       channel through the Go core (Bridge.MarkRead(channelID, unixMilli),
 *       which fans out to every session/device), then drop the notification.
 *   ACTION_REPLY — the "Reply" button's inline text field: send the text into
 *       the channel through the core (Bridge.SendMessage(channelID, content,
 *       replyTo, dir)), mark the channel read, and re-post the notification
 *       with the reply in its stack — or, if the send failed, with the reply
 *       marked as not sent.
 *   ACTION_DISMISSED — the notification was swiped away or auto-cancelled by
 *       a tap: forget its stacked MessagingStyle history so the next message
 *       starts a fresh stack instead of resurrecting old lines.
 */
public class MarkReadReceiver extends BroadcastReceiver {
    static final String ACTION_MARK_READ = "app.concord.mobile.action.MARK_READ";
    static final String ACTION_REPLY = "app.concord.mobile.action.REPLY";
    static final String ACTION_DISMISSED = "app.concord.mobile.action.NOTIF_DISMISSED";
    static final String EXTRA_TAG = "tag";

    @Override
    public void onReceive(Context ctx, Intent intent) {
        String tag = intent.getStringExtra(EXTRA_TAG);
        if (tag == null || tag.isEmpty()) return;
        if (ACTION_REPLY.equals(intent.getAction())) {
            reply(ctx, intent, tag);
            return;
        }
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
                markRead(tag);
            } catch (Exception ignored) {
                // Core not up (process restarted under a stale tray): the
                // notification is gone either way; read state syncs on open.
            } finally {
                pr.finish();
            }
        }, "concord-markread").start();
    }

    private static void markRead(String tag) throws Exception {
        Node n = NodeHolder.get();
        if (n != null) {
            String args = new JSONArray().put(tag).put(System.currentTimeMillis()).toString();
            n.dispatchJSON("MarkRead", args);
        }
    }

    private void reply(Context ctx, Intent intent, String tag) {
        Bundle results = RemoteInput.getResultsFromIntent(intent);
        CharSequence typed = results != null ? results.getCharSequence(ConcordCorePlugin.REPLY_KEY) : null;
        final String text = typed != null ? typed.toString().trim() : "";

        // Below API 31 the action cannot demand an unlocked device by itself
        // (setAuthenticationRequired is 31+), so refuse here instead: a reply
        // typed on the lock screen is not sent as the user. It stays in the
        // tray marked as not sent, so it is not lost either.
        boolean lockedOut = false;
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) {
            KeyguardManager km = (KeyguardManager) ctx.getSystemService(Context.KEYGUARD_SERVICE);
            lockedOut = km != null && km.isKeyguardLocked();
        }
        if (text.isEmpty() || lockedOut) {
            // Nothing to send, but the field is spinning until the
            // notification is updated — re-post it either way.
            if (text.isEmpty()) ConcordCorePlugin.repostQuietly(ctx, tag);
            else ConcordCorePlugin.postReplyResult(ctx, tag, text, false);
            return;
        }

        final Context app = ctx.getApplicationContext();
        final PendingResult pr = goAsync();
        new Thread(() -> {
            boolean sent = false;
            try {
                // Not booted on demand: a core that is down here is a process
                // the OS restarted without the service, or an identity that is
                // locked — neither can send from the tray. The failed state
                // says so instead of the words vanishing.
                Node n = NodeHolder.get();
                if (n != null) {
                    String args = new JSONArray().put(tag).put(text).put("").put("").toString();
                    JSONObject r = new JSONObject(n.dispatchJSON("SendMessage", args));
                    sent = !r.has("error");
                    if (sent) {
                        try {
                            markRead(tag); // answering a conversation is reading it
                        } catch (Exception ignored) {
                            // read state syncs on next open
                        }
                    }
                }
            } catch (Exception ignored) {
                // Falls through to the failed state. Deliberately not logged:
                // the exception can carry the message it failed to send.
            } finally {
                try {
                    ConcordCorePlugin.postReplyResult(app, tag, text, sent);
                } finally {
                    pr.finish();
                }
            }
        }, "concord-reply").start();
    }
}
