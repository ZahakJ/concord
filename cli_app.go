package main

// appEnvHelp is the environment both app builds read (internal/app and
// internal/bridge), shared by their --help texts so the two cannot drift.
const appEnvHelp = `  CONCORD_HOME          keep the profile (identity, chats, settings) in this
                        directory instead of the default concord folder in
                        the user config directory (~/.config/concord on Linux,
                        ~/Library/Application Support/concord on macOS,
                        %AppData%\concord on Windows)
  CONCORD_BOOTSTRAP     comma-separated rendezvous multiaddrs to bootstrap from
  CONCORD_DISABLE_MDNS  set to 1 to stop finding peers on the local network
  CONCORD_GUEST_BASE    origin of the guest gateway that meeting links point at
                        (default: derived from the rendezvous address)
`
