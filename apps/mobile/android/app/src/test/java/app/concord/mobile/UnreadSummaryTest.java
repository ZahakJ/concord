package app.concord.mobile;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import java.util.LinkedHashMap;
import java.util.Map;

import org.junit.Test;

/** Concord's unread card arithmetic (jaybos CONTRACTS.md §3). */
public class UnreadSummaryTest {
    private static final String A = "0123456789abcdef0123456789abcdef";
    private static final String B = "fedcba9876543210fedcba9876543210";

    private static Map<String, Long> m(Object... kv) {
        Map<String, Long> out = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) out.put((String) kv[i], ((Number) kv[i + 1]).longValue());
        return out;
    }

    @Test
    public void thePagesCountIsTheTruth() {
        UnreadSummary u = new UnreadSummary();
        u.replace(m(A, 3, B, 2, "bad id", 5, "z", 0, "n", -4), 1000);
        assertEquals(5, u.total());
        assertEquals(2, u.conversations());
        assertEquals(1000, u.asOf());
        assertFalse(u.partial());
    }

    @Test
    public void nativeBumpsArePartialUntilThePageCountsAgain() {
        UnreadSummary u = new UnreadSummary();
        u.replace(m(A, 1), 1000);
        u.bump(B);
        u.bump(B);
        u.bump("not|valid");
        assertEquals(3, u.total());
        assertEquals(2, u.conversations());
        assertTrue(u.partial());
        assertEquals("a bump is not a count", 1000, u.asOf());
        u.clear(B);
        assertEquals(1, u.total());
        u.replace(m(A, 4), 2000);
        assertFalse(u.partial());
        assertEquals(4, u.total());
    }

    @Test
    public void capsHold() {
        UnreadSummary u = new UnreadSummary();
        Map<String, Long> many = new LinkedHashMap<>();
        for (int i = 0; i < UnreadSummary.MAX_CONVERSATIONS + 10; i++) many.put("c" + i, 1_000_000L);
        u.replace(many, 1);
        assertEquals(UnreadSummary.MAX_CONVERSATIONS, u.conversations());
        assertEquals(UnreadSummary.MAX_TOTAL, u.total());
        u.bump("another");
        assertEquals("no conversation past the cap", UnreadSummary.MAX_CONVERSATIONS, u.conversations());
        String longId = new String(new char[129]).replace('\0', 'a');
        assertFalse(UnreadSummary.validId(longId));
        assertFalse(UnreadSummary.validId(""));
        assertFalse(UnreadSummary.validId(null));
    }

    @Test
    public void itSurvivesARestartAndMalformedStorageReadsEmpty() {
        UnreadSummary u = new UnreadSummary();
        u.replace(m(A, 3, B, 1), 1234);
        u.bump(A);
        UnreadSummary back = UnreadSummary.decode(u.encode());
        assertEquals(5, back.total());
        assertEquals(2, back.conversations());
        assertTrue(back.partial());
        assertEquals(1234, back.asOf());
        for (String bad : new String[] {null, "", "v2|1|0|", "v1|x|0|", "v1|1|0|a", "v1|1|0|a=x", "v1|1|0"}) {
            UnreadSummary e = UnreadSummary.decode(bad);
            assertEquals(String.valueOf(bad), 0, e.total());
            assertEquals(0, e.conversations());
        }
        assertEquals(0, UnreadSummary.decode("v1|1|0|").total());
    }

    @Test
    public void eraseLeavesNothing() {
        UnreadSummary u = new UnreadSummary();
        u.replace(m(A, 3), 99);
        u.bump(B);
        u.wipe();
        assertEquals(0, u.total());
        assertEquals(0, u.asOf());
        assertFalse(u.partial());
        assertEquals("v1|0|0|", u.encode());
    }
}
