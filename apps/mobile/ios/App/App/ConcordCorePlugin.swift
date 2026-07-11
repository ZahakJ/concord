import Foundation
import Capacitor
import Concord

/// ConcordCore boots the Go core (Concord.xcframework, built by `make
/// ios-core` on a Mac) inside this process and hands the webview the loopback
/// port + bearer token it needs to reach /rpc and /events. The frontend calls
/// start() once from main.js before mounting.
///
/// The data dir lives under Application Support and is excluded from backups:
/// restoring MLS ratchet state onto another install forks the group state
/// irrecoverably. The 24-word mnemonic is the only supported migration.
@objc(ConcordCorePlugin)
public class ConcordCorePlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "ConcordCorePlugin"
    public let jsName = "ConcordCore"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "start", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "stop", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "nudge", returnType: CAPPluginReturnPromise),
    ]

    private static var node: ConcordNode?
    private static let lock = NSLock()

    @objc func start(_ call: CAPPluginCall) {
        Self.lock.lock()
        defer { Self.lock.unlock() }
        do {
            if Self.node == nil {
                let support = try FileManager.default.url(
                    for: .applicationSupportDirectory, in: .userDomainMask,
                    appropriateFor: nil, create: true)
                var dataDir = support.appendingPathComponent("concord", isDirectory: true)
                try FileManager.default.createDirectory(at: dataDir, withIntermediateDirectories: true)
                var noBackup = URLResourceValues()
                noBackup.isExcludedFromBackup = true
                try dataDir.setResourceValues(noBackup)

                var err: NSError?
                guard let node = ConcordStart(dataDir.path, &err) else {
                    throw err ?? NSError(domain: "concord", code: 1)
                }
                Self.node = node
            }
            call.resolve([
                "port": Self.node!.port(),
                "token": Self.node!.token(),
            ])
        } catch {
            call.reject("failed to start core: \(error.localizedDescription)")
        }
    }

    @objc func stop(_ call: CAPPluginCall) {
        Self.lock.lock()
        defer { Self.lock.unlock() }
        Self.node?.stop()
        Self.node = nil
        call.resolve()
    }

    @objc func nudge(_ call: CAPPluginCall) {
        Self.node?.nudge()
        call.resolve()
    }
}
