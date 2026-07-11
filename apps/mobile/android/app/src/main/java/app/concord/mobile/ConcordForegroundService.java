package app.concord.mobile;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.os.Build;
import android.os.IBinder;

/**
 * A foreground service whose only job is to keep this process alive while the
 * app is backgrounded, so the in-process libp2p node stays connected and
 * receives messages (Android otherwise kills a backgrounded app within seconds).
 * The Go node itself runs in the same process (started by ConcordCorePlugin);
 * this service just holds the process up with a persistent, low-priority
 * notification. Toggled by the "Stay connected" setting via the plugin.
 */
public class ConcordForegroundService extends Service {
    private static final String CHANNEL_ID = "concord_connection";
    private static final int NOTIF_ID = 1;

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        startForeground(NOTIF_ID, buildNotification());
        // STICKY: if the OS reclaims us under heavy memory pressure, restart when
        // it can — the node re-establishes and drains the mailbox on restart.
        return START_STICKY;
    }

    private Notification buildNotification() {
        NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel ch = new NotificationChannel(
                CHANNEL_ID, "Connection", NotificationManager.IMPORTANCE_MIN);
            ch.setDescription("Keeps Concord connected so messages arrive.");
            ch.setShowBadge(false);
            nm.createNotificationChannel(ch);
        }

        Intent launch = new Intent(this, MainActivity.class);
        launch.setFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        int piFlags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            piFlags |= PendingIntent.FLAG_IMMUTABLE;
        }
        PendingIntent pi = PendingIntent.getActivity(this, 0, launch, piFlags);

        Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
            ? new Notification.Builder(this, CHANNEL_ID)
            : new Notification.Builder(this);
        return b
            .setContentTitle("Concord")
            .setContentText("Connected — you'll receive messages")
            .setSmallIcon(android.R.drawable.stat_sys_data_bluetooth) // replaced by app icon in assets pass
            .setContentIntent(pi)
            .setOngoing(true)
            .setPriority(Notification.PRIORITY_MIN)
            .build();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null; // not a bound service
    }

    // start/stop helpers the plugin calls.
    static void start(Context ctx) {
        Intent i = new Intent(ctx, ConcordForegroundService.class);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            ctx.startForegroundService(i);
        } else {
            ctx.startService(i);
        }
    }

    static void stop(Context ctx) {
        ctx.stopService(new Intent(ctx, ConcordForegroundService.class));
    }
}
