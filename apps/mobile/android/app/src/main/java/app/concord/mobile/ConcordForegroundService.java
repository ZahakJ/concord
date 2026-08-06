package app.concord.mobile;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.net.wifi.WifiManager;
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

    // Android filters inbound multicast at the Wi-Fi driver unless something
    // holds this lock, which makes libp2p's mDNS deaf: a phone on the same
    // network as the desktop never discovers it and has to go out through the
    // rendezvous instead. Held for as long as we keep the node alive.
    private WifiManager.MulticastLock multicastLock;

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
        // NOT acquiring a multicast lock. See acquireMulticastLock below.
        // STICKY: if the OS reclaims us under heavy memory pressure, restart when
        // it can — the node re-establishes and drains the mailbox on restart.
        return START_STICKY;
    }

    // DELIBERATELY UNUSED — kept so the reasoning survives the next person who
    // notices mDNS is off on Android and reaches for this.
    //
    // A Wi-Fi multicast lock switches OFF the chip's multicast filtering, so the
    // radio wakes the CPU for every broadcast and multicast packet on the
    // network — on a busy home or office LAN that is a constant, and it was held
    // for the entire life of the service, which is the entire time Concord is
    // running.
    //
    // It bought nothing. It exists for mDNS, and mDNS never starts on Android at
    // all: SELinux denies the netlink socket bind zeroconf needs, so
    // Host.startMDNS fails and the node logs and carries on over DHT + relay
    // (see internal/net/host.go, "mDNS discovery unavailable"). So this was pure
    // radio cost for a feature the platform does not permit.
    //
    // If mDNS ever does work on Android, acquire it THEN — gated on the discovery
    // actually having started, not on hope.
    @SuppressWarnings("unused")
    private void acquireMulticastLock() {
        if (multicastLock != null && multicastLock.isHeld()) return;
        try {
            WifiManager wm = (WifiManager)
                getApplicationContext().getSystemService(Context.WIFI_SERVICE);
            if (wm == null) return;
            multicastLock = wm.createMulticastLock("concord-mdns");
            multicastLock.setReferenceCounted(false);
            multicastLock.acquire();
        } catch (Exception e) {
            // Best effort: without it we simply fall back to the rendezvous,
            // which is how this behaved before. Not worth failing the service.
        }
    }

    @Override
    public void onDestroy() {
        if (multicastLock != null && multicastLock.isHeld()) {
            try {
                multicastLock.release();
            } catch (Exception ignored) {
            }
        }
        super.onDestroy();
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
