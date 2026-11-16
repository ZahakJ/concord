package app.concord.mobile;

import java.net.IDN;
import java.nio.charset.StandardCharsets;
import java.util.Locale;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * The words a native message notification shows, from a message's content.
 * Pure (no Android types) so a plain JVM test can hold it to the rules
 * (NotificationTextTest), the way AlertRules is.
 */
final class NotificationText {
    private NotificationText() {}

    // The tokens that only MODIFY a message (an expiry, a sealed time, an
    // effect) and the retired link-preview rider "[lp](concord://lp/v1/…)".
    // They say nothing to a reader, so they go before anything else; collapsed
    // to their labels they printed "eph" or "lp" at the end of the text.
    private static final Pattern RIDERS = Pattern.compile(
        "\\[(?:fx|eph|ts|lp)\\]\\(concord://(?:fx|eph|ts|lp)/v\\d+/[^)\\s]*\\)");

    // A link card (lib/linkcard.js) is a masked link plus a captioned picture:
    // its title stands for the link, and backslash escapes in it are undone.
    // A masked web link's words can say anything, so they are followed by the
    // host the link really goes to, as the feed prints its domain: a lock
    // screen reading "paypal.com" for a link to evil.example is the disguise
    // the feed refuses. (No public-suffix list here, so it is the whole host
    // without "www." — longer than the feed's domain, never less honest.)
    // When no host can be read, the link itself is printed: the words are
    // never shown alone.
    private static final Pattern MASKED_WEB = Pattern.compile(
        "\\[((?:\\\\.|[^\\]\\\\\\n])+)\\]\\((https?://[^\\s)]+)\\)(?: · ([A-Za-z0-9.-]+))?");

    // The authority of a web URL read the way a browser reads it (WHATWG): it
    // ends at the first "/", "?", "#" or backslash, so
    // "https://evil.example\@paypal.com/" goes to evil.example.
    private static final Pattern AUTHORITY = Pattern.compile(
        "^https?://([^/?#\\\\]*)", Pattern.CASE_INSENSITIVE);
    private static final Pattern PRINTABLE_HOST = Pattern.compile("[a-z0-9_-]+(?:\\.[a-z0-9_-]+)*");

    // Format characters (bidi overrides and isolates, zero-width characters)
    // are invisible and can reorder what follows them: an RLO in a link's
    // words would mirror the " (host)" printed after them. A masked link's
    // words lose them all; the rest of the text loses the bidi controls, which
    // reach past their own words, and keeps the joiners emoji are built with.
    private static final Pattern FORMAT = Pattern.compile("\\p{Cf}");
    private static final Pattern BIDI = Pattern.compile("[\\u202A-\\u202E\\u2066-\\u2069]");
    private static final Pattern ESCAPE = Pattern.compile("\\\\([\\\\`*_~|#\\[\\]{}:@])");

    static String preview(String content) {
        String s = RIDERS.matcher(content).replaceAll("")
            .replaceAll("!\\[image\\]\\(concord://[^)]*\\)", "🖼")
            .replaceAll("\\[([^\\]]*)\\]\\(concord://[^)]*\\)", "$1");
        Matcher m = MASKED_WEB.matcher(s);
        StringBuilder out = new StringBuilder();
        int last = 0;
        while (m.find()) {
            out.append(unescape(s.substring(last, m.start())));
            String words = FORMAT.matcher(unescape(m.group(1))).replaceAll("");
            String host = hostOf(m.group(2));
            // " · <domain>" after a card's title is there for readers that
            // print nothing else. The host printed here already says it when
            // it is true, and when it is not it is the sender's word against
            // the link's: it is never printed.
            out.append(words).append(" (").append(host.isEmpty() ? shownURL(m.group(2)) : host).append(')');
            last = m.end();
        }
        out.append(unescape(s.substring(last)));
        s = BIDI.matcher(out).replaceAll("").trim();
        if (s.length() > 300) s = s.substring(0, 300) + "…";
        return s.isEmpty() ? "Sent you something" : s;
    }

    private static String unescape(String s) {
        return ESCAPE.matcher(s).replaceAll("$1");
    }

    // hostOf is the host a web link goes to, in ASCII ("xn--" for a name in
    // another script, so a Cyrillic "р" cannot pass for a Latin "p"), without
    // "www."; or "" when none can be read. Read from the string, not by
    // java.net.URI, which gives no host at all for an underscore, a non-ASCII
    // letter or a backslash.
    static String hostOf(String url) {
        Matcher a = AUTHORITY.matcher(url);
        if (!a.find()) return "";
        String auth = a.group(1);
        int at = auth.lastIndexOf('@');
        if (at >= 0) auth = auth.substring(at + 1);
        String h;
        if (auth.startsWith("[")) {
            int end = auth.indexOf(']');
            if (end < 0) return "";
            h = auth.substring(0, end + 1).toLowerCase(Locale.ROOT);
            return h.matches("\\[[0-9a-f:.]+\\]") ? h : "";
        }
        int colon = auth.lastIndexOf(':');
        if (colon >= 0) auth = auth.substring(0, colon);
        String decoded = percentDecode(auth);
        if (decoded == null || decoded.isEmpty()) return "";
        try {
            h = IDN.toASCII(decoded, IDN.ALLOW_UNASSIGNED).toLowerCase(Locale.ROOT);
        } catch (IllegalArgumentException e) {
            return "";
        }
        if (h.endsWith(".")) h = h.substring(0, h.length() - 1);
        if (!PRINTABLE_HOST.matcher(h).matches()) return "";
        return h.startsWith("www.") ? h.substring(4) : h;
    }

    // %-escapes in a host are undone by a browser before it looks the name
    // up ("evil%2eexample" is evil.example). null when they are not UTF-8.
    private static String percentDecode(String s) {
        if (s.indexOf('%') < 0) return s;
        java.io.ByteArrayOutputStream b = new java.io.ByteArrayOutputStream();
        byte[] raw = s.getBytes(StandardCharsets.UTF_8);
        for (int i = 0; i < raw.length; i++) {
            if (raw[i] != '%') {
                b.write(raw[i]);
                continue;
            }
            if (i + 2 >= raw.length) return null;
            int hi = Character.digit(raw[i + 1], 16), lo = Character.digit(raw[i + 2], 16);
            if (hi < 0 || lo < 0) return null;
            b.write(hi * 16 + lo);
            i += 2;
        }
        try {
            return StandardCharsets.UTF_8.newDecoder().decode(java.nio.ByteBuffer.wrap(b.toByteArray())).toString();
        } catch (java.nio.charset.CharacterCodingException e) {
            return null;
        }
    }

    // shownURL is a link printed whole because no host could be read from it,
    // with its invisible characters written as %-escapes.
    private static String shownURL(String url) {
        StringBuilder b = new StringBuilder();
        url.codePoints().forEach(cp -> {
            if (Character.getType(cp) == Character.FORMAT || Character.isISOControl(cp)) {
                for (byte x : new String(Character.toChars(cp)).getBytes(StandardCharsets.UTF_8)) {
                    b.append(String.format("%%%02X", x & 0xff));
                }
            } else {
                b.appendCodePoint(cp);
            }
        });
        return b.toString();
    }
}
