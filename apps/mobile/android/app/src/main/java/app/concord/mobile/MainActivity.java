package app.concord.mobile;

import android.Manifest;
import android.content.pm.PackageManager;
import android.os.Build;
import android.os.Bundle;

import androidx.core.app.ActivityCompat;
import androidx.core.content.ContextCompat;

import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        // Local plugins must be registered before super.onCreate wires the bridge.
        registerPlugin(ConcordCorePlugin.class);
        super.onCreate(savedInstanceState);

        // Android 13+ needs a runtime grant before ANY notification (the
        // foreground-service one AND message notifications) can be shown. Ask
        // once on launch; if declined, delivery still works — just no tray alerts.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU
            && ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
               != PackageManager.PERMISSION_GRANTED) {
            ActivityCompat.requestPermissions(
                this, new String[]{Manifest.permission.POST_NOTIFICATIONS}, 1001);
        }
    }
}
