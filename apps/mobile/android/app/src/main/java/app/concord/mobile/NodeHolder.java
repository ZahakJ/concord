package app.concord.mobile;

import android.content.Context;

import java.io.File;

import concord.Concord;
import concord.Node;

/**
 * The one owner of the in-process Go core. Both entry points route here: the
 * plugin's start() (normal launch, WebView asks) and the foreground service's
 * onStartCommand (a START_STICKY restart after the OS reclaimed the process —
 * a path where NO JavaScript will ever run, so if the service didn't boot the
 * core itself, the tray would say "Connected" over an empty process and no
 * message would arrive until the user next opened the app).
 *
 * The native message notifier is attached at boot, whichever door the core
 * came in through, so delivery doesn't depend on a live WebView.
 */
final class NodeHolder {
    private static Node node;

    private NodeHolder() {}

    /** Boots the core if it isn't running. Idempotent; safe from any thread. */
    static synchronized Node ensureStarted(Context ctx) throws Exception {
        if (node == null) {
            Context app = ctx.getApplicationContext();
            File dataDir = new File(app.getFilesDir(), "concord");
            node = Concord.start(dataDir.getAbsolutePath());
            NativeNotifier.attach(app, node);
            // Tell the core straight away whether these bytes are billed, and
            // keep telling it. Here rather than in the Activity or the
            // foreground service because this is the one place whose lifetime
            // is the core's own — see NetworkWatch.
            NetworkWatch.attach(app, node);
        }
        return node;
    }

    /** The running core, or null. Never boots. */
    static synchronized Node get() {
        return node;
    }

    static synchronized void stop() {
        if (node != null) {
            node.stop();
            node = null;
        }
    }
}
