package app.concord.mobile;

import android.app.Activity;
import android.graphics.Color;
import android.util.TypedValue;
import android.view.Gravity;
import android.widget.TextView;

import androidx.webkit.WebViewFeature;

import java.util.function.BooleanSupplier;

/**
 * Concord runs only on a WebView with WEB_MESSAGE_LISTENER. Without it Capacitor falls back to
 * addJavascriptInterface, which hands the native bridge to every frame in the page, link previews
 * and YouTube embeds included (security review). An older WebView gets a plain screen instead, and
 * the bridge is never built.
 */
final class WebViewGate {
    static final String TOO_OLD = "Concord needs a newer Android System WebView.\n\n"
        + "Update Android System WebView (Vanadium on GrapheneOS) from your app store, then open Concord again. "
        + "Nothing was loaded.";

    private WebViewGate() {}

    /** True when the bridge must not be built: the feature is missing, or asking about it failed. */
    static boolean refuse(BooleanSupplier hasMessageListener) {
        try {
            return !hasMessageListener.getAsBoolean();
        } catch (Throwable t) {
            return true;
        }
    }

    static boolean refuse() {
        return refuse(() -> WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER));
    }

    static void showTooOld(Activity a) {
        TextView t = new TextView(a);
        t.setText(TOO_OLD);
        t.setTextColor(Color.WHITE);
        t.setBackgroundColor(Color.BLACK);
        t.setTextSize(TypedValue.COMPLEX_UNIT_SP, 17);
        t.setGravity(Gravity.CENTER);
        int pad = (int) (28 * a.getResources().getDisplayMetrics().density);
        t.setPadding(pad, pad, pad, pad);
        a.setContentView(t);
    }
}
