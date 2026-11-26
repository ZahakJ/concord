package app.concord.mobile;

import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.net.Uri;

import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.regex.Pattern;

/**
 * Only Concord itself opens a conversation from outside the page (jaybos
 * CONTRACTS.md §5, review D1). Opening one marks it read and sends read
 * receipts, so concord://open and concord://channel arriving at MainActivity's
 * BROWSABLE filter from a web page or another app must not do it. A conversation
 * link counts only when the intent carries this install's gate secret: random,
 * kept in private prefs, put there by Concord's own notification taps and by
 * OpenConversationActivity (which Android lets only the owner's Jayb start).
 * Any other conversation link loses its data before Capacitor sees it.
 */
final class OpenGate {
    static final String EXTRA = "app.concord.mobile.extra.OPEN_GATE";
    private static final String PREFS = "concord_open_gate";
    private static final Pattern ID = Pattern.compile("[A-Za-z0-9_-]{1,64}");

    private OpenGate() {}

    /** The id of a well-formed concord://open?c=<id>, or null. The same rule as the page's lib/openlink.js. */
    static String openId(String url) {
        if (url == null || url.length() > 200 || !url.startsWith("concord://open?c=")) return null;
        String id = url.substring("concord://open?c=".length());
        return ID.matcher(id).matches() ? id : null;
    }

    /** concord://open or concord://channel in any case or shape: what only a gated intent may carry. */
    static boolean isConversationLink(String url) {
        if (url == null) return false;
        String u = url.trim().toLowerCase(java.util.Locale.ROOT);
        return u.startsWith("concord://open") || u.startsWith("concord://channel") || u.startsWith("concord:open") || u.startsWith("concord:channel");
    }

    static synchronized String secret(Context ctx) {
        SharedPreferences p = ctx.getApplicationContext().getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        String s = p.getString("s", null);
        if (s == null || s.length() != 64) {
            byte[] b = new byte[32];
            new SecureRandom().nextBytes(b);
            StringBuilder h = new StringBuilder();
            for (byte x : b) h.append(String.format("%02x", x));
            s = h.toString();
            p.edit().putString("s", s).commit();
        }
        return s;
    }

    static Intent stamp(Context ctx, Intent i) {
        return i.putExtra(EXTRA, secret(ctx));
    }

    /** Constant-time: [given] is this install's secret. */
    static boolean matches(String given, String secret) {
        if (given == null || secret == null) return false;
        return MessageDigest.isEqual(given.getBytes(java.nio.charset.StandardCharsets.UTF_8), secret.getBytes(java.nio.charset.StandardCharsets.UTF_8));
    }

    /** Strips a conversation link that isn't Concord's own. Returns true when it stripped one. */
    static boolean sanitize(Context ctx, Intent i) {
        if (i == null) return false;
        Uri d = i.getData();
        if (d == null || !isConversationLink(d.toString())) return false;
        String given = null;
        try {
            given = i.getStringExtra(EXTRA);
        } catch (RuntimeException ignored) {
            // A malformed extras bundle from a stranger: not ours.
        }
        if (matches(given, secret(ctx))) return false;
        i.setData(null);
        return true;
    }
}
