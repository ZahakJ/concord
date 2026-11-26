package app.concord.mobile;

import android.content.Context;
import android.content.SharedPreferences;
import android.net.Uri;

import java.util.Map;

/**
 * Where the native side keeps Concord's unread card (UnreadSummary) between
 * runs: private prefs, conversation ids and counts only. Every change tells
 * observers of the card's URI (Jayb's tile).
 */
final class UnreadCard {
    static final String AUTHORITY_SUFFIX = ".cards";
    private static final String PREFS = "concord_cards";
    private static final String KEY = "unread";

    private UnreadCard() {}

    static Uri uri(Context ctx) {
        return Uri.parse("content://" + ctx.getPackageName() + AUTHORITY_SUFFIX + "/unread");
    }

    static synchronized UnreadSummary read(Context ctx) {
        return UnreadSummary.decode(prefs(ctx).getString(KEY, null));
    }

    static synchronized void replace(Context ctx, Map<String, Long> counts) {
        UnreadSummary u = new UnreadSummary();
        u.replace(counts, System.currentTimeMillis());
        save(ctx, u);
    }

    static synchronized void bump(Context ctx, String conversation) {
        UnreadSummary u = read(ctx);
        u.bump(conversation);
        save(ctx, u);
    }

    static synchronized void clear(Context ctx, String conversation) {
        UnreadSummary u = read(ctx);
        if (u.conversations() == 0) return;
        u.clear(conversation);
        save(ctx, u);
    }

    private static void save(Context ctx, UnreadSummary u) {
        prefs(ctx).edit().putString(KEY, u.encode()).apply();
        try {
            ctx.getContentResolver().notifyChange(uri(ctx), null);
        } catch (RuntimeException ignored) {
            // No observer machinery (a test, a dying process): the next query reads the prefs anyway.
        }
    }

    private static SharedPreferences prefs(Context ctx) {
        return ctx.getApplicationContext().getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }
}
