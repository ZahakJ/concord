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

    // LC2-01: java.net.URI gives no host for these three, and the words were
    // then printed alone.
    @Test
    public void aCyrillicHostIsPrintedInAscii() {
        assertEquals("log in: paypal.com (xn--aypal-uye.com)",
            NotificationText.preview("log in: [paypal.com](https://\u0440aypal.com/login)"));
    }

    @Test
    public void anUnderscoreHostIsPrinted() {
        assertEquals("paypal.com (evil_site.example)",
            NotificationText.preview("[paypal.com](https://evil_site.example/login)"));
    }

    @Test
    public void aBackslashEndsTheHostAsInABrowser() {
        assertEquals("paypal.com (evil.example)",
            NotificationText.preview("[paypal.com](https://evil.example\\@paypal.com/)"));
    }

    @Test
    public void escapedHostsAreReadAsTheBrowserReadsThem() {
        assertEquals("paypal.com (paypal.com.evil.example)",
            NotificationText.preview("[paypal.com](https://paypal.com%2eevil.example/)"));
        assertEquals("youtube.com (youtube.com)",
            NotificationText.preview("[youtube.com](https://\uff59\uff4f\uff55\uff54\uff55\uff42\uff45\uff0e\uff43\uff4f\uff4d/)"));
    }

    @Test
    public void aLinkWithNoReadableHostIsPrintedWhole() {
        String longLabel = "a".repeat(64);
        assertEquals("paypal.com (https://" + longLabel + ".example/x)",
            NotificationText.preview("[paypal.com](https://" + longLabel + ".example/x)"));
        assertEquals("paypal.com (https://ev%zzil.example/)",
            NotificationText.preview("[paypal.com](https://ev%zzil.example/)"));
        assertEquals("paypal.com (https://evil%E2%80%AE.example/)",
            NotificationText.preview("[paypal.com](https://evil\u202e.example/)"));
    }

    @Test
    public void aDomainThatIsNotTheLinksIsNotPrinted() {
        assertEquals("Never Gonna Give You Up (evil.example)",
            NotificationText.preview("[Never Gonna Give You Up](https://evil.example/v) \u00b7 youtube.com"));
    }

    @Test
    public void formatCharactersLeaveTheWords() {
        assertEquals("paypal.com (evil.example)",
            NotificationText.preview("[\u202epaypal.com\u200b](https://evil.example/)"));
        assertEquals("look: x (evil.example)",
            NotificationText.preview("\u202elook: [x](https://evil.example/)"));
        // The joiners an emoji is built from stay.
        assertEquals("\ud83d\udc68\u200d\ud83d\udc69\u200d\ud83d\udc67 hi",
            NotificationText.preview("\ud83d\udc68\u200d\ud83d\udc69\u200d\ud83d\udc67 hi"));
    }
}
