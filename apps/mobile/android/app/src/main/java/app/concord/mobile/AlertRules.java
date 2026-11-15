package app.concord.mobile;

import java.util.Set;

/**
 * The two decisions behind a message notification, kept free of Android types
 * so a plain JVM unit test can hold them to their word.
 */
final class AlertRules {
    private AlertRules() {}

    /**
     * How long a conversation's tray entry stays quiet after it last sounded.
     * Inside it, a new message updates the entry without a sound or a second
     * heads-up, so a burst in one conversation is one sound, not one per line.
     * The same pace the app gives a busy channel in the foreground
     * (lib/notifs.js CHATTER_GAP_MS).
     */
    static final long ALERT_GAP_MS = 10_000;

    /** Should this post sound, given when the conversation last sounded (null: never)? */
    static boolean shouldAlert(Long lastAlertAt, long now) {
        return lastAlertAt == null || now - lastAlertAt >= ALERT_GAP_MS;
    }

    /**
     * May the native side post a message in this channel? It runs only while
     * Concord is not on screen and cannot read the levels itself, so the page
     * hands it its decision (ConcordCorePlugin.setNotifyPolicy): the user's
     * Do Not Disturb status in Concord, and the direct messages that must not
     * notify on their own (level "Nothing", or "Only @mentions", where a mention
     * is something this side cannot judge).
     */
    static boolean nativeMayPost(boolean dnd, Set<String> quiet, String channelId) {
        if (dnd) return false;
        return quiet == null || !quiet.contains(channelId);
    }
}
