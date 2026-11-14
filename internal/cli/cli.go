// Package cli is the command-line front door every shipped Concord binary goes
// through before it does anything else.
//
// It exists because the app binaries used to ignore their arguments: running
// `concord --version` opened the default profile, bound the default port and,
// for the desktop build, relocated the binary into the install home — all to
// answer a question that needs none of it. Parse runs first in each main, and
// when it says the program is done, the program exits before touching the
// disk, the keystore or the network.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/ZahakJ/concord/internal/version"
)

// VersionLine is what --version prints: the stamped release (or "dev" for an
// unstamped build), which build of Concord this is, and the platform it was
// compiled for — the three things a bug report needs and a user can't see.
func VersionLine(flavour string) string {
	return fmt.Sprintf("concord %s (%s, %s/%s)", version.Version, flavour, runtime.GOOS, runtime.GOARCH)
}

// Parse adds --version to fs, parses args with it, and settles the cases
// where the binary must not start: --version prints VersionLine(flavour) and
// --help prints usage, both to stdout with status 0; an unknown flag or a
// stray argument prints usage to stderr with status 2, the convention for a
// command-line mistake. done reports whether the caller must exit with code.
//
// Go's flag package accepts every flag with one dash or two, so --version,
// -version, --help, -help and -h all work without listing each spelling.
func Parse(fs *flag.FlagSet, args []string, flavour, usage string, stdout, stderr io.Writer) (done bool, code int) {
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.SetOutput(stderr) // flag writes its own one-line error here
	fs.Usage = func() {}

	err := fs.Parse(stripLaunchServices(args))
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(stdout, usage)
		fmt.Fprint(stdout, BuiltInRendezvousNote())
		return true, 0
	case err != nil:
		fmt.Fprint(stderr, usage)
		return true, 2
	case fs.NArg() > 0:
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", fs.Name(), fs.Arg(0))
		fmt.Fprint(stderr, usage)
		return true, 2
	case *showVersion:
		fmt.Fprintln(stdout, VersionLine(flavour))
		if hosts := version.RendezvousHosts(); len(hosts) > 0 {
			fmt.Fprintf(stdout, "built-in rendezvous: %s\n", strings.Join(hosts, ", "))
		}
		return true, 0
	}
	return false, 0
}

// BuiltInRendezvousNote is the --help paragraph naming the rendezvous built in
// to this copy, or "" for a build with none. Said out loud because that node
// sees every new user's peer id and network address, and when they are online.
func BuiltInRendezvousNote() string {
	hosts := version.RendezvousHosts()
	if len(hosts) == 0 {
		return ""
	}
	return fmt.Sprintf("\nBuilt-in rendezvous: %s\n  This copy of Concord meets other people through that node until you set\n  another (Settings > Connection, or CONCORD_BOOTSTRAP). The node sees your\n  peer id, your network address and when you are online; it cannot read\n  anything you send.\n", strings.Join(hosts, ", "))
}

// stripLaunchServices drops the -psn_<n>_<n> argument macOS has handed to
// apps opened from Finder. Current releases no longer send it, but an app
// that exited with a usage error on an older one would never open at all.
func stripLaunchServices(args []string) []string {
	out := args[:0:0]
	for _, a := range args {
		if !strings.HasPrefix(a, "-psn_") {
			out = append(out, a)
		}
	}
	return out
}
