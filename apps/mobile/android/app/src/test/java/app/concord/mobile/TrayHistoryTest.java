package app.concord.mobile;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.List;

import org.junit.Before;
import org.junit.Test;

public class TrayHistoryTest {
    @Before
    public void reset() {
        TrayHistory.clearAll();
    }

    private static List<String> ids(ArrayDeque<TrayHistory.Line> lines) {
        List<String> out = new ArrayList<>();
        for (TrayHistory.Line l : lines) out.add(l.id);
        return out;
    }

    @Test
    public void aDeletedMessageLeavesTheStackAndTheRestStays() {
        TrayHistory.append("dm", new TrayHistory.Line("Ada", "one", 1000, false, "m1"));
        TrayHistory.append("dm", new TrayHistory.Line("Ada", "two", 2000, false, "m2"));
        TrayHistory.append("dm", new TrayHistory.Line("Ada", "three", 3000, false, "m3"));

        ArrayDeque<TrayHistory.Line> rest = TrayHistory.remove("dm", "m2");
        assertNotNull("a line showed m2", rest);
        assertEquals(java.util.Arrays.asList("m1", "m3"), ids(rest));
        assertEquals("the stack kept is the one re-posted", java.util.Arrays.asList("m1", "m3"), ids(TrayHistory.snapshot("dm")));
        for (TrayHistory.Line l : TrayHistory.snapshot("dm")) {
            assertTrue("the deleted words are gone", !l.text.equals("two"));
        }
    }

    @Test
    public void theOnlyLineGoingEmptiesTheStack() {
        TrayHistory.append("dm", new TrayHistory.Line("Ada", "only", 1000, false, "m1"));
        ArrayDeque<TrayHistory.Line> rest = TrayHistory.remove("dm", "m1");
        assertNotNull(rest);
        assertTrue("nothing remains, so the entry is cancelled", rest.isEmpty());
        assertTrue(TrayHistory.snapshot("dm").isEmpty());
        // A later message starts a fresh stack, without the deleted line.
        assertEquals(java.util.Arrays.asList("m2"), ids(TrayHistory.append("dm", new TrayHistory.Line("Ada", "next", 2000, false, "m2"))));
    }

    @Test
    public void aMessageNoLineShowsChangesNothing() {
        TrayHistory.append("dm", new TrayHistory.Line("Ada", "one", 1000, false, "m1"));
        TrayHistory.append("other", new TrayHistory.Line("Bo", "x", 1000, false, "m9"));
        assertNull("not in this conversation's stack", TrayHistory.remove("dm", "m9"));
        assertNull("no stack for the tag", TrayHistory.remove("gone", "m1"));
        assertNull("no id", TrayHistory.remove("dm", ""));
        assertEquals(java.util.Arrays.asList("m1"), ids(TrayHistory.snapshot("dm")));
        assertEquals(java.util.Arrays.asList("m9"), ids(TrayHistory.snapshot("other")));
    }

    @Test
    public void ownRepliesCarryNoIdAndStay() {
        TrayHistory.append("dm", new TrayHistory.Line("Ada", "hi", 1000, false, "m1"));
        TrayHistory.append("dm", new TrayHistory.Line(null, "hello back", 2000, false));
        ArrayDeque<TrayHistory.Line> rest = TrayHistory.remove("dm", "m1");
        assertNotNull(rest);
        assertEquals(1, rest.size());
        assertNull("the user's own reply stays", rest.peekFirst().sender);
    }

    @Test
    public void aRetractionRepostsCancelsOrForgets() {
        TrayHistory.append("dm", new TrayHistory.Line("Ada", "one", 1000, false, "m1"));
        TrayHistory.append("dm", new TrayHistory.Line("Ada", "two", 2000, false, "m2"));
        String[] shown = TrayHistory.ids(TrayHistory.snapshot("dm"));
        assertEquals(java.util.Arrays.asList("m1", "m2"), java.util.Arrays.asList(shown));

        assertEquals("other lines stay on screen", TrayHistory.Retract.REPOST,
            TrayHistory.decide(TrayHistory.remove("dm", "m1"), true, shown, "m1"));
        assertEquals("the only line left goes, and the entry with it", TrayHistory.Retract.CANCEL,
            TrayHistory.decide(TrayHistory.remove("dm", "m2"), true, new String[] {"m2"}, "m2"));

        TrayHistory.append("gone", new TrayHistory.Line("Ada", "one", 1000, false, "m3"));
        TrayHistory.append("gone", new TrayHistory.Line("Ada", "two", 2000, false, "m4"));
        assertEquals("an entry swiped away is not posted again", TrayHistory.Retract.FORGET,
            TrayHistory.decide(TrayHistory.remove("gone", "m3"), false, null, "m3"));

        assertEquals("a message no line shows changes nothing", TrayHistory.Retract.NOTHING,
            TrayHistory.decide(null, true, new String[] {"m5"}, "m6"));
    }

    @Test
    public void afterARestartTheEntryShowingTheMessageGoes() {
        // The process restarted: TrayHistory is empty, the tray entry and its
        // extras are not.
        String[] trayIds = {"m1", "m2"};
        assertNull(TrayHistory.remove("dm", "m2"));
        assertEquals(TrayHistory.Retract.CANCEL, TrayHistory.decide(TrayHistory.remove("dm", "m2"), true, trayIds, "m2"));
        assertEquals("not shown there", TrayHistory.Retract.NOTHING, TrayHistory.decide(null, true, trayIds, "m9"));
        assertEquals("no entry in the tray", TrayHistory.Retract.NOTHING, TrayHistory.decide(null, false, trayIds, "m2"));
        assertEquals("an entry posted before ids were kept", TrayHistory.Retract.NOTHING, TrayHistory.decide(null, true, null, "m2"));
    }

    @Test
    public void ownRepliesAreNotListedAsMessages() {
        TrayHistory.append("dm", new TrayHistory.Line("Ada", "hi", 1000, false, "m1"));
        TrayHistory.append("dm", new TrayHistory.Line(null, "hello back", 2000, false));
        assertEquals(java.util.Arrays.asList("m1"), java.util.Arrays.asList(TrayHistory.ids(TrayHistory.snapshot("dm"))));
    }
}
