package app.concord.mobile;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

public class WebViewGateTest {
    @Test public void aWebViewWithTheMessageListenerIsLetIn() {
        assertFalse(WebViewGate.refuse(() -> true));
    }

    @Test public void anOlderWebViewIsRefused() {
        assertTrue(WebViewGate.refuse(() -> false));
    }

    @Test public void aFailedQuestionIsARefusal() {
        assertTrue(WebViewGate.refuse(() -> { throw new IllegalStateException("no WebView provider"); }));
    }

    @Test public void theScreenSaysWhatToDoAndThatNothingLoaded() {
        assertTrue(WebViewGate.TOO_OLD.contains("Android System WebView"));
        assertTrue(WebViewGate.TOO_OLD.contains("Nothing was loaded."));
    }
}
