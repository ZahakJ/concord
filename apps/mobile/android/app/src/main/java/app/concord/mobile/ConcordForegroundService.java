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
        // A START_STICKY restart hands back an EMPTY process: no WebView, no
        // JavaScript, nobody to call the plugin's start(). If the service
        // doesn't boot the core itself, the tray reads "Connected — you'll
        // receive messages" over a process where nothing is running — a lie
        // that holds until the user next opens the app. Booting here is what
        // makes the notification's promise true (and it's a no-op on the
        // normal path, where the plugin already started the node).
        try {
            NodeHolder.ensureStarted(getApplicationContext());
        } catch (Exception e) {
            // Without a core this service is only decoration — don't stand in
            // the tray promising a connection that doesn't exist.
            stopSelf();
            return START_NOT_STICKY;
        }
        // No multicast lock is taken. It would only be of use to mDNS, and mDNS
        // cannot start on Android: SELinux denies the netlink socket bind
        // zeroconf needs, so Host.startMDNS fails and the node discovers over
        // DHT + relay (see internal/net/host.go, "mDNS discovery unavailable").
        // Holding one would switch OFF the Wi-Fi chip's multicast filtering for
        // the entire life of the service, waking the CPU for every broadcast on
        // the network, in exchange for nothing. If mDNS ever does work here,
        // acquire the lock THEN — gated on the discovery having actually
        // started, not on hope.
        //
        // STICKY: if the OS reclaims us under heavy memory pressure, restart when
        // it can — the node re-establishes and drains the mailbox on restart.
        return START_STICKY;
    }

    /**
     * Android 15 gives a dataSync foreground service a budget — 6 hours in any
     * 24 — and when it runs out the framework calls this. A service that does
     * not override it is killed with an ANR, which is a crash in the user's
     * eyes and a bad-behaviour signal in Play's vitals. So the only question is
     * whether we go quietly.
     *
     * We go quietly: drop the notification and stop the service. What we do NOT
     * do is stop the node. It lives in this process, not in this service, and
     * the process survives as long as Android lets it — so messages keep
     * arriving for a while after the tray icon disappears. That is the honest
     * shape of the tradeoff: the platform has decided this app may not hold the
     * process up any longer, and no amount of restarting the service changes
     * that (an immediate restart would only burn the next budget window and
     * arrive back here). The next time the user opens the app, or the OS
     * delivers a push, the existing start paths put the service back.
     *
     * Deliberately not START_STICKY-restarted and deliberately not silent: the
     * notification is removed rather than left standing, because a tray entry
     * reading "Connected — you'll receive messages" over a process the OS is
     * now free to reap is exactly the lie onStartCommand's comment warns about.
     */
    @Override
    public void onTimeout(int startId, int fgsType) {
        retire();
    }

    /** API 34's single-argument form. Only ever fires for shortService, which
     *  this is not, but overriding it costs nothing and means a platform that
     *  routes the timeout here instead still gets a clean stop rather than an
     *  ANR. */
    @Override
    public void onTimeout(int startId) {
        retire();
    }

    private void retire() {
        stopForeground(Service.STOP_FOREGROUND_REMOVE); // API 24; minSdk is 26.
        stopSelf();
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
            // This one sits in the status bar permanently (the setting defaults
            // on), so it has to be Concord's own mark: the platform Bluetooth
            // glyph that used to be here read as the app doing something with
            // the radio.
            .setSmallIcon(R.drawable.ic_stat_concord)
            .setColor(ConcordCorePlugin.BRAND_TEAL)
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
