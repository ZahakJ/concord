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
import com.getcapacitor.WebViewListener;

public class MainActivity extends BridgeActivity {
    // The web layer signals its first paint (App.svelte's onMount) so the system
    // splash can stay up across the WebView load AND the Go core boot instead of
    // handing over to a blank window for an indeterminate few seconds. The
    // deadline is the safety net: if the bridge never comes up, holding the
    // splash forever would be worse than showing whatever did load.
    private static volatile boolean webReady = false;
    // The live activity, so markWebReady() (a static callback from the plugin)
    // can re-push the insets onto the freshly-loaded document.
    private static volatile MainActivity current = null;
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
        current = this;
        installInsetBridge();
    }

    @Override
    public void onDestroy() {
        if (current == this) current = null;
        super.onDestroy();
    }

    /** Called from ConcordCorePlugin once the web layer has mounted. */
    static void markWebReady() {
        webReady = true;
        // The insets are published by writing INLINE STYLES onto
        // document.documentElement, and a page load wipes those. The first push
        // happens from onCreate, before the WebView has loaded anything, so its
        // values were written onto a document that was then thrown away — and
        // because pushInsets() de-duplicates on the value it sent, it saw no
        // change afterwards and never wrote them again. Result: every safe-area
        // rule in the app resolved to 0 on a real phone, which is exactly the
        // "it still draws under the status bar" this bridge was added to fix.
        //
        // Every page load ends here (this runs on mount, including the reload
        // after sign-out), so this is the right place to forget what we sent and
        // say it again.
        MainActivity self = current;
        if (self != null) {
            self.runOnUiThread(() -> {
                self.lastPushed = "";
                self.pushInsets();
            });
        }
    }

    // ---- window insets → CSS custom properties ----
    // env(safe-area-inset-*) is dead weight in the Android WebView: it reports
    // display cutouts and nothing else — never the status bar, the gesture nav
    // bar, or the IME. So every safe-area rule in the stylesheet resolved to 0,
    // the 52px top bar rendered underneath the clock, and the composer sat below
    // the gesture pill. Read the insets natively and publish them as
    // --sa-top/-bottom/-left/-right and --kb, which the shell CSS consumes.
    //
    // WHO OWNS THE WEBVIEW'S GEOMETRY (this is where every prior fix died):
    // Capacitor 8's built-in SystemBars plugin installs its own insets listener
    // on the WebView's parent and picks ONE of two layouts per device:
    //   - WebView (Chromium) < 140, API 35+: it PADS the parent, so the WebView
    //     is laid out below the status bar (the emulator, WebView 124, is here);
    //   - WebView >= 140 AND the page has viewport-fit=cover: it removes the
    //     padding and leaves the WebView FULL-BLEED (any phone with a current
    //     Play-updated WebView is here).
    // The choice also flips at runtime, because the viewport check lands async
    // after DOM-ready. Every "verified on the emulator" was verified in the
    // padded configuration, which a real phone is not in — and cannot be put in,
    // because its WebView auto-updates. That is why fixes kept not reproducing.
    //
    // So the configuration fork is now REMOVED at the source:
    // capacitor.config.json sets plugins.SystemBars.insetsHandling = "disable",
    // which stops Capacitor from installing its listener at all. Nothing pads
    // the parent on any device: the WebView is deterministically full-bleed and
    // this bridge is the only thing translating insets for the page.
    //
    // Belt and braces on top of that, in case some OEM or future Capacitor
    // version insets the WebView anyway:
    //   - the remaining-inset subtraction below publishes only what the platform
    //     did NOT already do;
    //   - the pushed script also publishes the RAW bar height (--sa-bars-top)
    //     and installs a tiny page-side floor: whenever the page can see that
    //     its viewport spans the whole screen (innerHeight >= screen.height) it
    //     floors the top inset at the raw bar height, so a native measurement
    //     gone wrong can never silently produce an unpadded status bar again.

    private String lastPushed = "";

    private void installInsetBridge() {
        WebView wv = getBridge() != null ? getBridge().getWebView() : null;
        if (wv == null) return;
        // NOTE: this listener REPLACES the WebView's own onApplyWindowInsets
        // (that is how View listeners work), so Chromium >= 140 never gets to
        // resolve env(safe-area-inset-*) itself. Deliberate: the page must see
        // ONE consistent source of truth (--sa-*), not two racing ones.
        ViewCompat.setOnApplyWindowInsetsListener(wv, (v, insets) -> {
            pushInsets();
            // The view geometry read by pushInsets() can be one layout pass
            // stale during an insets dispatch (padding changes apply on the
            // NEXT layout). Re-push after this traversal settles; the value
            // dedup makes it free when nothing actually moved.
            v.post(this::pushInsets);
            return insets;
        });
        // The insets listener alone misses the IME on some OEM WebViews (the
        // animation is dispatched to the content frame, not to us). A layout
        // callback catches those, plus rotation and the gesture/3-button switch.
        wv.getViewTreeObserver().addOnGlobalLayoutListener(this::pushInsets);
        wv.post(this::pushInsets);
        // Every NAVIGATION wipes the inline --sa-* styles with the old document,
        // but lastPushed still says "already sent" — so every later push was
        // being deduplicated away and the new document rendered with 0 insets.
        // markWebReady() covers the pages that reach App.svelte's onMount; this
        // covers the ones that don't (a crashed bundle, a stuck boot screen, any
        // future page): forget what was sent the moment a new document paints.
        // (Capacitor itself fires requestApplyInsets at page commit — it was the
        // dedup that swallowed it.)
        if (getBridge() != null) {
            getBridge().addWebViewListener(
                new WebViewListener() {
                    @Override
                    public void onPageCommitVisible(WebView view, String url) {
                        runOnUiThread(() -> {
                            lastPushed = "";
                            pushInsets();
                        });
                    }
                }
            );
        }
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

        // Publish only the inset the CSS still has to make up for: measure where
        // the WebView actually sits and subtract what the platform already did.
        // With SystemBars insets handling disabled nothing should be padding the
        // parent, so the subtraction is normally zero — it exists so that a
        // platform that DOES inset the WebView (see the header comment) yields a
        // correct value instead of a double band.
        //
        // Window coordinates, not screen: insets are window-relative, and the
        // two differ whenever the window itself is offset (multi-window, DeX,
        // Samsung's "hide camera cutout" letterboxing).
        int[] loc = new int[2];
        wv.getLocationInWindow(loc);
        int wvTop = loc[1];
        int wvBottom = loc[1] + wv.getHeight();
        View root = wv.getRootView();
        int winH = root != null ? root.getHeight() : 0;
        if (winH <= 0) winH = getResources().getDisplayMetrics().heightPixels;

        int topPx = Math.max(0, bars.top - wvTop);
        int bottomBarsPx = Math.max(0, bars.bottom - Math.max(0, winH - wvBottom));
        int imePx = Math.max(0, ime.bottom - Math.max(0, winH - wvBottom));

        int top = Math.round(topPx / d);
        // Some OEM WebViews report a 0 top inset — before the first layout pass,
        // and on a few builds persistently. Trusting that puts the app's own top
        // bar underneath the clock. Only fall back when the WebView is genuinely
        // full-bleed at the top, or this would re-introduce the double band.
        int barsTopDp = Math.round(bars.top / d);
        if (top <= 0 && wvTop <= 0 && bars.top <= 0) {
            int px = 0;
            int resId = getResources().getIdentifier("status_bar_height", "dimen", "android");
            if (resId > 0) px = getResources().getDimensionPixelSize(resId);
            top = px > 0 ? Math.round(px / d) : 24;
            barsTopDp = top;
        }
        int left = Math.round(bars.left / d);
        int right = Math.round(bars.right / d);
        int kb = Math.round(imePx / d);
        // The IME inset already spans the nav-bar strip, so the two must never be
        // added: a composer floating 48px above the keyboard is that bug.
        int bottom = Math.max(0, Math.round(bottomBarsPx / d) - kb);

        String js =
            "(function(s){s.setProperty('--sa-top','" + top + "px');" +
            "s.setProperty('--sa-bottom','" + bottom + "px');" +
            "s.setProperty('--sa-left','" + left + "px');" +
            "s.setProperty('--sa-right','" + right + "px');" +
            "s.setProperty('--kb','" + kb + "px');" +
            "s.setProperty('--sa-bars-top','" + barsTopDp + "px');" +
            // A per-document history of what this bridge pushed, readable from
            // the in-app diagnostics (Settings → Connection → Stats). This is
            // the logcat we cannot get from a user's phone: a correct top later
            // replaced by 0 shows up as two entries. Values are dp except the
            // *Px fields, which are the raw native measurements.
            "var L=window.__saLog=window.__saLog||[];" +
            "L.push({t:Date.now(),top:" + top + ",bottom:" + bottom + ",kb:" + kb +
            ",barsTopPx:" + bars.top + ",wvTopPx:" + wvTop + ",winHPx:" + winH +
            ",d:" + d + "});" +
            "if(L.length>40)L.shift();" +
            // The floor: measured by the renderer itself, so it stays right even
            // if the native geometry above ever lies. Reinstalling it here (and
            // only here) means a page reload — which wipes both the inline
            // styles and window.__saFloor — heals on the markWebReady re-push.
            "if(!window.__saFloor){window.__saFloor=function(){" +
            // -2, not -1: innerHeight and screen.height round the same physical
            // size through devicePixelRatio separately (measured: 914 vs 915 on
            // WebView 145 @2.625x — a 1px disagreement on a genuinely full-bleed
            // screen). A platform-padded WebView is short by a whole bar (24dp+),
            // so a 2px tolerance cannot misfire the floor.
            "var fb=window.innerHeight>=window.screen.height-2;" +
            "document.documentElement.style.setProperty('--sa-floor-top'," +
            "fb?(document.documentElement.style.getPropertyValue('--sa-bars-top')||'0px'):'0px');" +
            "};addEventListener('resize',window.__saFloor);}" +
            "window.__saFloor();" +
            "})(document.documentElement.style)";
        // onGlobalLayout fires on every frame of a scroll; only cross the bridge
        // when something actually moved.
        if (js.equals(lastPushed)) return;
        lastPushed = js;
        // One line per actual change; readable on a real phone via
        //   adb logcat -s ConcordInsets
        // — the missing eyes every previous round of this bug was fixed without.
        android.util.Log.d("ConcordInsets",
            "bars=" + bars + " ime.bottom=" + ime.bottom +
            " wv=[" + wvTop + "," + wvBottom + "] winH=" + winH +
            " -> top=" + top + " bottom=" + bottom + " kb=" + kb);
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
