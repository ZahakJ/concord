package app.concord.mobile;

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
}

