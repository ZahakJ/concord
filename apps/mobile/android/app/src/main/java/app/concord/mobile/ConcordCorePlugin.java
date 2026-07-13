package app.concord.mobile;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.os.Build;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

import java.io.File;

import concord.Concord;
import concord.Node;

/**
 * ConcordCore boots the Go core (from concord.aar, built by `make
 * android-core`) inside this process and hands the webview the loopback
 * port + bearer token it needs to reach /rpc and /events. The frontend calls
 * start() once from main.js before mounting.
 *
 * The data dir lives under getFilesDir() and MUST stay out of OS backups
 * (allowBackup=false in the manifest): restoring MLS ratchet state onto
 * another install forks the group state irrecoverably.
 */
@CapacitorPlugin(name = "ConcordCore")
public class ConcordCorePlugin extends Plugin {
    private static Node node; // survives activity recreation; one core per process

    @PluginMethod
    public void start(PluginCall call) {
        try {
            synchronized (ConcordCorePlugin.class) {
                if (node == null) {
                    File dataDir = new File(getContext().getFilesDir(), "concord");
                    node = Concord.start(dataDir.getAbsolutePath());
                }
            }
            if (app.concord.mobile.BuildConfig.DEBUG) {
                // Debug builds only: lets a USB-connected dev drive the loopback
                // API (e.g. to orchestrate/verify a link). Never in release.
                android.util.Log.i("ConcordCore",
                    "loopback API 127.0.0.1:" + node.port() + " token=" + node.token());
            }
            JSObject ret = new JSObject();
            ret.put("port", node.port());
            ret.put("token", node.token());
            call.resolve(ret);
        } catch (Exception e) {
            call.reject("failed to start core: " + e.getMessage(), e);
        }
    }

    @PluginMethod
    public void stop(PluginCall call) {
        synchronized (ConcordCorePlugin.class) {
            if (node != null) {
                node.stop();
                node = null;
            }
        }
        call.resolve();
    }

    @PluginMethod
    public void nudge(PluginCall call) {
        Node n = node;
        if (n != null) n.nudge();
        call.resolve();
    }

    // "Stay connected": run a foreground service so Android keeps this process
    // (and the in-process libp2p node) alive while backgrounded.
    @PluginMethod
    public void startBackground(PluginCall call) {
        ConcordForegroundService.start(getContext());
        call.resolve();
    }

    @PluginMethod
    public void stopBackground(PluginCall call) {
        ConcordForegroundService.stop(getContext());
        call.resolve();
    }

    // ---- local message notifications ----
    // Post a heads-up notification for a new message/mention. This is the mobile
    // counterpart to the web Notification API (which the Android WebView doesn't
    // surface to the tray): the JS gate in notify.js decides WHEN to call this;
    // here we just render it. No content leaves the device — it's already been
    // decrypted locally. Tapping opens the app. Needs no Firebase/push creds.
    private static final String MSG_CHANNEL_ID = "concord_messages";
    private static final int MSG_NOTIF_ID = 2;

    @PluginMethod
    public void postNotification(PluginCall call) {
        String title = call.getString("title", "Concord");
        String body = call.getString("body", "");
        // Per-conversation tag: a new message in a channel REPLACES the last one
        // for that channel instead of stacking a fresh alert each time.
        String tag = call.getString("tag", "concord");

        Context ctx = getContext();
        NotificationManager nm =
            (NotificationManager) ctx.getSystemService(Context.NOTIFICATION_SERVICE);

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel ch = new NotificationChannel(
                MSG_CHANNEL_ID, "Messages", NotificationManager.IMPORTANCE_HIGH);
            ch.setDescription("New messages and mentions.");
            nm.createNotificationChannel(ch);
        }

        Intent launch = new Intent(ctx, MainActivity.class);
        launch.setFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        int piFlags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            piFlags |= PendingIntent.FLAG_IMMUTABLE;
        }
        PendingIntent pi = PendingIntent.getActivity(ctx, tag.hashCode(), launch, piFlags);

        Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
            ? new Notification.Builder(ctx, MSG_CHANNEL_ID)
            : new Notification.Builder(ctx);
        Notification n = b
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(new Notification.BigTextStyle().bigText(body))
            .setSmallIcon(android.R.drawable.stat_notify_chat)
            .setContentIntent(pi)
            .setAutoCancel(true)
            .setPriority(Notification.PRIORITY_HIGH)
            .build();

        try {
            nm.notify(tag, MSG_NOTIF_ID, n);
        } catch (SecurityException e) {
            // POST_NOTIFICATIONS not granted (Android 13+): silently no-op — the
            // in-app badge/chime still fired.
        }
        call.resolve();
    }
}

