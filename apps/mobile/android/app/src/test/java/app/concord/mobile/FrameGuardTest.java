package app.concord.mobile;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

public class FrameGuardTest {
    @Test public void aFrameCantFireAnIntent() {
        for (String s : new String[] {"tel", "market", "intent", "sms", "mailto", "geo", "concord", "file", "content", "javascript", "data", "blob", "about", ""}) {
            assertTrue(s, FrameGuard.refuse(false, s));
        }
        assertTrue(FrameGuard.refuse(false, null));
    }

    @Test public void aFrameMayNavigateOverTheWeb() {
        assertFalse(FrameGuard.refuse(false, "https"));
        assertFalse(FrameGuard.refuse(false, "http"));
        assertFalse(FrameGuard.refuse(false, "HTTPS"));
    }

    @Test public void theMainFrameIsLeftToCapacitor() {
        assertFalse(FrameGuard.refuse(true, "tel"));
        assertFalse(FrameGuard.refuse(true, "https"));
    }
}
