package app.concord.mobile;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.os.Build;
import android.os.IBinder;

/**
 * Call-scoped foreground service of type "microphone". Android 14+ hard-blocks
 * microphone capture for backgrounded apps unless exactly this is running, so
 * without it, switching apps or turning the screen off mid-call silently
 * delivered dead air to the room — the other side heard you drop with no error
 * anywhere. Runs only for the duration of a call (started on join, stopped on
 * leave, both from the voice lifecycle in App.svelte via the plugin), with an
 * ongoing notification that names the state and offers a hang-up.
 */
public class ConcordCallService extends Service {
    private static final String CHANNEL_ID = "concord_call";
    private static final int NOTIF_ID = 3;
    static final String ACTION_HANGUP = "app.concord.mobile.CALL_HANGUP";

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && ACTION_HANGUP.equals(intent.getAction())) {
            // The notification's Hang up button. The WebView owns the actual
            // call teardown (mesh, signalling), so relay to JS and get out of
            // the way; leaveVoice() will call stop() on us in turn.
            ConcordCorePlugin.emitHangup();
            stopSelf();
            return START_NOT_STICKY;
        }
        Notification n = buildNotification();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE);
        } else {
            startForeground(NOTIF_ID, n);
        }
        // NOT sticky: a call that died with the process is over; resurrecting
        // the notification for it would announce a call nobody is on.
        return START_NOT_STICKY;
    }

    private Notification buildNotification() {
        NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel ch = new NotificationChannel(
                CHANNEL_ID, "Calls", NotificationManager.IMPORTANCE_LOW);
            ch.setDescription("Shown during a voice call so the microphone keeps working in the background.");
            ch.setShowBadge(false);
            nm.createNotificationChannel(ch);
        }

        int piFlags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            piFlags |= PendingIntent.FLAG_IMMUTABLE;
        }

        Intent launch = new Intent(this, MainActivity.class);
        launch.setFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        PendingIntent open = PendingIntent.getActivity(this, 0, launch, piFlags);

        Intent hangup = new Intent(this, ConcordCallService.class).setAction(ACTION_HANGUP);
        PendingIntent hangupPi = PendingIntent.getService(this, 1, hangup, piFlags);

        Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
            ? new Notification.Builder(this, CHANNEL_ID)
            : new Notification.Builder(this);
        return b
            .setContentTitle("In a call")
            .setContentText("Tap to return to Concord")
            .setSmallIcon(R.drawable.ic_stat_concord)
            .setColor(ConcordCorePlugin.BRAND_TEAL)
            .setContentIntent(open)
            .addAction(new Notification.Action.Builder(null, "Hang up", hangupPi).build())
            .setOngoing(true)
            .setCategory(Notification.CATEGORY_CALL)
            .build();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    static void start(Context ctx) {
        Intent i = new Intent(ctx, ConcordCallService.class);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            ctx.startForegroundService(i);
        } else {
            ctx.startService(i);
        }
    }

    static void stop(Context ctx) {
        ctx.stopService(new Intent(ctx, ConcordCallService.class));
    }
}
