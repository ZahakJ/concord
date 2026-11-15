package app.concord.mobile;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import java.util.Collections;
import java.util.HashSet;
import java.util.Set;

import org.junit.Test;

public class AlertRulesTest {
    @Test
    public void aBurstInOneConversationSoundsOnce() {
        assertTrue("the first message sounds", AlertRules.shouldAlert(null, 1000));
        assertFalse("one a second later does not", AlertRules.shouldAlert(1000L, 2000));
        assertFalse("nor one just inside the gap", AlertRules.shouldAlert(1000L, 1000 + AlertRules.ALERT_GAP_MS - 1));
        assertTrue("after the gap it sounds again", AlertRules.shouldAlert(1000L, 1000 + AlertRules.ALERT_GAP_MS));
    }

    @Test
    public void theNativeSideHonoursLevelsAndDoNotDisturb() {
        Set<String> quiet = new HashSet<>(Collections.singletonList("muted-dm"));
        assertTrue("an ordinary DM posts", AlertRules.nativeMayPost(false, quiet, "dm"));
        assertFalse("a DM set to Nothing does not", AlertRules.nativeMayPost(false, quiet, "muted-dm"));
        assertFalse("nothing posts under Do Not Disturb", AlertRules.nativeMayPost(true, quiet, "dm"));
        assertTrue("no policy yet: post, as before", AlertRules.nativeMayPost(false, null, "dm"));
    }
}
