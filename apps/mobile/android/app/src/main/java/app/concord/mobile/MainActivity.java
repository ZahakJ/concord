package app.concord.mobile;

import android.os.Bundle;

import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        // Local plugins must be registered before super.onCreate wires the bridge.
        registerPlugin(ConcordCorePlugin.class);
        super.onCreate(savedInstanceState);
    }
}
