package app.concord.mobile;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

/** Only Concord itself opens a conversation (jaybos CONTRACTS.md §5, review D1). */
public class OpenGateTest {
    private static final String ID = "0123456789abcdef0123456789abcdef";

    @Test
    public void theOpenLinkIsStrict() {
        assertEquals(ID, OpenGate.openId("concord://open?c=" + ID));
        assertEquals("a_b-C9", OpenGate.openId("concord://open?c=a_b-C9"));
        String[] bad = {null, "", "concord://open?c=", "concord://open?c=" + ID + "&send=hi", "concord://open?c=a/b",
            "concord://open?c=a%2Fb", "concord://open?c=" + ID + "#x", "CONCORD://open?c=" + ID, "concord://open/?c=" + ID,
            "concord://open?c=" + new String(new char[65]).replace('\0', 'a'), "concord://channel?id=" + ID};
        for (String b : bad) assertNull(String.valueOf(b), OpenGate.openId(b));
    }

    @Test
    public void everyConversationShapeNeedsTheGate() {
        for (String u : new String[] {"concord://open?c=" + ID, "concord://channel?id=" + ID, "CONCORD://Channel?id=x",
                "concord:open?c=x", " concord://open", "concord://open/x"}) {
            assertTrue(u, OpenGate.isConversationLink(u));
        }
        assertFalse(OpenGate.isConversationLink("concord://link?c=CL1abc"));
        assertFalse(OpenGate.isConversationLink(null));
    }

    @Test
    public void theSecretMatchesOnlyItself() {
        String s = "ab".repeat(32);
        assertTrue(OpenGate.matches(s, s));
        assertFalse(OpenGate.matches("ab".repeat(31) + "ac", s));
        assertFalse(OpenGate.matches(null, s));
        assertFalse(OpenGate.matches("", s));
        assertFalse(OpenGate.matches(s, null));
    }
}
