package app.concord.mobile;

import java.util.LinkedHashMap;
import java.util.Map;
import java.util.regex.Pattern;

/**
 * The unread counts Concord offers Jayb's Messages tile (jaybos CONTRACTS.md
 * §3): one number per conversation, and nothing else. The page's count
 * replaces everything ({@link #replace}); while the page isn't running the
 * native message path adds one per direct message it notifies ({@link #bump})
 * and marks the whole count {@code partial}; the tray's "Mark read" clears a
 * conversation. Plain Java, so the arithmetic is unit tested without Android.
 */
final class UnreadSummary {
    static final int MAX_CONVERSATIONS = 2000;
    static final long MAX_COUNT = 999_999;
    static final long MAX_TOTAL = 999_999;
    private static final Pattern ID = Pattern.compile("[0-9A-Za-z_.:-]{1,128}");

    private final LinkedHashMap<String, Long> counts = new LinkedHashMap<>();
    private boolean partial;
    private long asOf;

    static boolean validId(String id) {
        return id != null && ID.matcher(id).matches();
    }

    /** The page counted: its numbers are the truth, and nothing is partial any more. */
    void replace(Map<String, Long> page, long now) {
        counts.clear();
        if (page != null) {
            for (Map.Entry<String, Long> e : page.entrySet()) {
                if (counts.size() >= MAX_CONVERSATIONS) break;
                Long n = e.getValue();
                if (!validId(e.getKey()) || n == null || n <= 0) continue;
                counts.put(e.getKey(), Math.min(n, MAX_COUNT));
            }
        }
        partial = false;
        asOf = now;
    }

    /** One message notified while the page wasn't counting. */
    void bump(String id) {
        if (!validId(id)) return;
        Long n = counts.get(id);
        if (n == null && counts.size() >= MAX_CONVERSATIONS) return;
        counts.put(id, Math.min((n == null ? 0 : n) + 1, MAX_COUNT));
        partial = true;
    }

    /** "Mark read" from the tray. */
    void clear(String id) {
        counts.remove(id);
    }

    /** Erase: nothing of the account stays. */
    void wipe() {
        counts.clear();
        partial = false;
        asOf = 0;
    }

    long total() {
        long t = 0;
        for (long n : counts.values()) t = Math.min(t + n, MAX_TOTAL);
        return t;
    }

    int conversations() {
        return counts.size();
    }

    boolean partial() {
        return partial;
    }

    long asOf() {
        return asOf;
    }

    /** "v1|asOf|partial|id=n,id=n": ids never hold '|', '=' or ','. */
    String encode() {
        StringBuilder b = new StringBuilder("v1|").append(asOf).append('|').append(partial ? 1 : 0).append('|');
        boolean first = true;
        for (Map.Entry<String, Long> e : counts.entrySet()) {
            if (!first) b.append(',');
            b.append(e.getKey()).append('=').append(e.getValue());
            first = false;
        }
        return b.toString();
    }

    /** Anything malformed reads as empty: a count is never guessed. */
    static UnreadSummary decode(String s) {
        UnreadSummary u = new UnreadSummary();
        if (s == null) return u;
        String[] p = s.split("\\|", -1);
        if (p.length != 4 || !"v1".equals(p[0])) return u;
        try {
            long asOf = Long.parseLong(p[1]);
            LinkedHashMap<String, Long> m = new LinkedHashMap<>();
            if (!p[3].isEmpty()) {
                for (String kv : p[3].split(",", -1)) {
                    int i = kv.indexOf('=');
                    if (i <= 0) return new UnreadSummary();
                    m.put(kv.substring(0, i), Long.parseLong(kv.substring(i + 1)));
                }
            }
            u.replace(m, asOf);
            u.partial = "1".equals(p[2]);
        } catch (NumberFormatException e) {
            return new UnreadSummary();
        }
        return u;
    }
}
