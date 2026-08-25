package app.concord.mobile;

import android.content.Context;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;

import concord.Node;

/**
 * Tells the core whether the bytes it is about to spend are billed.
 *
 * Concord's periodic peer discovery is a Kademlia walk — many small datagrams to
 * many hosts — and that is both the most expensive traffic shape for a cellular
 * modem and the easiest to slow down without anyone noticing, because it is the
 * search for peers we do NOT have. Nothing here touches delivery: messages,
 * mailbox drains and sync run at the same cadence on any network.
 *
 * <p>Registered from {@link NodeHolder} rather than from an Activity or from
 * {@code ConcordForegroundService}, because those are both shorter-lived than
 * the thing that needs the answer. The Activity is destroyed whenever the app
 * is backgrounded, and the foreground service only runs while the "Stay
 * connected" preference is on — a user who turns it off would otherwise be the
 * one user whose data plan we never learn about. The core is process-scoped and
 * so is this: one callback, on the application context, for the life of the
 * node.
 */
final class NetworkWatch {
    private static ConnectivityManager.NetworkCallback callback;

    private NetworkWatch() {}

    /**
     * Reports the current network to the node and starts watching for changes.
     * Idempotent; safe to call from any thread. Never throws: a device that
     * denies us ConnectivityManager should run Concord exactly as before, on
     * the unmetered default, rather than fail to start.
     */
    static synchronized void attach(Context ctx, Node node) {
        if (callback != null) return;
        ConnectivityManager cm = ctx.getSystemService(ConnectivityManager.class);
        if (cm == null) return;

        // The initial read matters on its own. registerDefaultNetworkCallback
        // does deliver the current network shortly after registration, but
        // "shortly" is asynchronous and the node starts walking the DHT
        // immediately — so the first minute on cellular, the exact minute a
        // cold start is at its most talkative, would be paid at the Wi-Fi
        // cadence.
        try {
            node.setMetered(isMetered(cm.getNetworkCapabilities(cm.getActiveNetwork())));
        } catch (Exception e) {
            return;
        }

        ConnectivityManager.NetworkCallback cb = new ConnectivityManager.NetworkCallback() {
            @Override
            public void onCapabilitiesChanged(Network network, NetworkCapabilities caps) {
                report(caps);
            }

            @Override
            public void onLost(Network network) {
                // No default network. Not a reason to hurry: the frugal answer
                // is the right one for "we don't know", and the core's own
                // offline handling (netKick on the way back) is what restores
                // the eager cadence.
                report(null);
            }
        };
        try {
            cm.registerDefaultNetworkCallback(cb);
            callback = cb;
        } catch (Exception e) {
            // SecurityException on a device that refuses the permission, or the
            // platform's "too many callbacks" IllegalArgumentException. The
            // startup reading above still stands; we simply won't hear changes.
        }
    }

    /**
     * Whether a network is billed by the byte. Absence of the capability — not
     * merely its negation — is what means metered: caps are null when there is
     * no default network at all (airplane mode, between handovers), and the
     * safe answer for "we don't know" is the frugal one.
     */
    static boolean isMetered(NetworkCapabilities caps) {
        return caps == null || !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED);
    }

    /** Pushes a fresh reading at the core, if there is a core to push it at. */
    static void report(NetworkCapabilities caps) {
        Node n = NodeHolder.get();
        if (n == null) return;
        try {
            n.setMetered(isMetered(caps));
        } catch (Exception e) {
            // The core is going away underneath us; the next boot re-reads.
        }
    }
}
