package app.concord.mobile;

import android.webkit.WebResourceRequest;
import android.webkit.WebView;
import com.getcapacitor.Bridge;
import com.getcapacitor.BridgeWebViewClient;
import java.util.Locale;

/**
 * A subframe (the YouTube player, a link card) may navigate only over http(s). Anything else it asks for, tel:,
 * market:, intent:, concord: and the rest, is refused here, before Capacitor's launchIntent could hand it to
 * another app. A plugin's shouldOverrideLoad can't do this: it is given the URL alone, not which frame asked.
 */
final class FrameGuard extends BridgeWebViewClient {
    FrameGuard(Bridge bridge) {
        super(bridge);
    }

    static boolean refuse(boolean mainFrame, String scheme) {
        if (mainFrame) return false;
        if (scheme == null) return true;
        String s = scheme.toLowerCase(Locale.ROOT);
        return !(s.equals("https") || s.equals("http"));
    }

    @Override
    public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
        if (refuse(request.isForMainFrame(), request.getUrl().getScheme())) return true;
        return super.shouldOverrideUrlLoading(view, request);
    }
}
