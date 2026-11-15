package app.concord.mobile;

import android.content.Context;
import android.content.SharedPreferences;

import java.util.Collections;
import java.util.HashSet;
import java.util.Set;

/**
 * The page's notification decision, kept where the native side can read it
 * after the WebView is gone and across a process restart (AlertRules).
 */
final class NotifyPolicy {
    private static final String PREFS = "concord_notify";

    private NotifyPolicy() {}

    static void save(Context ctx, boolean dnd, Set<String> quiet) {
        prefs(ctx).edit()
            .putBoolean("dnd", dnd)
            .putStringSet("quiet", new HashSet<>(quiet))
            .apply();
    }

    static boolean allows(Context ctx, String channelId) {
        SharedPreferences p = prefs(ctx);
        Set<String> quiet = p.getStringSet("quiet", Collections.emptySet());
        return AlertRules.nativeMayPost(p.getBoolean("dnd", false), quiet, channelId);
    }

    private static SharedPreferences prefs(Context ctx) {
        return ctx.getApplicationContext().getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }
}
