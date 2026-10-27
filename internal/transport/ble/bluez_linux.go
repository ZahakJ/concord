//go:build linux && !android

package ble

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/ZahakJ/concord/internal/transport"
)

const (
	bluezBus      = "org.bluez"
	ifAdapter     = "org.bluez.Adapter1"
	ifDevice      = "org.bluez.Device1"
	ifGattMgr     = "org.bluez.GattManager1"
	ifAdvMgr      = "org.bluez.LEAdvertisingManager1"
	ifAdvert      = "org.bluez.LEAdvertisement1"
	ifService     = "org.bluez.GattService1"
	ifChar        = "org.bluez.GattCharacteristic1"
	ifProps       = "org.freedesktop.DBus.Properties"
	ifObjMgr      = "org.freedesktop.DBus.ObjectManager"
	genericAlias  = "Bluetooth"
	maxKnown      = 4096 // devices classified as ours / not ours
	maxDevices    = 256  // Concord devices tracked
	maxWriters    = 64   // centrals remembered for having written to us
	maxInbound    = 7    // Concord centrals allowed connected to us at once
	connectWait   = 20 * time.Second
	resolveWait   = 15 * time.Second
	bluezCallWait = 5 * time.Second
	cleanupWait   = 3 * time.Second
	sweepEvery    = 10 * time.Second
)

// ErrNoPrivacy is why Start refuses an adapter without LE privacy.
var ErrNoPrivacy = errors.New("ble: LE privacy is off on this adapter, so it would advertise its fixed public address; " +
	"set `Privacy = device` under [General] in /etc/bluetooth/main.conf and restart bluetooth")

// BlueZConfig chooses and constrains the adapter.
type BlueZConfig struct {
	// Adapter is the adapter name, e.g. "hci0". Empty picks the first.
	Adapter string
	// AllowPublicAddress lets the link run on an adapter without LE
	// privacy. Such an adapter advertises and connects from its fixed public
	// address, which anyone nearby can log and follow. Concord itself never
	// sets this. It exists so that concord-blecheck can exercise the radio
	// on a test machine where privacy cannot be turned on.
	AllowPublicAddress bool
}

// BlueZ is the Linux Radio: BlueZ's GATT server, advertising and discovery
// APIs over the system D-Bus.
//
// Everything it registers lives on its own private bus connection, so if the
// process dies, BlueZ drops the GATT service, the advertisement and the
// discovery session by itself. A crash cannot do the same for the adapter
// alias, the Pairable setting, connections, or devices in BlueZ's cache;
// Down handles those, and Up repairs a crashed run's alias.
type BlueZ struct {
	cfg  BlueZConfig
	base dbus.ObjectPath

	mu           sync.Mutex
	conn         *dbus.Conn
	adapter      dbus.ObjectPath
	ev           Events
	advOn        bool
	appOn        bool
	wantScan     bool
	scanning     bool
	notifying    bool
	poweredByUs  bool
	aliasSet     bool
	savedAlias   string
	systemName   string
	pairableSet  bool
	devices      map[dbus.ObjectPath]*devState       // Concord devices heard
	known        map[dbus.ObjectPath]bool            // classification cache: ours?
	dialed       map[dbus.ObjectPath]bool            // devices we Connect()ed, including failed attempts
	writers      map[dbus.ObjectPath]time.Time       // centrals that wrote to us
	inboundSince map[dbus.ObjectPath]time.Time       // Concord centrals connected to us, since
	txPaths      map[dbus.ObjectPath]dbus.ObjectPath // remote notify char → device
	health       string                              // why the radio is not doing its job, if it isn't
	recovering   bool
	quit         chan struct{}
	bg           sync.WaitGroup
	sigDone      chan struct{}
}

type devState struct {
	connected bool
	lastHeard time.Time
}

var bluezInstances atomic.Int64

// NewBlueZ returns a radio for cfg. It touches nothing until Probe or Up.
func NewBlueZ(cfg BlueZConfig) *BlueZ {
	return &BlueZ{cfg: cfg, base: dbus.ObjectPath(fmt.Sprintf("/org/concord/ble%d", bluezInstances.Add(1)))}
}

// Default is the link Concord registers: the first adapter, with privacy
// required.
func Default() *Link { return New(NewBlueZ(BlueZConfig{})) }

func (b *BlueZ) appPath() dbus.ObjectPath { return b.base }
func (b *BlueZ) svcPath() dbus.ObjectPath { return b.base + "/service0" }
func (b *BlueZ) rxPath() dbus.ObjectPath  { return b.base + "/service0/rx" }
func (b *BlueZ) txPath() dbus.ObjectPath  { return b.base + "/service0/tx" }
func (b *BlueZ) advPath() dbus.ObjectPath { return b.base + "/advertisement0" }

// Probe looks for an adapter on D-Bus and in sysfs, and reads its privacy
// setting. It sends nothing over the air and changes nothing. While the link
// runs, it also reports a radio that has silently stopped working, for
// example one that came back from suspend without its advertisement.
func (b *BlueZ) Probe() transport.Status {
	b.mu.Lock()
	running, health := b.conn != nil, b.health
	b.mu.Unlock()
	if running && health != "" {
		return transport.Status{State: transport.Failed, Detail: health}
	}
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return transport.Status{State: transport.NoHardware, Detail: "no system D-Bus"}
	}
	defer conn.Close()
	adapter, props, err := findAdapter(conn, b.cfg.Adapter)
	if err != nil {
		return transport.Status{State: transport.NoHardware, Detail: err.Error()}
	}
	name := filepath.Base(string(adapter))
	if hard, soft := rfkilled(name); hard {
		return transport.Status{State: transport.Failed, Detail: name + " is switched off by a hardware switch"}
	} else if soft {
		return transport.Status{State: transport.Failed, Detail: name + " is blocked (rfkill); unblock Bluetooth in system settings"}
	}
	priv, err := adapterPrivacy(adapter)
	switch {
	case err != nil && !b.cfg.AllowPublicAddress:
		return transport.Status{State: transport.Failed, Detail: "cannot verify LE privacy: " + err.Error()}
	case !priv && !b.cfg.AllowPublicAddress:
		return transport.Status{State: transport.Failed, Detail: ErrNoPrivacy.Error()}
	}
	detail := name
	if !priv {
		detail += ": PUBLIC ADDRESS — privacy is off"
	}
	if p, _ := props["Powered"].Value().(bool); !p {
		if running {
			return transport.Status{State: transport.Failed, Detail: name + " was switched off; the link resumes when it is back on"}
		}
		detail += " (off; enabling this link turns it on)"
	}
	return transport.Status{State: transport.Available, Detail: detail}
}

// Up powers the adapter if needed, hides its name, refuses pairing, serves
// the GATT service and starts listening to BlueZ's signals.
func (b *BlueZ) Up(ctx context.Context, ev Events) (err error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("ble: system bus: %w", err)
	}
	adapter, props, err := findAdapter(conn, b.cfg.Adapter)
	if err != nil {
		conn.Close()
		return err
	}
	name := filepath.Base(string(adapter))
	if hard, soft := rfkilled(name); hard || soft {
		conn.Close()
		return fmt.Errorf("ble: %s is blocked by rfkill", name)
	}
	if !b.cfg.AllowPublicAddress {
		priv, err := adapterPrivacy(adapter)
		if err != nil {
			conn.Close()
			return fmt.Errorf("ble: cannot verify LE privacy: %w", err)
		}
		if !priv {
			conn.Close()
			return ErrNoPrivacy
		}
	}

	b.mu.Lock()
	b.conn, b.adapter, b.ev = conn, adapter, ev
	b.devices = map[dbus.ObjectPath]*devState{}
	b.known = map[dbus.ObjectPath]bool{}
	b.dialed = map[dbus.ObjectPath]bool{}
	b.writers = map[dbus.ObjectPath]time.Time{}
	b.inboundSince = map[dbus.ObjectPath]time.Time{}
	b.txPaths = map[dbus.ObjectPath]dbus.ObjectPath{}
	b.sigDone, b.quit, b.health, b.recovering = nil, make(chan struct{}), "", false
	b.notifying, b.advOn, b.appOn, b.scanning, b.wantScan = false, false, false, false, false
	b.poweredByUs, b.aliasSet, b.pairableSet = false, false, false
	b.mu.Unlock()
	defer func() {
		if err != nil {
			_ = b.Down()
		}
	}()

	ad := conn.Object(bluezBus, adapter)
	if p, _ := props["Powered"].Value().(bool); !p {
		if err := ad.SetProperty(ifAdapter+".Powered", dbus.MakeVariant(true)); err != nil {
			return fmt.Errorf("ble: power on %s: %w", name, err)
		}
		b.mu.Lock()
		b.poweredByUs = true
		b.mu.Unlock()
		if err := waitFor(ctx, 5*time.Second, func() bool {
			v, err := ad.GetProperty(ifAdapter + ".Powered")
			p, _ := v.Value().(bool)
			return err == nil && p
		}); err != nil {
			return fmt.Errorf("ble: %s did not power on", name)
		}
	}

	// Anyone who connects can read the GAP Device Name characteristic, and on
	// Linux that is the adapter alias, usually the hostname. Swap it for a
	// generic one while the link runs. If the alias already reads "Bluetooth"
	// but the system name does not, an earlier run crashed before restoring
	// it, so the system name is what to restore.
	alias, _ := props["Alias"].Value().(string)
	sysName, _ := props["Name"].Value().(string)
	saved := alias
	if alias == genericAlias && sysName != genericAlias {
		saved = sysName
	}
	if alias != genericAlias {
		if err := ad.SetProperty(ifAdapter+".Alias", dbus.MakeVariant(genericAlias)); err != nil {
			return fmt.Errorf("ble: set generic alias: %w", err)
		}
	}
	b.mu.Lock()
	b.savedAlias, b.systemName, b.aliasSet = saved, sysName, saved != genericAlias
	b.mu.Unlock()

	// Bonding would hand a stranger our IRK and identity address, which
	// resolve every private address we will ever use. Refuse pairing while
	// the link runs.
	if p, _ := props["Pairable"].Value().(bool); p {
		if err := ad.SetProperty(ifAdapter+".Pairable", dbus.MakeVariant(false)); err != nil {
			return fmt.Errorf("ble: refuse pairing: %w", err)
		}
		b.mu.Lock()
		b.pairableSet = true
		b.mu.Unlock()
	}

	if err := b.export(conn); err != nil {
		return err
	}

	sigCh := make(chan *dbus.Signal, 256)
	sigDone := make(chan struct{})
	for _, opts := range [][]dbus.MatchOption{
		{dbus.WithMatchSender(bluezBus), dbus.WithMatchInterface(ifObjMgr), dbus.WithMatchMember("InterfacesAdded")},
		{dbus.WithMatchSender(bluezBus), dbus.WithMatchInterface(ifObjMgr), dbus.WithMatchMember("InterfacesRemoved")},
		{dbus.WithMatchSender(bluezBus), dbus.WithMatchInterface(ifProps), dbus.WithMatchMember("PropertiesChanged"),
			dbus.WithMatchPathNamespace(adapter)},
		{dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"),
			dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, bluezBus)},
	} {
		if err := conn.AddMatchSignal(opts...); err != nil {
			return fmt.Errorf("ble: subscribe to BlueZ signals: %w", err)
		}
	}
	b.mu.Lock()
	b.sigDone = sigDone
	b.mu.Unlock()
	conn.Signal(sigCh)
	go b.signals(sigCh, sigDone)

	if err := b.call(ctx, adapter, ifGattMgr+".RegisterApplication", b.appPath(), map[string]dbus.Variant{}); err != nil {
		return fmt.Errorf("ble: register GATT service: %w", err)
	}
	b.mu.Lock()
	b.appOn = true
	quit := b.quit
	b.bg.Add(1)
	b.mu.Unlock()
	go b.sweeper(quit)
	// Devices already in BlueZ's cache are deliberately not seeded. A Concord
	// device counts only once it is heard on air during this run.
	return nil
}

// Advertise (re)registers the advertisement. Any earlier registration is
// withdrawn first, whatever we believe its state to be: BlueZ keeps an
// advertisement registered across an adapter power cycle.
func (b *BlueZ) Advertise() error {
	b.mu.Lock()
	conn, adapter := b.conn, b.adapter
	b.mu.Unlock()
	if conn == nil {
		return ErrNotRunning
	}
	ctx := context.Background()
	_ = b.call(ctx, adapter, ifAdvMgr+".UnregisterAdvertisement", b.advPath())
	err := b.call(ctx, adapter, ifAdvMgr+".RegisterAdvertisement", b.advPath(), map[string]dbus.Variant{})
	b.mu.Lock()
	b.advOn = err == nil
	b.mu.Unlock()
	if err != nil {
		return fmt.Errorf("ble: register advertisement: %w", err)
	}
	return nil
}

// Scan starts or stops LE discovery, filtered to the Concord service UUID.
func (b *BlueZ) Scan(on bool) error {
	b.mu.Lock()
	conn, adapter, scanning := b.conn, b.adapter, b.scanning
	b.wantScan = on
	b.mu.Unlock()
	if conn == nil {
		return ErrNotRunning
	}
	ctx := context.Background()
	if on && !scanning {
		if err := b.startDiscovery(ctx, adapter); err != nil {
			return err
		}
	} else if !on && scanning {
		if err := b.call(ctx, adapter, ifAdapter+".StopDiscovery"); err != nil {
			return fmt.Errorf("ble: stop discovery: %w", err)
		}
	}
	b.mu.Lock()
	b.scanning = on
	b.mu.Unlock()
	return nil
}

func (b *BlueZ) startDiscovery(ctx context.Context, adapter dbus.ObjectPath) error {
	filter := map[string]dbus.Variant{
		"Transport": dbus.MakeVariant("le"),
		"UUIDs":     dbus.MakeVariant([]string{ServiceUUID}),
		// Keep reporting devices that stay in range, so that they are not
		// forgotten as stale.
		"DuplicateData": dbus.MakeVariant(true),
	}
	if err := b.call(ctx, adapter, ifAdapter+".SetDiscoveryFilter", filter); err != nil {
		return fmt.Errorf("ble: discovery filter: %w", err)
	}
	if err := b.call(ctx, adapter, ifAdapter+".StartDiscovery"); err != nil && !isDBusError(err, "org.bluez.Error.InProgress") {
		return fmt.Errorf("ble: start discovery: %w", err)
	}
	return nil
}

// Notify sends frame to every subscribed central. BlueZ turns a change of
// the characteristic's Value into one notification per subscriber, and drops
// it if there are none. So it is emitted whatever our view of the
// subscription state is.
func (b *BlueZ) Notify(frame []byte) error {
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	if conn == nil {
		return ErrNotRunning
	}
	return conn.Emit(b.txPath(), ifProps+".PropertiesChanged", ifChar,
		map[string]dbus.Variant{"Value": dbus.MakeVariant(frame)}, []string{})
}

// Connect dials a Concord device, waits for its services, checks that the
// MTU fits a whole frame, and subscribes to its notifications. It never
// pairs. If anything fails, including a cancelled or timed-out Connect that
// BlueZ might still complete, the device is disconnected and removed from
// BlueZ's cache.
func (b *BlueZ) Connect(ctx context.Context, id string) (PeerConn, error) {
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	if conn == nil {
		return nil, ErrNotRunning
	}
	path := dbus.ObjectPath(id)
	if !path.IsValid() || !b.under(path) {
		return nil, fmt.Errorf("ble: bad device %q", id)
	}
	b.mu.Lock()
	if b.dialed != nil {
		b.dialed[path] = true
	}
	b.mu.Unlock()
	c := &bluezConn{b: b, conn: conn, dev: path}
	fail := func(err error) (PeerConn, error) {
		_ = c.Close()
		return nil, err
	}
	dev := conn.Object(bluezBus, path)
	cctx, cancel := context.WithTimeout(ctx, connectWait)
	err := dev.CallWithContext(cctx, ifDevice+".Connect", 0).Err
	cancel()
	if err != nil {
		return fail(fmt.Errorf("ble: connect: %w", err))
	}
	if err := waitFor(ctx, resolveWait, func() bool {
		v, err := dev.GetProperty(ifDevice + ".ServicesResolved")
		ok, _ := v.Value().(bool)
		return err == nil && ok
	}); err != nil {
		return fail(fmt.Errorf("ble: services not resolved: %w", err))
	}
	objs, err := managedObjects(conn)
	if err != nil {
		return fail(err)
	}
	prefix := string(path) + "/"
	for p, ifaces := range objs {
		ch, ok := ifaces[ifChar]
		if !ok || !strings.HasPrefix(string(p), prefix) {
			continue
		}
		u, _ := ch["UUID"].Value().(string)
		switch strings.ToLower(u) {
		case RxUUID:
			c.rx = p
		case TxUUID:
			c.tx = p
		}
	}
	if c.rx == "" || c.tx == "" {
		return fail(errors.New("ble: device does not serve the Concord characteristics"))
	}
	// MTU has been exposed since BlueZ 5.62. If it is absent, a write that
	// is too long fails loudly instead of truncating, and the peer is
	// dropped.
	if v, err := conn.Object(bluezBus, c.rx).GetProperty(ifChar + ".MTU"); err == nil {
		if mtu, ok := v.Value().(uint16); ok && int(mtu) < MinATTMTU {
			return fail(fmt.Errorf("ble: peer ATT MTU %d is below %d", mtu, MinATTMTU))
		}
	}
	b.mu.Lock()
	if b.txPaths != nil {
		b.txPaths[c.tx] = path
	}
	b.mu.Unlock()
	if err := b.call(ctx, c.tx, ifChar+".StartNotify"); err != nil {
		return fail(fmt.Errorf("ble: subscribe: %w", err))
	}
	return c, nil
}

// Down stops discovery and withdraws the advertisement and the GATT service.
// It disconnects every Concord device in either direction and removes the
// unpaired ones from BlueZ's cache. Only after that does it restore the
// alias, Pairable and power, so that no peer still connected can read the
// real name. Finally it closes the bus connection. It is safe to call twice
// and after a failed Up.
func (b *BlueZ) Down() error {
	b.mu.Lock()
	conn := b.conn
	if conn == nil {
		b.mu.Unlock()
		return nil
	}
	quit := b.quit
	b.quit = nil
	b.mu.Unlock()
	if quit != nil {
		close(quit)
	}
	b.bg.Wait() // sweeper, recoveries

	b.mu.Lock()
	adapter := b.adapter
	scanning, advOn, appOn := b.scanning, b.advOn, b.appOn
	aliasSet, savedAlias, systemName := b.aliasSet, b.savedAlias, b.systemName
	pairableSet, poweredByUs := b.pairableSet, b.poweredByUs
	sigDone := b.sigDone
	peers := map[dbus.ObjectPath]bool{}
	for p := range b.dialed {
		peers[p] = true
	}
	for p := range b.writers {
		peers[p] = true
	}
	for p := range b.devices {
		peers[p] = true
	}
	b.mu.Unlock()

	ctx := context.Background()
	var errs []error
	if scanning {
		if err := b.call(ctx, adapter, ifAdapter+".StopDiscovery"); err != nil {
			errs = append(errs, fmt.Errorf("stop discovery: %w", err))
		}
	}
	if advOn {
		if err := b.call(ctx, adapter, ifAdvMgr+".UnregisterAdvertisement", b.advPath()); err != nil {
			errs = append(errs, fmt.Errorf("unregister advertisement: %w", err))
		}
	}
	if appOn {
		if err := b.call(ctx, adapter, ifGattMgr+".UnregisterApplication", b.appPath()); err != nil {
			errs = append(errs, fmt.Errorf("unregister GATT service: %w", err))
		}
	}

	// Every Concord peer, whichever side dialed, goes now: dialed devices,
	// centrals that wrote to us, devices heard advertising the service. A
	// paired device is never touched, because it is the user's own (a phone
	// that also runs Concord, say) and the bond already knows our name.
	var gone []dbus.ObjectPath
	for p := range peers {
		if b.paired(p) {
			continue
		}
		if b.isConnected(p) {
			b.disconnect(p)
		}
		gone = append(gone, p)
	}
	_ = waitFor(ctx, cleanupWait, func() bool {
		for _, p := range gone {
			if b.isConnected(p) {
				return false
			}
		}
		return true
	})
	for _, p := range gone {
		b.remove(p)
	}

	ad := conn.Object(bluezBus, adapter)
	if pairableSet {
		if err := ad.SetProperty(ifAdapter+".Pairable", dbus.MakeVariant(true)); err != nil {
			errs = append(errs, fmt.Errorf("restore pairable: %w", err))
		}
	}
	if aliasSet {
		// An alias equal to the system name was never really set. "" puts
		// the adapter back to following the system name.
		restore := savedAlias
		if restore == systemName {
			restore = ""
		}
		if err := ad.SetProperty(ifAdapter+".Alias", dbus.MakeVariant(restore)); err != nil {
			errs = append(errs, fmt.Errorf("restore alias: %w", err))
		} else {
			// BlueZ applies a reset alias asynchronously. Wait, so that a
			// caller reading it back after Stop sees the old name.
			_ = waitFor(ctx, 2*time.Second, func() bool {
				v, err := ad.GetProperty(ifAdapter + ".Alias")
				a, _ := v.Value().(string)
				return err == nil && a == savedAlias
			})
		}
	}
	// Power the adapter back off only if we turned it on, and only if
	// nothing else (headphones, a keyboard) has connected since.
	if poweredByUs && !anyConnected(conn, b) {
		if err := ad.SetProperty(ifAdapter+".Powered", dbus.MakeVariant(false)); err != nil {
			errs = append(errs, fmt.Errorf("power off: %w", err))
		}
	}
	for _, p := range []dbus.ObjectPath{b.appPath(), b.svcPath(), b.rxPath(), b.txPath(), b.advPath()} {
		_ = conn.Export(nil, p, ifObjMgr)
		_ = conn.Export(nil, p, ifProps)
		_ = conn.Export(nil, p, ifChar)
		_ = conn.Export(nil, p, ifAdvert)
	}
	_ = conn.Close() // ends the signal channel
	if sigDone != nil {
		<-sigDone
	}

	b.mu.Lock()
	b.conn = nil
	b.ev = Events{}
	b.devices, b.known, b.dialed, b.writers, b.inboundSince, b.txPaths = nil, nil, nil, nil, nil, nil
	b.scanning, b.wantScan, b.advOn, b.appOn, b.notifying = false, false, false, false, false
	b.aliasSet, b.pairableSet, b.poweredByUs, b.health = false, false, false, ""
	b.mu.Unlock()
	return errors.Join(errs...)
}

// Counts is a diagnostic: devices BlueZ reported during this run, and how many
// of them advertise the Concord service.
func (b *BlueZ) Counts() (all, concord int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.known), len(b.devices)
}

// Advertising reports whether our advertisement is registered.
func (b *BlueZ) Advertising() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.advOn
}

func (b *BlueZ) under(p dbus.ObjectPath) bool {
	b.mu.Lock()
	a := b.adapter
	b.mu.Unlock()
	return a != "" && strings.HasPrefix(string(p), string(a)+"/")
}

func (b *BlueZ) call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) error {
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	if conn == nil {
		return ErrNotRunning
	}
	cctx, cancel := context.WithTimeout(ctx, bluezCallWait)
	defer cancel()
	return conn.Object(bluezBus, path).CallWithContext(cctx, method, 0, args...).Err
}

// devProp reads one Device1 property with a short timeout.
func (b *BlueZ) devProp(p dbus.ObjectPath, name string) (any, bool) {
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	if conn == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), cleanupWait)
	defer cancel()
	var v dbus.Variant
	if err := conn.Object(bluezBus, p).CallWithContext(ctx, ifProps+".Get", 0, ifDevice, name).Store(&v); err != nil {
		return nil, false
	}
	return v.Value(), true
}

func (b *BlueZ) isConnected(p dbus.ObjectPath) bool {
	v, _ := b.devProp(p, "Connected")
	c, _ := v.(bool)
	return c
}

// paired is true for anything with a bond or the user's trust, and for
// anything whose state cannot be read. Those devices are never disconnected
// or removed.
func (b *BlueZ) paired(p dbus.ObjectPath) bool {
	for _, name := range []string{"Paired", "Bonded", "Trusted"} {
		v, ok := b.devProp(p, name)
		if name == "Bonded" && !ok {
			continue // Bonded was only added in BlueZ 5.67
		}
		if is, _ := v.(bool); !ok || is {
			return true
		}
	}
	return false
}

// disconnect is best effort, on a fresh short context so that it still runs
// when the caller's context is already cancelled.
func (b *BlueZ) disconnect(p dbus.ObjectPath) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupWait)
	defer cancel()
	_ = b.call(ctx, p, ifDevice+".Disconnect")
}

// remove deletes an unpaired device from BlueZ's cache. Otherwise BlueZ
// writes every device we connected to under /var/lib/bluetooth, which would
// build a growing log of who we met.
func (b *BlueZ) remove(p dbus.ObjectPath) {
	b.mu.Lock()
	adapter := b.adapter
	b.mu.Unlock()
	if b.paired(p) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cleanupWait)
	defer cancel()
	_ = b.call(ctx, adapter, ifAdapter+".RemoveDevice", p)
	b.mu.Lock()
	delete(b.dialed, p)
	delete(b.writers, p)
	delete(b.devices, p)
	delete(b.known, p)
	b.mu.Unlock()
}

// sweeper limits connections that other devices opened to us. They can only
// be kept for MaxConnAge, so that an address is not pinned past its rotation,
// and only maxInbound may be open at once, newest dropped first.
func (b *BlueZ) sweeper(quit chan struct{}) {
	defer b.bg.Done()
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-quit:
			return
		case <-t.C:
			b.sweep(time.Now())
		}
	}
}

func (b *BlueZ) sweep(now time.Time) {
	b.mu.Lock()
	cands := map[dbus.ObjectPath]bool{}
	for p, d := range b.devices {
		if d.connected && !b.dialed[p] {
			cands[p] = true
		}
	}
	for p := range b.writers {
		if !b.dialed[p] {
			cands[p] = true
		}
	}
	b.mu.Unlock()

	type in struct {
		p     dbus.ObjectPath
		since time.Time
	}
	var live []in
	for p := range cands {
		if !b.isConnected(p) || b.paired(p) {
			b.mu.Lock()
			delete(b.inboundSince, p)
			delete(b.writers, p)
			b.mu.Unlock()
			continue
		}
		b.mu.Lock()
		since, ok := b.inboundSince[p]
		if !ok {
			since = now
			b.inboundSince[p] = now
		}
		b.mu.Unlock()
		live = append(live, in{p, since})
	}
	sort.Slice(live, func(i, j int) bool { return live[i].since.Before(live[j].since) })
	for i, c := range live {
		if i >= maxInbound || now.Sub(c.since) >= MaxConnAge {
			b.disconnect(c.p)
			b.mu.Lock()
			delete(b.inboundSince, c.p)
			b.mu.Unlock()
		}
	}
	// Forget Concord devices that have not been heard in a while and are
	// not connected.
	b.mu.Lock()
	for p, d := range b.devices {
		if !d.connected && now.Sub(d.lastHeard) > 2*staleAfter {
			delete(b.devices, p)
			delete(b.known, p)
		}
	}
	b.mu.Unlock()
}

// recover re-registers the advertisement and the discovery session after the
// adapter comes back from power-off or suspend, or after bluetoothd restarts
// (pass app=true to re-register the GATT service too). It retries a few times.
// A failure is surfaced through Probe instead of leaving the Manager showing
// a running link that does nothing.
func (b *BlueZ) recover(app bool) {
	b.mu.Lock()
	if b.conn == nil || b.quit == nil || b.recovering {
		b.mu.Unlock()
		return
	}
	b.recovering = true
	quit := b.quit
	b.bg.Add(1)
	b.mu.Unlock()
	go func() {
		defer b.bg.Done()
		defer func() {
			b.mu.Lock()
			b.recovering = false
			b.mu.Unlock()
		}()
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			select {
			case <-quit:
				return
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
			if err = b.reregister(app); err == nil {
				b.mu.Lock()
				b.health = ""
				b.mu.Unlock()
				return
			}
		}
		b.mu.Lock()
		b.health = "Bluetooth came back but the link could not resume: " + err.Error()
		b.mu.Unlock()
	}()
}

func (b *BlueZ) reregister(app bool) error {
	b.mu.Lock()
	adapter, want := b.adapter, b.wantScan
	b.mu.Unlock()
	ctx := context.Background()
	if app {
		_ = b.call(ctx, adapter, ifGattMgr+".UnregisterApplication", b.appPath())
		if err := b.call(ctx, adapter, ifGattMgr+".RegisterApplication", b.appPath(), map[string]dbus.Variant{}); err != nil {
			return fmt.Errorf("register GATT service: %w", err)
		}
		b.mu.Lock()
		b.appOn = true
		b.mu.Unlock()
	}
	if err := b.Advertise(); err != nil {
		return err
	}
	if want {
		if err := b.startDiscovery(ctx, adapter); err != nil {
			return err
		}
		b.mu.Lock()
		b.scanning = true
		b.mu.Unlock()
	}
	return nil
}

func (b *BlueZ) signals(ch chan *dbus.Signal, done chan struct{}) {
	defer close(done)
	for sig := range ch {
		switch sig.Name {
		case "org.freedesktop.DBus.NameOwnerChanged":
			// bluetoothd restarted. Everything we registered went with it.
			if len(sig.Body) == 3 {
				if owner, _ := sig.Body[2].(string); owner != "" {
					b.mu.Lock()
					b.advOn, b.scanning = false, false
					b.health = "bluetoothd restarted; resuming"
					b.mu.Unlock()
					b.recover(true)
				} else {
					b.mu.Lock()
					b.advOn, b.scanning, b.appOn = false, false, false
					b.health = "bluetoothd stopped"
					b.mu.Unlock()
				}
			}
		case ifObjMgr + ".InterfacesAdded":
			if len(sig.Body) < 2 {
				continue
			}
			path, _ := sig.Body[0].(dbus.ObjectPath)
			ifaces, _ := sig.Body[1].(map[string]map[string]dbus.Variant)
			if d, ok := ifaces[ifDevice]; ok && b.under(path) {
				b.device(path, d)
			}
		case ifObjMgr + ".InterfacesRemoved":
			if len(sig.Body) < 2 {
				continue
			}
			path, _ := sig.Body[0].(dbus.ObjectPath)
			ifaces, _ := sig.Body[1].([]string)
			for _, i := range ifaces {
				if i == ifDevice {
					b.forget(path)
				}
			}
		case ifProps + ".PropertiesChanged":
			if len(sig.Body) < 2 {
				continue
			}
			iface, _ := sig.Body[0].(string)
			changed, _ := sig.Body[1].(map[string]dbus.Variant)
			switch iface {
			case ifAdapter:
				b.adapterChanged(sig.Path, changed)
			case ifDevice:
				b.device(sig.Path, changed)
			case ifChar:
				v, ok := changed["Value"]
				if !ok {
					continue
				}
				frame, _ := v.Value().([]byte)
				b.mu.Lock()
				dev, ours := b.txPaths[sig.Path]
				ev := b.ev
				b.mu.Unlock()
				if ours && ev.Notified != nil {
					ev.Notified(string(dev), frame)
				}
			}
		}
	}
}

// adapterChanged watches for power loss (user toggle, suspend, rfkill) and
// recovery. While the adapter is off, Probe says so. When it comes back, the
// advertisement and discovery are registered again. We never power it back on
// ourselves: switching it off was the user's call.
func (b *BlueZ) adapterChanged(path dbus.ObjectPath, changed map[string]dbus.Variant) {
	b.mu.Lock()
	ours := path == b.adapter
	b.mu.Unlock()
	if !ours {
		return
	}
	v, ok := changed["Powered"]
	if !ok {
		return
	}
	if on, _ := v.Value().(bool); on {
		b.recover(false)
		return
	}
	b.mu.Lock()
	b.advOn, b.scanning = false, false
	b.mu.Unlock()
}

// device handles a Device1's properties: the full set (InterfacesAdded) or
// only what changed (PropertiesChanged). A device counts as heard only when
// the update is something that came off the air (RSSI or advertised data).
// A cached entry, or a Connected flag on its own, does not count.
func (b *BlueZ) device(path dbus.ObjectPath, props map[string]dbus.Variant) {
	_, rssi := props["RSSI"]
	_, sd := props["ServiceData"]
	_, md := props["ManufacturerData"]
	heard := rssi || sd || md
	uuOurs, uuHas := false, false
	if v, ok := props["UUIDs"]; ok {
		uuHas = true
		uu, _ := v.Value().([]string)
		for _, u := range uu {
			if strings.EqualFold(u, ServiceUUID) {
				uuOurs = true
			}
		}
	}
	connected, hasConn := false, false
	if v, ok := props["Connected"]; ok {
		connected, hasConn = v.Value().(bool)
	}

	b.mu.Lock()
	if b.conn == nil || b.devices == nil {
		b.mu.Unlock()
		return
	}
	st := b.devices[path]
	if st == nil {
		ours, classified := b.known[path]
		if uuHas {
			ours, classified = uuOurs, true
		}
		if !classified && heard {
			// A device that was cached before this run and then heard
			// again reports only RSSI. Ask for its UUIDs once.
			b.mu.Unlock()
			v, _ := b.devProp(path, "UUIDs")
			uu, _ := v.([]string)
			ours = false
			for _, u := range uu {
				if strings.EqualFold(u, ServiceUUID) {
					ours = true
				}
			}
			b.mu.Lock()
			if b.devices == nil {
				b.mu.Unlock()
				return
			}
			classified = true
		}
		if classified {
			if len(b.known) >= maxKnown {
				b.known = map[dbus.ObjectPath]bool{}
			}
			b.known[path] = ours
		}
		if !ours || !heard {
			b.mu.Unlock()
			return
		}
		if len(b.devices) >= maxDevices && !b.evictDeviceLocked() {
			b.mu.Unlock()
			return
		}
		st = &devState{}
		b.devices[path] = st
		if hasConn {
			st.connected = connected
		}
	}
	dropped := hasConn && !connected && st.connected // a link just went down
	if hasConn {
		st.connected = connected
	}
	if heard {
		st.lastHeard = time.Now()
	}
	c, ev := st.connected, b.ev
	b.mu.Unlock()
	switch {
	case dropped:
		if ev.Lost != nil {
			ev.Lost(string(path))
		}
	case heard && ev.Seen != nil:
		ev.Seen(string(path), c)
	case hasConn && connected && ev.Seen != nil:
		// It connected (likely dialed us). Tell Link, so that it does not
		// dial back over the same link.
		ev.Seen(string(path), true)
	}
}

// evictDeviceLocked drops the longest-unheard unconnected Concord device, so
// that a burst of spoofed advertisements cycles through the table instead of
// filling it.
func (b *BlueZ) evictDeviceLocked() bool {
	var victim dbus.ObjectPath
	var oldest time.Time
	for p, d := range b.devices {
		if d.connected || b.dialed[p] {
			continue
		}
		if victim == "" || d.lastHeard.Before(oldest) {
			victim, oldest = p, d.lastHeard
		}
	}
	if victim == "" {
		return false
	}
	delete(b.devices, victim)
	return true
}

func (b *BlueZ) forget(path dbus.ObjectPath) {
	b.mu.Lock()
	_, ours := b.devices[path]
	delete(b.devices, path)
	delete(b.known, path)
	delete(b.writers, path)
	delete(b.inboundSince, path)
	ev := b.ev
	b.mu.Unlock()
	if ours && ev.Lost != nil {
		ev.Lost(string(path))
	}
}

// --- the objects BlueZ calls back into ---

func (b *BlueZ) export(conn *dbus.Conn) error {
	type exp struct {
		v     any
		path  dbus.ObjectPath
		iface string
	}
	for _, e := range []exp{
		{gattApp{b}, b.appPath(), ifObjMgr},
		{&props{b, b.svcPath()}, b.svcPath(), ifProps},
		{&props{b, b.rxPath()}, b.rxPath(), ifProps},
		{&props{b, b.txPath()}, b.txPath(), ifProps},
		{&props{b, b.advPath()}, b.advPath(), ifProps},
		{rxChar{b}, b.rxPath(), ifChar},
		{txChar{b}, b.txPath(), ifChar},
		{advert{b}, b.advPath(), ifAdvert},
	} {
		if err := conn.Export(e.v, e.path, e.iface); err != nil {
			return fmt.Errorf("ble: export %s: %w", e.path, err)
		}
	}
	return nil
}

// objects is the GATT application and the advertisement, as BlueZ sees them.
func (b *BlueZ) objects() map[dbus.ObjectPath]map[string]map[string]dbus.Variant {
	b.mu.Lock()
	notifying := b.notifying
	b.mu.Unlock()
	v := dbus.MakeVariant
	return map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		b.svcPath(): {ifService: {
			"UUID":    v(ServiceUUID),
			"Primary": v(true),
		}},
		b.rxPath(): {ifChar: {
			"UUID":    v(RxUUID),
			"Service": v(b.svcPath()),
			// No encrypt-* flags. Requiring encryption would make BlueZ
			// pair, and pairing hands over the identity address and the
			// IRK. Frames are already MLS ciphertext.
			"Flags": v([]string{"write", "write-without-response"}),
		}},
		b.txPath(): {ifChar: {
			"UUID":      v(TxUUID),
			"Service":   v(b.svcPath()),
			"Flags":     v([]string{"notify"}),
			"Notifying": v(notifying),
		}},
		b.advPath(): {ifAdvert: {
			"Type": v("peripheral"),
			// Only the service UUID. No service data, no LocalName, no
			// Appearance, no TX power, no manufacturer data: nothing
			// that differs from one Concord device to the next. The
			// Discoverable flag is needed for BlueZ scanners to report us
			// at all. It does not make the adapter discoverable over
			// classic Bluetooth.
			"ServiceUUIDs": v([]string{ServiceUUID}),
			"Discoverable": v(true),
		}},
	}
}

type gattApp struct{ b *BlueZ }

func (a gattApp) GetManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error) {
	objs := a.b.objects()
	delete(objs, a.b.advPath()) // the advertisement is not part of the GATT tree
	return objs, nil
}

type props struct {
	b    *BlueZ
	path dbus.ObjectPath
}

func (p *props) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	return p.b.objects()[p.path][iface], nil
}

func (p *props) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	v, ok := p.b.objects()[p.path][iface][name]
	if !ok {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"no such property"})
	}
	return v, nil
}

func (p *props) Set(string, string, dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", nil)
}

type rxChar struct{ b *BlueZ }

// WriteValue is a central writing one frame to us.
func (c rxChar) WriteValue(value []byte, opts map[string]dbus.Variant) *dbus.Error {
	if v, ok := opts["offset"]; ok {
		if off, _ := v.Value().(uint16); off != 0 {
			// A long write would split a frame across calls. Peers below
			// MinATTMTU are not supported.
			return dbus.NewError("org.bluez.Error.InvalidOffset", nil)
		}
	}
	if len(value) == 0 || len(value) > MaxFrame {
		return dbus.NewError("org.bluez.Error.InvalidValueLength", nil)
	}
	c.b.mu.Lock()
	if v, ok := opts["device"]; ok && c.b.writers != nil {
		if p, _ := v.Value().(dbus.ObjectPath); p != "" {
			if _, seen := c.b.writers[p]; !seen && len(c.b.writers) < maxWriters {
				c.b.writers[p] = time.Now()
			}
		}
	}
	ev := c.b.ev
	c.b.mu.Unlock()
	if ev.Inbound != nil {
		ev.Inbound(value)
	}
	return nil
}

type txChar struct{ b *BlueZ }

func (c txChar) StartNotify() *dbus.Error { c.set(true); return nil }
func (c txChar) StopNotify() *dbus.Error  { c.set(false); return nil }

func (c txChar) set(on bool) {
	c.b.mu.Lock()
	c.b.notifying = on
	ev := c.b.ev
	c.b.mu.Unlock()
	if ev.Subscribed != nil {
		ev.Subscribed(on)
	}
}

type advert struct{ b *BlueZ }

// Release is BlueZ dropping our advertisement (adapter reset, power off).
func (a advert) Release() *dbus.Error {
	a.b.mu.Lock()
	a.b.advOn = false
	a.b.mu.Unlock()
	return nil
}

// --- an outbound connection ---

type bluezConn struct {
	b      *BlueZ
	conn   *dbus.Conn
	dev    dbus.ObjectPath
	rx, tx dbus.ObjectPath
	once   sync.Once
}

func (c *bluezConn) Write(ctx context.Context, frame []byte) error {
	return c.conn.Object(bluezBus, c.rx).CallWithContext(ctx, ifChar+".WriteValue", 0,
		frame, map[string]dbus.Variant{"type": dbus.MakeVariant("command")}).Err
}

// Close unsubscribes, disconnects and, unless the device is paired, removes
// it from BlueZ's cache.
func (c *bluezConn) Close() error {
	c.once.Do(func() {
		c.b.mu.Lock()
		if c.b.txPaths != nil && c.tx != "" {
			delete(c.b.txPaths, c.tx)
		}
		c.b.mu.Unlock()
		if c.tx != "" {
			ctx, cancel := context.WithTimeout(context.Background(), cleanupWait)
			_ = c.conn.Object(bluezBus, c.tx).CallWithContext(ctx, ifChar+".StopNotify", 0).Err
			cancel()
		}
		if c.b.paired(c.dev) {
			return
		}
		c.b.disconnect(c.dev)
		c.b.remove(c.dev)
	})
	return nil
}

// --- helpers ---

func isDBusError(err error, name string) bool {
	var de dbus.Error
	if errors.As(err, &de) {
		return de.Name == name
	}
	var dp *dbus.Error
	return errors.As(err, &dp) && dp.Name == name
}

func managedObjects(conn *dbus.Conn) (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	ctx, cancel := context.WithTimeout(context.Background(), bluezCallWait)
	defer cancel()
	err := conn.Object(bluezBus, "/").CallWithContext(ctx, ifObjMgr+".GetManagedObjects", 0).Store(&objs)
	return objs, err
}

// findAdapter returns the named adapter (or the first) and its properties.
func findAdapter(conn *dbus.Conn, name string) (dbus.ObjectPath, map[string]dbus.Variant, error) {
	objs, err := managedObjects(conn)
	if err != nil {
		if isDBusError(err, "org.freedesktop.DBus.Error.ServiceUnknown") {
			return "", nil, errors.New("bluetoothd is not running")
		}
		return "", nil, fmt.Errorf("BlueZ: %w", err)
	}
	var paths []string
	for p, ifaces := range objs {
		if _, ok := ifaces[ifAdapter]; ok {
			if name == "" || filepath.Base(string(p)) == name {
				paths = append(paths, string(p))
			}
		}
	}
	if len(paths) == 0 {
		return "", nil, errors.New("no Bluetooth adapter")
	}
	sort.Strings(paths)
	p := dbus.ObjectPath(paths[0])
	return p, objs[p][ifAdapter], nil
}

// anyConnected reports whether any device on the adapter is connected.
func anyConnected(conn *dbus.Conn, b *BlueZ) bool {
	objs, err := managedObjects(conn)
	if err != nil {
		return true // unsure, so leave the radio on
	}
	for p, ifaces := range objs {
		if d, ok := ifaces[ifDevice]; ok && b.under(p) {
			if c, _ := d["Connected"].Value().(bool); c {
				return true
			}
		}
	}
	return false
}

func adapterPrivacy(adapter dbus.ObjectPath) (bool, error) {
	idx, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(string(adapter)), "hci"))
	if err != nil {
		return false, fmt.Errorf("adapter %s has no index", adapter)
	}
	return lePrivacy(uint16(idx))
}

// rfkilled reads the adapter's rfkill switches from sysfs.
func rfkilled(name string) (hard, soft bool) {
	dirs, _ := filepath.Glob(filepath.Join("/sys/class/bluetooth", name, "rfkill*"))
	for _, d := range dirs {
		if b, err := os.ReadFile(filepath.Join(d, "hard")); err == nil && strings.TrimSpace(string(b)) == "1" {
			hard = true
		}
		if b, err := os.ReadFile(filepath.Join(d, "soft")); err == nil && strings.TrimSpace(string(b)) == "1" {
			soft = true
		}
	}
	return
}

func waitFor(ctx context.Context, max time.Duration, cond func() bool) error {
	deadline := time.Now().Add(max)
	for !cond() {
		if time.Now().After(deadline) {
			return errors.New("timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}
