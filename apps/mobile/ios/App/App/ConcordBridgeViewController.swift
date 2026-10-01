import UIKit
import Capacitor

/// The app's root view controller. Capacitor only auto-registers plugins that
/// come from npm packages; ConcordCore lives in this target, so it has to be
/// registered by hand here, before the web layer calls ConcordCore.start().
class ConcordBridgeViewController: CAPBridgeViewController {
    override open func capacitorDidLoad() {
        bridge?.registerPluginInstance(ConcordCorePlugin())
    }

    override open func router() -> Router {
        return ConcordRouter()
    }
}

/// Maps capacitor://localhost/<path> onto the bundled web app.
///
/// Capacitor's own router decides "is this an SPA route?" from
/// URL(fileURLWithPath: path).pathExtension. For the app's first request the
/// path is empty, and an empty file path resolves against the working
/// directory — inside the app that is App.app, whose extension is "app" — so
/// the router served the web root directory itself and the load failed with
/// "The file “public” couldn't be opened". Decide from the path string instead.
struct ConcordRouter: Router {
    var basePath: String = ""

    func route(for path: String) -> String {
        let last = path.split(separator: "/").last.map(String.init) ?? ""
        if !last.contains(".") {
            return basePath + "/index.html"
        }
        return basePath + path
    }
}
