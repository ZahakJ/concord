package app.concord.mobile;

import android.Manifest;
import android.app.Activity;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Person;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.content.ContentValues;
import android.os.Build;
import android.os.Environment;
import android.provider.MediaStore;
import android.provider.Settings;
import android.util.Base64;
import android.view.WindowManager;

import androidx.activity.result.ActivityResult;
import androidx.core.app.NotificationManagerCompat;

import java.io.OutputStream;
import java.util.ArrayDeque;
import java.util.HashMap;
import java.util.Map;

import com.getcapacitor.JSObject;
import com.getcapacitor.PermissionState;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.ActivityCallback;
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

    /**
     * Whether this build can actually talk to Firebase Cloud Messaging.
     *
     * The frontend must know, because registering for push without a
     * configuration is not a recoverable error: PushNotifications.register()
     * reaches FirebaseMessaging, which throws on a handler thread where no JS
     * try/catch can reach it, and the app dies. That is why the registration
     * path was gated behind a hand-set window.__CONCORD_PUSH global — a switch
     * somebody had to remember to flip, in a second place, at build time.
     *
     * This asks the build itself instead. The google-services Gradle plugin is
     * applied only when app/google-services.json exists (see app/build.gradle)
     * and its whole job is to turn that file into string resources, of which
     * google_app_id is the one FirebaseApp's own default initialisation looks
     * for. Present and non-empty means Firebase can start; absent means the
     * plugin never ran. Reading the resource rather than calling into Firebase
     * keeps this free of a compile-time dependency on a library that is only
     * present transitively, and cannot itself be the thing that throws.
     *
     * Fail-safe in both directions: any doubt resolves to false, which is
     * precisely today's behaviour — no registration, no crash, and delivery
     * still working over live sockets and drain-on-open.
     *
     * See docs/PUSH.md for what dropping the file in switches on.
     */
    @PluginMethod
    public void pushAvailable(PluginCall call) {
        JSObject ret = new JSObject();
        ret.put("available", firebaseConfigured());
        call.resolve(ret);
    }

    /**
     * Who installed this copy of the app.
     *
     * Concord can update itself: it downloads a signed APK from a peer or from
     * the release feed and hands it to the package installer. That is the whole
     * point of a build somebody sideloaded — there is no other way for them to
     * get a fix. It is also, word for word, what Play's Device and Network
     * Abuse policy forbids an app distributed through Play from doing, and the
     * penalty is suspension rather than a rejected upload.
     *
     * The two cases are the same binary, so the build cannot decide this; only
     * the running install knows where it came from. "com.android.vending" is
     * Play. Anything else — a file manager, adb, F-Droid, or null for a plain
     * sideload — is a copy whose user has no store to update them, and they
     * keep the feature.
     *
     * Resolves {installer: string|null}. Any failure resolves null, which the
     * frontend reads as "not Play" and therefore leaves self-update on. That
     * fail-open direction is deliberate: the alternative fails a sideloaded
     * user into having no update path at all, and a Play install that somehow
     * threw here would still be caught by the store's own review.
     */
    @PluginMethod
    public void installerSource(PluginCall call) {
        JSObject ret = new JSObject();
        ret.put("installer", installerPackage());
        call.resolve(ret);
    }

    private String installerPackage() {
        try {
            Context ctx = getContext();
            String self = ctx.getPackageName();
            android.content.pm.PackageManager pm = ctx.getPackageManager();
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                // getInstallerPackageName is deprecated from API 30 and returns
                // null for some installs it used to name.
                return pm.getInstallSourceInfo(self).getInstallingPackageName();
            }
            return pm.getInstallerPackageName(self);
        } catch (Exception e) {
            return null;
        }
    }

    private boolean firebaseConfigured() {
        try {
            Context ctx = getContext();
            int id = ctx.getResources().getIdentifier("google_app_id", "string", ctx.getPackageName());
            return id != 0 && !ctx.getString(id).trim().isEmpty();
        } catch (Exception e) {
            return false;
        }
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

    // ---- handing the user a file ----
    //
    // `<a download>` is a SILENT no-op in this WebView. Nothing registers a
    // DownloadListener, so a click on a blob: URL with a download attribute
    // produces no file, no error and no clue — which is what every export in
    // the app was doing on Android, and what "Save Image" still was.
    //
    // Two routes, because the two kinds of file want different homes. A
    // document (a report, a history export, an encrypted backup) goes through
    // the storage-access framework: the user picks where it lands, no
    // permission is involved, and it is the only way to reach the folder they
    // actually want. An image goes to the shared Pictures collection instead —
    // a picture saved out of a chat belongs in the gallery, and asking someone
    // to choose a directory for it is a worse answer than putting it where
    // every other app puts theirs. MediaStore has needed no permission for
    // that since API 29, and below that the app's own Pictures directory is
    // still visible to the user.

    /** Bytes from a base64 payload, tolerating a whole `data:` URL. */
    private static byte[] payload(String data) {
        if (data == null) return new byte[0];
        int comma = data.startsWith("data:") ? data.indexOf(',') : -1;
        return Base64.decode(comma >= 0 ? data.substring(comma + 1) : data, Base64.DEFAULT);
    }

    /**
     * Opens the system "save as" sheet for a document. Resolves
     * { saved: true, where } once the bytes are written, or { saved: false }
     * if the user backed out — a cancel is not a failure and must not be
     * reported as one.
     */
    @PluginMethod
    public void saveFile(PluginCall call) {
        String name = call.getString("filename", "concord-export");
        String mime = call.getString("mime", "application/octet-stream");
        try {
            Intent i = new Intent(Intent.ACTION_CREATE_DOCUMENT);
            i.addCategory(Intent.CATEGORY_OPENABLE);
            i.setType(mime);
            i.putExtra(Intent.EXTRA_TITLE, name);
            startActivityForResult(call, i, "saveFileResult");
        } catch (Exception e) {
            call.reject("no app on this device can save a file", e);
        }
    }

    @ActivityCallback
    private void saveFileResult(PluginCall call, ActivityResult result) {
        if (call == null) return;
        JSObject r = new JSObject();
        Uri target = result != null && result.getData() != null ? result.getData().getData() : null;
        if (result == null || result.getResultCode() != Activity.RESULT_OK || target == null) {
            r.put("saved", false);
            call.resolve(r);
            return;
        }
        try (OutputStream os = getContext().getContentResolver().openOutputStream(target)) {
            if (os == null) throw new java.io.IOException("no stream for " + target);
            os.write(payload(call.getString("data", "")));
            os.flush();
        } catch (Exception e) {
            call.reject("couldn't write the file", e);
            return;
        }
        r.put("saved", true);
        r.put("where", "file");
        call.resolve(r);
    }

    /**
     * Writes an image into the shared Pictures collection, under a Concord
     * folder, and makes it visible to the gallery. No picker and no
     * permission: this is the one destination a saved picture is ever wanted
     * in, and IS_PENDING keeps a half-written file out of the gallery if the
     * write dies partway.
     */
    @PluginMethod
    public void saveImage(PluginCall call) {
        String name = call.getString("filename", "concord-image.png");
        String mime = call.getString("mime", "image/png");
        ContentValues v = new ContentValues();
        v.put(MediaStore.Images.Media.DISPLAY_NAME, name);
        v.put(MediaStore.Images.Media.MIME_TYPE, mime);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            v.put(MediaStore.Images.Media.RELATIVE_PATH, Environment.DIRECTORY_PICTURES + "/Concord");
            v.put(MediaStore.Images.Media.IS_PENDING, 1);
        }
        Uri target = null;
        try {
            target = getContext().getContentResolver().insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, v);
            if (target == null) throw new java.io.IOException("MediaStore refused the insert");
            try (OutputStream os = getContext().getContentResolver().openOutputStream(target)) {
                if (os == null) throw new java.io.IOException("no stream for " + target);
                os.write(payload(call.getString("data", "")));
                os.flush();
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                v.clear();
                v.put(MediaStore.Images.Media.IS_PENDING, 0);
                getContext().getContentResolver().update(target, v, null, null);
            }
        } catch (Exception e) {
            // A pending row with nothing in it would sit in the gallery as a
            // broken thumbnail forever.
            if (target != null) {
                try {
                    getContext().getContentResolver().delete(target, null, null);
                } catch (Exception ignored) {
                }
            }
            call.reject("couldn't save the picture", e);
            return;
        }
        JSObject r = new JSObject();
        r.put("saved", true);
        r.put("where", "gallery");
        call.resolve(r);
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

