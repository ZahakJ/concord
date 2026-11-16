package app.concord.mobile;

import java.util.ArrayDeque;
import java.util.HashMap;
import java.util.Iterator;
import java.util.Map;

/**
 * The in-memory conversation history behind each message notification's
 * MessagingStyle: the last few (sender, text, time) lines per tag, so a second
 * message stacks under the first instead of overwriting it. Process-local on
 * purpose: the tray itself survives a process restart and simply restacks from
 * one line again. Kept free of Android types so a plain JVM unit test can hold
 * it to its word; ConcordCorePlugin renders what it returns.
 */
final class TrayHistory {
    private TrayHistory() {}

    static final int MAX = 6;
    static final long TTL_MS = 60L * 60 * 1000; // stale lines age out

    private static final Map<String, ArrayDeque<Line>> BY_TAG = new HashMap<>();

    static final class Line {
        final String sender;
        final String text;
        final long at;

        final boolean failed;
        // The message this line shows, when known. The page and NativeNotifier
        // can both post the same message (the WebView keeps running for a while
        // after the activity leaves the screen), and the second post must not
        // stack the line a second time. A deleted or expired message is taken
        // out of the stack by this id.
        final String id;

        // sender == null is the device owner's own line (an inline reply).
        Line(String sender, String text, long at, boolean failed) {
            this(sender, text, at, failed, "");
        }

        Line(String sender, String text, long at, boolean failed, String id) {
            this.sender = sender;
            this.text = text;
            this.at = at;
            this.failed = failed;
            this.id = id == null ? "" : id;
        }

        String label() {
            return failed ? "Not sent: " + text : text;
        }

        // What the tray may show for this line. An own reply is the user's own
        // words, so unless the setting is "full" it is not echoed either.
        String shown(boolean full) {
            if (full || sender != null) return label();
            return failed ? "Not sent" : "Reply sent";
        }
    }

    /**
     * Appends a line to the tag's stack and returns a copy of the whole stack
     * to render, newest last; null when a line for the same message is already
     * there.
     */
    static ArrayDeque<Line> append(String tag, Line line) {
        synchronized (BY_TAG) {
            ArrayDeque<Line> q = BY_TAG.get(tag);
            if (q == null) BY_TAG.put(tag, q = new ArrayDeque<>());
            while (!q.isEmpty() && (q.size() >= MAX || line.at - q.peekFirst().at > TTL_MS)) {
                q.pollFirst();
            }
            if (!line.id.isEmpty()) {
                for (Line l : q) {
                    if (line.id.equals(l.id)) return null; // already on screen
                }
            }
            q.addLast(line);
            return new ArrayDeque<>(q);
        }
    }

    /**
     * Takes the line showing message {@code id} out of the tag's stack. Returns
     * null when no line in the stack shows it (nothing on screen changes), and
     * otherwise a copy of the lines that remain, empty when it was the only
     * one; an empty stack is forgotten.
     */
    static ArrayDeque<Line> remove(String tag, String id) {
        if (tag == null || id == null || id.isEmpty()) return null;
        synchronized (BY_TAG) {
            ArrayDeque<Line> q = BY_TAG.get(tag);
            if (q == null) return null;
            boolean found = false;
            for (Iterator<Line> it = q.iterator(); it.hasNext(); ) {
                if (id.equals(it.next().id)) {
                    it.remove();
                    found = true;
                }
            }
            if (!found) return null;
            if (q.isEmpty()) BY_TAG.remove(tag);
            return new ArrayDeque<>(q);
        }
    }

    /** What a retraction does to the conversation's tray entry. */
    enum Retract {
        /** No line on screen shows the message. */
        NOTHING,
        /** Remove the entry: the message was its only line, or the stack behind it is gone. */
        CANCEL,
        /** Re-post the remaining lines quietly in place. */
        REPOST,
        /** The entry is no longer in the tray: forget the stack, post nothing. */
        FORGET,
    }

    /**
     * Decides a retraction. {@code rest} is what {@link #remove} returned;
     * {@code showing} whether the tag's entry is in the tray; {@code trayIds}
     * the message ids that entry was posted with (its extras), which outlive
     * this process when the tray does. When the stack is gone (the process
     * restarted under a standing entry) and the entry shows the message, the
     * other lines cannot be rebuilt, so the entry goes.
     */
    static Retract decide(ArrayDeque<Line> rest, boolean showing, String[] trayIds, String id) {
        if (rest == null) {
            if (!showing || trayIds == null || id == null || id.isEmpty()) return Retract.NOTHING;
            for (String t : trayIds) {
                if (id.equals(t)) return Retract.CANCEL;
            }
            return Retract.NOTHING;
        }
        if (rest.isEmpty()) return Retract.CANCEL;
        return showing ? Retract.REPOST : Retract.FORGET;
    }

    /** The message ids of a stack, for the entry's extras. */
    static String[] ids(ArrayDeque<Line> lines) {
        java.util.List<String> out = new java.util.ArrayList<>();
        for (Line l : lines) {
            if (!l.id.isEmpty()) out.add(l.id);
        }
        return out.toArray(new String[0]);
    }

    /** A copy of the tag's stack, empty when there is none. */
    static ArrayDeque<Line> snapshot(String tag) {
        synchronized (BY_TAG) {
            ArrayDeque<Line> q = BY_TAG.get(tag);
            return q == null ? new ArrayDeque<>() : new ArrayDeque<>(q);
        }
    }

    static void clear(String tag) {
        synchronized (BY_TAG) {
            BY_TAG.remove(tag);
        }
    }

    static void clearAll() {
        synchronized (BY_TAG) {
            BY_TAG.clear();
        }
    }
}
