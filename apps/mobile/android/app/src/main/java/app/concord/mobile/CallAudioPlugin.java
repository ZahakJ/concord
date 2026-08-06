package app.concord.mobile;

import android.content.Context;
import android.media.AudioDeviceInfo;
import android.media.AudioManager;
import android.os.Build;
import android.os.PowerManager;

import com.getcapacitor.JSArray;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

/**
 * The native half of lib/devices.js's call-audio contract: earpiece /
 * loudspeaker / Bluetooth for an in-progress call. The WebView cannot do any
 * of this (no setSinkId, no audiooutput devices — the route belongs to
 * AudioManager), so the JS side self-hides its route button until this plugin
 * exists and calls:
 *
 *   setRoute({route: "earpiece"|"speaker"|"bluetooth"}) -> {route}   (what stuck)
 *   getRoute() -> {route, available: string[]}
 *   reset()    — call ended: MODE_NORMAL, default route, locks released
 *
 * On the earpiece a proximity wake lock blanks the screen against the cheek —
 * without it the ear mashes the mute button, which is the classic phone-call
 * bug the platform lock exists for.
 */
@CapacitorPlugin(name = "CallAudio")
public class CallAudioPlugin extends Plugin {
    private PowerManager.WakeLock proximity;

    private AudioManager am() {
        return (AudioManager) getContext().getSystemService(Context.AUDIO_SERVICE);
    }

    @PluginMethod
    public void setRoute(PluginCall call) {
        String route = call.getString("route", "earpiece");
        AudioManager am = am();
        // Communication mode is what points the chosen device at the CALL
        // stream (and enables echo cancellation tuning); without it the
        // earpiece/speaker split doesn't apply to WebRTC audio at all.
        am.setMode(AudioManager.MODE_IN_COMMUNICATION);
        boolean ok = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S
            ? setRouteModern(am, route)
            : setRouteLegacy(am, route);
        if (!ok) {
            // The asked-for device isn't there (unpaired headset, a tablet with
            // no earpiece): loudspeaker is the one route every device has.
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) setRouteModern(am, "speaker");
            else setRouteLegacy(am, "speaker");
        }
        // The lock follows the route that actually took effect, not the ask.
        holdProximity("earpiece".equals(currentRoute(am)));
        JSObject r = new JSObject();
        r.put("route", currentRoute(am));
        call.resolve(r);
    }

    @PluginMethod
    public void getRoute(PluginCall call) {
        AudioManager am = am();
        JSObject r = new JSObject();
        r.put("route", currentRoute(am));
        JSArray avail = new JSArray();
        boolean earpiece = false;
        boolean bt = false;
        // getDevices (not getAvailableCommunicationDevices) so this also works
        // pre-31 — presence is the question here, not selection.
        for (AudioDeviceInfo d : am.getDevices(AudioManager.GET_DEVICES_OUTPUTS)) {
            if (d.getType() == AudioDeviceInfo.TYPE_BUILTIN_EARPIECE) earpiece = true;
            if (isBluetooth(d.getType())) bt = true;
        }
        if (earpiece) avail.put("earpiece");
        avail.put("speaker");
        if (bt) avail.put("bluetooth");
        r.put("available", avail);
        call.resolve(r);
    }

    /** Call over: default route, normal mode, screen allowed to behave. */
    @PluginMethod
    public void reset(PluginCall call) {
        AudioManager am = am();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            am.clearCommunicationDevice();
        } else {
            am.setSpeakerphoneOn(false);
            am.stopBluetoothSco();
            am.setBluetoothScoOn(false);
        }
        am.setMode(AudioManager.MODE_NORMAL);
        holdProximity(false);
        call.resolve();
    }

    // API 31+: pick from the devices the platform says can carry a call.
    private boolean setRouteModern(AudioManager am, String route) {
        for (AudioDeviceInfo d : am.getAvailableCommunicationDevices()) {
            int t = d.getType();
            boolean match = "speaker".equals(route) ? t == AudioDeviceInfo.TYPE_BUILTIN_SPEAKER
                : "bluetooth".equals(route) ? isBluetooth(t)
                : t == AudioDeviceInfo.TYPE_BUILTIN_EARPIECE;
            if (match) return am.setCommunicationDevice(d);
        }
        return false;
    }

    // Pre-31 fallback: the speakerphone flag plus SCO for Bluetooth.
    @SuppressWarnings("deprecation")
    private boolean setRouteLegacy(AudioManager am, String route) {
        if ("bluetooth".equals(route)) {
            am.setSpeakerphoneOn(false);
            am.startBluetoothSco();
            am.setBluetoothScoOn(true);
            return true;
        }
        am.stopBluetoothSco();
        am.setBluetoothScoOn(false);
        am.setSpeakerphoneOn("speaker".equals(route));
        return true;
    }

    @SuppressWarnings("deprecation")
    private String currentRoute(AudioManager am) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            AudioDeviceInfo d = am.getCommunicationDevice();
            if (d == null) return "earpiece";
            if (d.getType() == AudioDeviceInfo.TYPE_BUILTIN_SPEAKER) return "speaker";
            if (isBluetooth(d.getType())) return "bluetooth";
            return "earpiece";
        }
        if (am.isBluetoothScoOn()) return "bluetooth";
        return am.isSpeakerphoneOn() ? "speaker" : "earpiece";
    }

    private static boolean isBluetooth(int type) {
        if (type == AudioDeviceInfo.TYPE_BLUETOOTH_SCO || type == AudioDeviceInfo.TYPE_BLUETOOTH_A2DP) return true;
        // BLE audio types exist from API 31 on; compare by constant, guarded.
        return Build.VERSION.SDK_INT >= Build.VERSION_CODES.S
            && (type == AudioDeviceInfo.TYPE_BLE_HEADSET || type == AudioDeviceInfo.TYPE_BLE_SPEAKER);
    }

    // Held while (and only while) the live route is the earpiece. RELEASE_FLAG
    // waits for the face to leave the sensor, so the screen doesn't flash on
    // under the cheek during a route change.
    @SuppressWarnings("WakelockTimeout") // scoped to the call, not a duration
    private void holdProximity(boolean hold) {
        PowerManager pm = (PowerManager) getContext().getSystemService(Context.POWER_SERVICE);
        if (hold) {
            if (proximity == null && pm.isWakeLockLevelSupported(PowerManager.PROXIMITY_SCREEN_OFF_WAKE_LOCK)) {
                proximity = pm.newWakeLock(PowerManager.PROXIMITY_SCREEN_OFF_WAKE_LOCK, "concord:call");
            }
            if (proximity != null && !proximity.isHeld()) proximity.acquire();
        } else if (proximity != null && proximity.isHeld()) {
            proximity.release(PowerManager.RELEASE_FLAG_WAIT_FOR_NO_PROXIMITY);
        }
    }

    @Override
    protected void handleOnDestroy() {
        // The WebView going down mid-call must not leave a proximity lock (or
        // communication mode) orphaned on the whole phone.
        try {
            AudioManager am = am();
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) am.clearCommunicationDevice();
            am.setMode(AudioManager.MODE_NORMAL);
        } catch (Exception ignored) {
            /* audio service gone with the process — nothing left to restore */
        }
        holdProximity(false);
    }
}
