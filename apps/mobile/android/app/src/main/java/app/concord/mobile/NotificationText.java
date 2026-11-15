package app.concord.mobile;

import java.net.URI;
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
    private static final Pattern MASKED_WEB = Pattern.compile(
        "\\[((?:\\\\.|[^\\]\\\\\\n])+)\\]\\((https?://[^\\s)]+)\\)(?: · ([A-Za-z0-9.-]+))?");

    static String preview(String content) {
        String s = RIDERS.matcher(content).replaceAll("")
            .replaceAll("!\\[image\\]\\(concord://[^)]*\\)", "🖼")
            .replaceAll("\\[([^\\]]*)\\]\\(concord://[^)]*\\)", "$1");
        Matcher m = MASKED_WEB.matcher(s);
        StringBuilder out = new StringBuilder();
        while (m.find()) {
            String words = m.group(1);
            String host = hostOf(m.group(2));
            String said = m.group(3);
            String shown = words;
            if (!host.isEmpty()) {
                shown = words + " (" + host + ")";
                // " · <domain>" after a card's title is there for readers
                // that print nothing else; with the host shown it repeats it.
                if (said != null && !(host.equals(said) || host.endsWith("." + said))) shown += " · " + said;
            } else if (said != null) {
                shown += " · " + said;
            }
            m.appendReplacement(out, Matcher.quoteReplacement(shown));
        }
        m.appendTail(out);
        s = out.toString()
            .replaceAll("\\\\([\\\\`*_~|#\\[\\]{}:@])", "$1")
            .trim();
        if (s.length() > 300) s = s.substring(0, 300) + "…";
        return s.isEmpty() ? "Sent you something" : s;
    }

    private static String hostOf(String url) {
        try {
            String h = new URI(url).getHost();
            if (h == null) return "";
            h = h.toLowerCase(Locale.ROOT);
            if (h.endsWith(".")) h = h.substring(0, h.length() - 1);
            return h.startsWith("www.") ? h.substring(4) : h;
        } catch (Exception e) {
            return "";
        }
    }
}
