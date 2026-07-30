package app.concord.mobile;

import android.graphics.Color;
import android.os.Build;
import android.os.Bundle;
import android.view.View;
import android.view.WindowManager;
import android.webkit.WebView;

import androidx.core.graphics.Insets;
import androidx.core.splashscreen.SplashScreen;
import androidx.core.view.ViewCompat;
import androidx.core.view.WindowCompat;
import androidx.core.view.WindowInsetsCompat;
import androidx.core.view.WindowInsetsControllerCompat;

import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {
    // The web layer signals its first paint (App.svelte's onMount) so the system
    // splash can stay up across the WebView load AND the Go core boot instead of
    // handing over to a blank window for an indeterminate few seconds. The
    // deadline is the safety net: if the bridge never comes up, holding the
    // splash forever would be worse than showing whatever did load.
    private static volatile boolean webReady = false;
    private long splashDeadline = 0;

    @Override
    public void onCreate(Bundle savedInstanceState) {
        // Must run before super.onCreate: it swaps the launch theme for the
        // post-splash theme and installs the splash view.
        SplashScreen splash = SplashScreen.installSplashScreen(this);
        splashDeadline = System.currentTimeMillis() + 8000;
        splash.setKeepOnScreenCondition(() -> !webReady && System.currentTimeMillis() < splashDeadline);

        // Local plugins must be registered before super.onCreate wires the bridge.
        registerPlugin(ConcordCorePlugin.class);
        super.onCreate(savedInstanceState);

        // Android 15+ (targetSdk 35+) forces edge-to-edge with no way to opt out;
        // declare it explicitly so older releases behave identically, then hand
        // the real inset sizes to CSS below. POST_NOTIFICATIONS is deliberately
        // NOT requested here any more — see ConcordCorePlugin.requestNotifications():
        // a permission dialog on top of the splash, before the user has seen what
        // Concord even is, is the reflex-deny that costs them every future alert.
        WindowCompat.setDecorFitsSystemWindows(getWindow(), false);
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.VANILLA_ICE_CREAM) {
            // Deprecated (and ignored) from API 35 on, where the bars are always
            // transparent. Below it, this is what stops the system painting an
            // opaque strip over the app's own top bar.
            getWindow().setStatusBarColor(Color.TRANSPARENT);
            getWindow().setNavigationBarColor(Color.TRANSPARENT);
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            // Without this the default cutout mode letterboxes the app in
            // landscape — a black bar down the notch side — instead of letting
            // it draw across and pad with the left/right insets below.
            WindowManager.LayoutParams lp = getWindow().getAttributes();
            lp.layoutInDisplayCutoutMode =
                WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES;
            getWindow().setAttributes(lp);
        }
        installInsetBridge();
    }

    /** Called from ConcordCorePlugin once the web layer has mounted. */
    static void markWebReady() {
        webReady = true;
    }

    // ---- window insets → CSS custom properties ----
    // env(safe-area-inset-*) is dead weight in the Android WebView: it reports
    // display cutouts and nothing else — never the status bar, the gesture nav
    // bar, or the IME. So every safe-area rule in the stylesheet resolved to 0,
    // the 52px top bar rendered underneath the clock, and the composer sat below
    // the gesture pill. Read the insets natively and publish them as
    // --sa-top/-bottom/-left/-right and --kb, which the shell CSS consumes.

    private String lastPushed = "";

    private void installInsetBridge() {
        WebView wv = getBridge() != null ? getBridge().getWebView() : null;
        if (wv == null) return;
        // Read-only listener: return the insets untouched, so the Keyboard
        // plugin's own listener further up the chain keeps working.
        ViewCompat.setOnApplyWindowInsetsListener(wv, (v, insets) -> {
            pushInsets();
            return insets;
        });
        // The insets listener alone misses the IME on some OEM WebViews (the
        // animation is dispatched to the content frame, not to us). A layout
        // callback catches those, plus rotation and the gesture/3-button switch.
        wv.getViewTreeObserver().addOnGlobalLayoutListener(this::pushInsets);
        wv.post(this::pushInsets);
    }

    private void pushInsets() {
        WebView wv = getBridge() != null ? getBridge().getWebView() : null;
        if (wv == null) return;
        WindowInsetsCompat wi = ViewCompat.getRootWindowInsets(wv);
        if (wi == null) return;
        Insets bars = wi.getInsets(
            WindowInsetsCompat.Type.systemBars() | WindowInsetsCompat.Type.displayCutout());
        Insets ime = wi.getInsets(WindowInsetsCompat.Type.ime());
        float d = getResources().getDisplayMetrics().density;
        if (d <= 0) d = 1f;

        int top = Math.round(bars.top / d);
        int left = Math.round(bars.left / d);
        int right = Math.round(bars.right / d);
        int kb = Math.round(ime.bottom / d);
        // The IME inset already spans the nav-bar strip, so the two must never be
        // added: a composer floating 48px above the keyboard is that bug.
        int bottom = Math.max(0, Math.round(bars.bottom / d) - kb);

        String js =
            "(function(s){s.setProperty('--sa-top','" + top + "px');" +
            "s.setProperty('--sa-bottom','" + bottom + "px');" +
            "s.setProperty('--sa-left','" + left + "px');" +
            "s.setProperty('--sa-right','" + right + "px');" +
            "s.setProperty('--kb','" + kb + "px');" +
            "})(document.documentElement.style)";
        // onGlobalLayout fires on every frame of a scroll; only cross the bridge
        // when something actually moved.
        if (js.equals(lastPushed)) return;
        lastPushed = js;
        wv.evaluateJavascript(js, null);
    }

    // ---- status/navigation bar appearance ----
    // Concord's light/dark choice is made IN the app, not by the system setting,
    // so the platform default (bar icons follow the phone's theme) is wrong half
    // the time: dark icons on Concord's near-black top bar are invisible.
    void applySystemBarStyle(final boolean lightBackground) {
        runOnUiThread(() -> {
            View decor = getWindow().getDecorView();
            WindowInsetsControllerCompat c = WindowCompat.getInsetsController(getWindow(), decor);
            c.setAppearanceLightStatusBars(lightBackground);
            c.setAppearanceLightNavigationBars(lightBackground);
        });
    }

    // ---- app lock / screenshot privacy ----
    // The app-lock gate is a WebView repaint, which happens AFTER the OS has
    // snapshotted the app for the recents carousel — so an app sold on
    // end-to-end encryption left the last open conversation sitting in the task
    // switcher for anyone holding the phone. FLAG_SECURE blanks that snapshot
    // (and blocks screenshots and screen recording), which is what someone
    // turning on App Lock is asking for.
    void applySecureFlag(final boolean secure) {
        runOnUiThread(() -> {
            if (secure) {
                getWindow().setFlags(
                    WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE);
            } else {
                getWindow().clearFlags(WindowManager.LayoutParams.FLAG_SECURE);
            }
        });
    }
}
