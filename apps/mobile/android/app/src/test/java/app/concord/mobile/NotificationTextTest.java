package app.concord.mobile;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;

import org.junit.Test;

public class NotificationTextTest {
    private static final String YT = "https://www.youtube.com/watch?v=dQw4w9WgXcQ";
    private static final String THUMB = "![image](concord://attach/v2/" + "a".repeat(64) + "/" + "B".repeat(75)
        + "/jpeg/480x270/2//UmljayBBc3RsZXk)";

    @Test
    public void aRetiredPreviewRiderLeavesNoWord() {
        String c = "look https://example.com/a\n[lp](concord://lp/v1/W3sidSI6Imh0dHBzOi8vZXhhbXBsZS5jb20vYSJ9XQ)";
        String got = NotificationText.preview(c);
        assertEquals("look https://example.com/a", got);
        assertFalse("no \"lp\" at the end (ZL-09)", got.endsWith("lp"));
    }

    @Test
    public void stampsLeaveNoWord() {
        assertEquals("hi", NotificationText.preview("[eph](concord://eph/v1/1900000000)hi"));
    }

    @Test
    public void aCardIsItsTitleAndWhereItGoes() {
        assertEquals("Never Gonna Give You Up (youtube.com)\n🖼",
            NotificationText.preview("[Never Gonna Give You Up](" + YT + ") · youtube.com\n" + THUMB));
    }

    @Test
    public void aDisguisedLinkShowsItsHost() {
        assertEquals("log in: paypal.com (evil.example)",
            NotificationText.preview("log in: [paypal.com](https://evil.example/login)"));
    }

    @Test
    public void escapesAreUndone() {
        assertEquals("A *starred* [thing] (example.org)",
            NotificationText.preview("[A \\*starred\\* \\[thing\\]](https://example.org/a)"));
    }
}
