package app.concord.mobile;

import android.Manifest;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Person;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.os.Build;
import android.provider.Settings;
import android.view.WindowManager;

import androidx.core.app.NotificationManagerCompat;

import java.util.ArrayDeque;
import java.util.HashMap;
import java.util.Map;

import com.getcapacitor.JSObject;
import com.getcapacitor.PermissionState;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;
import com.getcapacitor.annotation.Permission;
import com.getcapacitor.annotation.PermissionCallback;

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
@CapacitorPlugin(
    name = "ConcordCore",
    permissions = {
        @Permission(alias = ConcordCorePlugin.NOTIF_ALIAS, strings = { Manifest.permission.POST_NOTIFICATIONS })
    }
)
public class ConcordCorePlugin extends Plugin {
    static final String NOTIF_ALIAS = "notifications";

    // The live plugin, so native code (the call service's Hang up action) can
    // hand an event to JS. Null when no bridge exists — callers must tolerate.
    private static volatile ConcordCorePlugin instance;

    @Override
    public void load() {
        instance = this;
        // A share that arrived before the bridge existed (cold start straight
        // from another app's share sheet) waits here for the first listener.
        String stashed = pendingShare;
        if (stashed != null) {
            pendingShare = null;
            emitShareIn(stashed);
        }
    }

    // ---- share-sheet intake ----
    // MainActivity hands shared text here; JS (App.svelte) listens for
    // "shareIn". retainUntilConsumed=true because on a cold start the intent
    // is processed long before App.svelte's onMount attaches the listener.
    private static volatile String pendingShare;

    static void emitShareIn(String text) {
        ConcordCorePlugin p = instance;
        if (p == null) {
            pendingShare = text;
            return;
        }
        JSObject data = new JSObject();
        data.put("text", text);
        p.notifyListeners("shareIn", data, true);
    }

    @PluginMethod
    public void start(PluginCall call) {
        try {
            Node node = NodeHolder.ensureStarted(getContext());
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
        NodeHolder.stop();
        call.resolve();
    }

    @PluginMethod
    public void nudge(PluginCall call) {
        Node n = NodeHolder.get();
        if (n != null) n.nudge();
        call.resolve();
    }

    // App visibility → core cadence. Called from MainActivity.onStart/onStop
    // (NOT from JS: the WebView's own timers are throttled precisely when this
    // matters, so the native lifecycle is the only reliable messenger). Off
    // screen — including screen-off — the Go core slows its periodic
    // discovery/sync loops so the radio can sleep; connections and message
    // delivery stay up. See Node.SetForeground.
    static void setForeground(boolean fg) {
        Node n = NodeHolder.get();
        if (n != null) n.setForeground(fg);
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

    // ---- call-scoped microphone service ----
    // Android 14+ blocks background mic capture without a microphone-type
    // foreground service; the voice lifecycle brackets every call with these.

    @PluginMethod
    public void startCallService(PluginCall call) {
        ConcordCallService.start(getContext());
        call.resolve();
    }

    @PluginMethod
    public void stopCallService(PluginCall call) {
        ConcordCallService.stop(getContext());
        call.resolve();
    }

    /** The call notification's Hang up button → a "hangup" event JS acts on. */
    static void emitHangup() {
        ConcordCorePlugin p = instance;
        if (p != null) p.notifyListeners("hangup", new JSObject());
    }

    // ---- local message notifications ----
    // Post a heads-up notification for a new message/mention. This is the mobile
    // counterpart to the web Notification API (which the Android WebView doesn't
    // surface to the tray): the JS gate in notify.js decides WHEN to call this;
    // here we just render it. No content leaves the device — it's already been
    // decrypted locally. Tapping opens the app. Needs no Firebase/push creds.
    private static final String MSG_CHANNEL_ID = "concord_messages";
    static final int MSG_NOTIF_ID = 2; // package-private: MarkReadReceiver cancels by it

    // In-memory conversation history behind MessagingStyle: the last few
    // (sender, text, time) lines per tag, so a second message stacks under the
    // first instead of overwriting it — the tag-replace behavior above used to
    // mean a busy channel's notification only ever showed its newest line.
    // Process-local on purpose: the tray itself survives a process restart and
    // simply restacks from one line again.
    private static final int HISTORY_MAX = 6;
    private static final long HISTORY_TTL_MS = 60L * 60 * 1000; // stale lines age out
    private static final Map<String, ArrayDeque<MsgLine>> HISTORY = new HashMap<>();

    private static final class MsgLine {
        final String sender;
        final String text;
        final long at;

        MsgLine(String sender, String text, long at) {
            this.sender = sender;
            this.text = text;
            this.at = at;
        }
    }

    /** Swiped away, tapped, or marked read: the stack is done with. */
    static void clearNotificationHistory(String tag) {
        synchronized (HISTORY) {
            HISTORY.remove(tag);
        }
    }

    @PluginMethod
    public void postNotification(PluginCall call) {
        postMessageNotification(
            getContext(),
            call.getString("title", "Concord"),
            call.getString("body", ""),
            // Per-conversation tag: a new message in a channel REPLACES the last
            // one for that channel instead of stacking a fresh alert each time.
            call.getString("tag", "concord"));
        call.resolve();
    }

    // Static so NativeNotifier can post through the exact same channel, intent
    // and tag scheme while the WebView is torn down — one path, one look, and
    // a JS-side and native-side post for the same conversation replace each
    // other instead of stacking.
    static void postMessageNotification(Context ctx, String title, String body, String tag) {
        NotificationManager nm =
            (NotificationManager) ctx.getSystemService(Context.NOTIFICATION_SERVICE);

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel ch = new NotificationChannel(
                MSG_CHANNEL_ID, "Messages", NotificationManager.IMPORTANCE_HIGH);
            ch.setDescription("New messages and mentions.");
            nm.createNotificationChannel(ch);
        }

        // ACTION_VIEW + concord://channel?id=<tag>: the tap has to land in the
        // conversation it announced. A bare launch intent dropped the user
        // wherever they last were, which on a phone — where the notification IS
        // the way into the app — cost a hamburger tap and a scroll every time.
        // Capacitor surfaces this to appUrlOpen (warm) and getLaunchUrl (cold);
        // lib/deeplink.js routes it.
        Intent launch = new Intent(ctx, MainActivity.class);
        launch.setAction(Intent.ACTION_VIEW);
        launch.setData(Uri.parse("concord://channel?id=" + Uri.encode(tag)));
        launch.setFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        int piFlags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            piFlags |= PendingIntent.FLAG_IMMUTABLE;
        }
        PendingIntent pi = PendingIntent.getActivity(ctx, tag.hashCode(), launch, piFlags);

        // Append to the tag's history and render the WHOLE stack, MessagingStyle:
        // sender names against their lines, newest at the bottom, like every
        // other messenger's tray entry. A copy is rendered outside the lock.
        long now = System.currentTimeMillis();
        ArrayDeque<MsgLine> lines;
        synchronized (HISTORY) {
            ArrayDeque<MsgLine> q = HISTORY.get(tag);
            if (q == null) HISTORY.put(tag, q = new ArrayDeque<>());
            while (!q.isEmpty()
                && (q.size() >= HISTORY_MAX || now - q.peekFirst().at > HISTORY_TTL_MS)) {
                q.pollFirst();
            }
            q.addLast(new MsgLine(title, body, now));
            lines = new ArrayDeque<>(q);
        }
        Notification.MessagingStyle style;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            // "You" is the device owner slot; we never add own-messages, so the
            // name is never rendered — the API just requires a user Person.
            style = new Notification.MessagingStyle(new Person.Builder().setName("You").build());
            for (MsgLine l : lines) {
                style.addMessage(new Notification.MessagingStyle.Message(
                    l.text, l.at, new Person.Builder().setName(l.sender).build()));
            }
        } else {
            style = new Notification.MessagingStyle("You");
            for (MsgLine l : lines) style.addMessage(l.text, l.at, l.sender);
        }

        // Swipe-away (and tap, via autoCancel) must clear the stacked history,
        // or a channel's next message resurrects lines the user already saw.
        Intent dismissed = new Intent(ctx, MarkReadReceiver.class)
            .setAction(MarkReadReceiver.ACTION_DISMISSED)
            .putExtra(MarkReadReceiver.EXTRA_TAG, tag);
        PendingIntent delPi = PendingIntent.getBroadcast(ctx, tag.hashCode(), dismissed, piFlags);
        // "Mark read" pushes the read cursor through the core (fans out to all
        // devices) without opening the app. Reply-from-tray (RemoteInput) is a
        // deliberate follow-up — the send path is the heavy half.
        Intent markRead = new Intent(ctx, MarkReadReceiver.class)
            .setAction(MarkReadReceiver.ACTION_MARK_READ)
            .putExtra(MarkReadReceiver.EXTRA_TAG, tag);
        PendingIntent mrPi = PendingIntent.getBroadcast(ctx, tag.hashCode(), markRead, piFlags);

        Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
            ? new Notification.Builder(ctx, MSG_CHANNEL_ID)
            : new Notification.Builder(ctx);
        Notification n = b
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(style)
            .setSmallIcon(R.drawable.ic_stat_concord)
            .setColor(BRAND_TEAL)
            .setContentIntent(pi)
            .setDeleteIntent(delPi)
            .addAction(new Notification.Action.Builder(
                (android.graphics.drawable.Icon) null, "Mark read", mrPi).build())
            .setAutoCancel(true)
            .setPriority(Notification.PRIORITY_HIGH)
            .build();

        try {
            nm.notify(tag, MSG_NOTIF_ID, n);
        } catch (SecurityException e) {
            // POST_NOTIFICATIONS not granted (Android 13+): silently no-op — the
            // in-app badge/chime still fired.
        }
    }

    /** Concord teal, for setColor on notifications. Mirrors --accent in app.css. */
    static final int BRAND_TEAL = 0xFF14A394;

    // ---- notification permission, asked at a moment that makes sense ----
    // This used to fire from MainActivity.onCreate, i.e. on top of the splash,
    // before the user had seen a single pixel. Android 13+ hard-denies after two
    // dismissals and there is no way back except system Settings, so a reflex
    // "no" there silently cost the user every message alert forever. The web
    // layer now asks once the account exists (lib/notify.js).

    @PluginMethod
    public void notificationStatus(PluginCall call) {
        JSObject r = new JSObject();
        boolean enabled = NotificationManagerCompat.from(getContext()).areNotificationsEnabled();
        r.put("enabled", enabled);
        // canRequest false + enabled false means the system dialog will no longer
        // appear — the only route left is Settings, which is what the UI must say.
        boolean canRequest = Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU
            || getPermissionState(NOTIF_ALIAS) == PermissionState.PROMPT
            || getPermissionState(NOTIF_ALIAS) == PermissionState.PROMPT_WITH_RATIONALE;
        r.put("canRequest", !enabled && canRequest);
        call.resolve(r);
    }

    @PluginMethod
    public void requestNotifications(PluginCall call) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU
            || getPermissionState(NOTIF_ALIAS) == PermissionState.GRANTED) {
            notificationStatus(call);
            return;
        }
        requestPermissionForAlias(NOTIF_ALIAS, call, "notifPermissionResult");
    }

    @PermissionCallback
    private void notifPermissionResult(PluginCall call) {
        notificationStatus(call);
    }

    /** Opens this app's system settings page — the only recovery from a hard deny. */
    @PluginMethod
    public void openAppSettings(PluginCall call) {
        try {
            Intent i = new Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
                Uri.fromParts("package", getContext().getPackageName(), null));
            i.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
            getContext().startActivity(i);
            call.resolve();
        } catch (Exception e) {
            call.reject("couldn't open settings", e);
        }
    }

    // ---- window-level bits the web layer owns the policy for ----

    /** App Lock on → FLAG_SECURE, so the recents thumbnail and screenshots blank. */
    @PluginMethod
    public void setSecure(PluginCall call) {
        boolean on = Boolean.TRUE.equals(call.getBoolean("secure", false));
        if (getActivity() instanceof MainActivity) ((MainActivity) getActivity()).applySecureFlag(on);
        call.resolve();
    }

    /** Match the system bar icons to Concord's own theme, not the phone's. */
    @PluginMethod
    public void setSystemBarStyle(PluginCall call) {
        boolean light = Boolean.TRUE.equals(call.getBoolean("light", false));
        if (getActivity() instanceof MainActivity) ((MainActivity) getActivity()).applySystemBarStyle(light);
        call.resolve();
    }

    /** First paint: lets the launch splash hand over instead of flashing blank. */
    @PluginMethod
    public void appReady(PluginCall call) {
        MainActivity.markWebReady();
        call.resolve();
    }

    /**
     * FLAG_KEEP_SCREEN_ON while something is worth watching hands-free (a video
     * tile, a screen share, the QR scanner held up to another screen). The web
     * navigator.wakeLock covers calls, but the WebView drops it silently on
     * visibility changes; the window flag has no such failure mode.
     */
    @PluginMethod
    public void setKeepAwake(PluginCall call) {
        boolean on = Boolean.TRUE.equals(call.getBoolean("on", false));
        android.app.Activity act = getActivity();
        if (act != null) {
            act.runOnUiThread(() -> {
                if (on) act.getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
                else act.getWindow().clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
            });
        }
        call.resolve();
    }
}

