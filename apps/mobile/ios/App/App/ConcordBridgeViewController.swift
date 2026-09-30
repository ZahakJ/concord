import UIKit
import Capacitor

/// The app's root view controller. Capacitor only auto-registers plugins that
/// come from npm packages; ConcordCore lives in this target, so it has to be
/// registered by hand here, before the web layer calls ConcordCore.start().
class ConcordBridgeViewController: CAPBridgeViewController {
    override open func capacitorDidLoad() {
        bridge?.registerPluginInstance(ConcordCorePlugin())
    }
}
