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
	maxSeenAll    = 4096
	maxDevices    = 256 // Concord devices tracked; a spoofer cannot grow it past this
	connectWait   = 20 * time.Second
	resolveWait   = 15 * time.Second
	bluezCallWait = 5 * time.Second
)

// ErrNoPrivacy is why Start refuses an adapter without LE privacy.
var ErrNoPrivacy = errors.New("ble: LE privacy is off on this adapter, so it would advertise its fixed public address; " +
	"set `Privacy = device` under [General] in /etc/bluetooth/main.conf and restart bluetooth")

// BlueZConfig chooses and constrains the adapter.
type BlueZConfig struct {
	// Adapter is the adapter name, e.g. "hci0". Empty picks the first.
	Adapter string
	// AllowPublicAddress lets the link run on an adapter without LE
	// privacy. That adapter advertises and connects from its fixed public
	// address, which anyone nearby can log and follow. Concord itself never
	// sets this; it exists so concord-blecheck can exercise the radio on a
	// test machine where privacy cannot be turned on.
	AllowPublicAddress bool
}

// BlueZ is the Linux Radio: BlueZ's GATT server, advertising and discovery
// APIs over the system D-Bus.
//
// Everything it registers lives on its own private bus connection, so if the
// process dies BlueZ drops the GATT service, the advertisement and the
// discovery session by itself.
type BlueZ struct {
	cfg  BlueZConfig
	base dbus.ObjectPath

	mu          sync.Mutex
	conn        *dbus.Conn
	adapter     dbus.ObjectPath
	ev          Events
	advData     []byte
	advOn       bool
	appOn       bool
	scanning    bool
	notifying   bool
	poweredByUs bool
	savedAlias  string
	aliasSet    bool
	systemName  string
	devices     map[dbus.ObjectPath]*devState       // Concord devices
	txPaths     map[dbus.ObjectPath]dbus.ObjectPath // remote notify char → device
	seenAll     map[dbus.ObjectPath]struct{}        // every LE device seen (diagnostics)
	sigCh       chan *dbus.Signal
	sigDone     chan struct{}
}

type devState struct {
	ad        []byte
	connected bool
}

var bluezInstances atomic.Int64

// NewBlueZ returns a radio for cfg. It touches nothing until Probe or Up.
func NewBlueZ(cfg BlueZConfig) *BlueZ {
	return &BlueZ{cfg: cfg, base: dbus.ObjectPath(fmt.Sprintf("/org/concord/ble%d", bluezInstances.Add(1)))}
}

// Default is the link Concord registers: the first adapter, privacy required.
func Default() *Link { return New(NewBlueZ(BlueZConfig{})) }

func (b *BlueZ) appPath() dbus.ObjectPath { return b.base }
func (b *BlueZ) svcPath() dbus.ObjectPath { return b.base + "/service0" }
func (b *BlueZ) rxPath() dbus.ObjectPath  { return b.base + "/service0/rx" }
func (b *BlueZ) txPath() dbus.ObjectPath  { return b.base + "/service0/tx" }
func (b *BlueZ) advPath() dbus.ObjectPath { return b.base + "/advertisement0" }

// Probe looks for an adapter on D-Bus and in sysfs and reads its privacy
// setting. It sends nothing over the air and changes nothing.
func (b *BlueZ) Probe() transport.Status {
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
		detail += " (off; enabling this link turns it on)"
	}
	return transport.Status{State: transport.Available, Detail: detail}
}

// Up powers the adapter if needed, hides its name, serves the GATT service
// and starts listening for BlueZ's signals.
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
	b.txPaths = map[dbus.ObjectPath]dbus.ObjectPath{}
	b.seenAll = map[dbus.ObjectPath]struct{}{}
	b.sigCh, b.sigDone = nil, nil
	b.notifying, b.advOn, b.appOn, b.scanning, b.poweredByUs, b.aliasSet = false, false, false, false, false, false
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

	// The GAP Device Name characteristic is readable by anyone who
	// connects, and on Linux it is the adapter alias — usually the
	// hostname. Swap it for a generic one while the link runs.
	if alias, _ := props["Alias"].Value().(string); alias != genericAlias {
		if err := ad.SetProperty(ifAdapter+".Alias", dbus.MakeVariant(genericAlias)); err != nil {
			return fmt.Errorf("ble: set generic alias: %w", err)
		}
		sysName, _ := props["Name"].Value().(string)
		b.mu.Lock()
		b.savedAlias, b.systemName, b.aliasSet = alias, sysName, true
		b.mu.Unlock()
	}

	if err := b.export(conn); err != nil {
		return err
	}

	b.mu.Lock()
	b.sigCh = make(chan *dbus.Signal, 256)
	b.sigDone = make(chan struct{})
	sigCh, sigDone := b.sigCh, b.sigDone
	b.mu.Unlock()
	for _, opts := range [][]dbus.MatchOption{
		{dbus.WithMatchSender(bluezBus), dbus.WithMatchInterface(ifObjMgr), dbus.WithMatchMember("InterfacesAdded")},
		{dbus.WithMatchSender(bluezBus), dbus.WithMatchInterface(ifObjMgr), dbus.WithMatchMember("InterfacesRemoved")},
		{dbus.WithMatchSender(bluezBus), dbus.WithMatchInterface(ifProps), dbus.WithMatchMember("PropertiesChanged"),
			dbus.WithMatchPathNamespace(adapter)},
	} {
		if err := conn.AddMatchSignal(opts...); err != nil {
			close(sigDone)
			return fmt.Errorf("ble: subscribe to BlueZ signals: %w", err)
		}
	}
	conn.Signal(sigCh)
	go b.signals(sigCh, sigDone)

	if err := b.call(ctx, adapter, ifGattMgr+".RegisterApplication", b.appPath(), map[string]dbus.Variant{}); err != nil {
		return fmt.Errorf("ble: register GATT service: %w", err)
	}
	b.mu.Lock()
	b.appOn = true
	b.mu.Unlock()

	// Devices BlueZ already knows (from an earlier scan) emit no
	// InterfacesAdded; seed them.
	if objs, err := managedObjects(conn); err == nil {
		for path, ifaces := range objs {
			if d, ok := ifaces[ifDevice]; ok && b.under(path) {
				b.device(path, d)
			}
		}
	}
	return nil
}

// Advertise (re)registers the advertisement with data as its service data.
// Re-registering is how the nonce rotates.
func (b *BlueZ) Advertise(data []byte) error {
	b.mu.Lock()
	conn, adapter, wasOn := b.conn, b.adapter, b.advOn
	b.advData = append([]byte(nil), data...)
	b.mu.Unlock()
	if conn == nil {
		return ErrNotRunning
	}
	ctx := context.Background()
	if wasOn {
		_ = b.call(ctx, adapter, ifAdvMgr+".UnregisterAdvertisement", b.advPath())
	}
	err := b.call(ctx, adapter, ifAdvMgr+".RegisterAdvertisement", b.advPath(), map[string]dbus.Variant{})
	b.mu.Lock()
	b.advOn = err == nil
	b.mu.Unlock()
	if err != nil {
		return fmt.Errorf("ble: register advertisement: %w", err)
	}
	return nil
}

// Scan starts or stops LE discovery. No UUID filter is set: BlueZ matches
// discovery filters against advertised UUID lists, and our advertisement
// carries the service UUID only as the key of its service data (both would
// not fit in 31 bytes), so matching happens in device().
func (b *BlueZ) Scan(on bool) error {
	b.mu.Lock()
	conn, adapter, scanning := b.conn, b.adapter, b.scanning
	b.mu.Unlock()
	if conn == nil {
		return ErrNotRunning
	}
	ctx := context.Background()
	if on && !scanning {
		filter := map[string]dbus.Variant{
			"Transport": dbus.MakeVariant("le"),
			// Report service data every time it is heard, so a rotated
			// nonce and a device still in range are both noticed.
			"DuplicateData": dbus.MakeVariant(true),
		}
		if err := b.call(ctx, adapter, ifAdapter+".SetDiscoveryFilter", filter); err != nil {
			return fmt.Errorf("ble: discovery filter: %w", err)
		}
		if err := b.call(ctx, adapter, ifAdapter+".StartDiscovery"); err != nil {
			return fmt.Errorf("ble: start discovery: %w", err)
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

// Notify sends frame to every subscribed central. BlueZ turns a change of
// the characteristic's Value into one notification per subscriber.
func (b *BlueZ) Notify(frame []byte) error {
	b.mu.Lock()
	conn, on := b.conn, b.notifying
	b.mu.Unlock()
	if conn == nil {
		return ErrNotRunning
	}
	if !on {
		return nil
	}
	return conn.Emit(b.txPath(), ifProps+".PropertiesChanged", ifChar,
		map[string]dbus.Variant{"Value": dbus.MakeVariant(frame)}, []string{})
}

// Connect dials a Concord device, waits for its services, checks the MTU
// fits a whole frame and subscribes to its notifications. It never pairs:
// the characteristics require no encryption, so no keys — and no identity
// address — are ever exchanged.
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
	dev := conn.Object(bluezBus, path)
	cctx, cancel := context.WithTimeout(ctx, connectWait)
	err := dev.CallWithContext(cctx, ifDevice+".Connect", 0).Err
	cancel()
	if err != nil {
		return nil, fmt.Errorf("ble: connect: %w", err)
	}
	c := &bluezConn{b: b, conn: conn, dev: path}
	fail := func(err error) (PeerConn, error) {
		_ = c.Close()
		return nil, err
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
	// MTU is exposed since BlueZ 5.62; if absent, a too-long write fails
	// loudly rather than truncating, and the peer is dropped.
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

// Down stops discovery, withdraws the advertisement and the GATT service,
// restores the alias and power state, and closes the bus connection (which by
// itself makes BlueZ drop anything of ours that is left). Safe to call twice
// and after a failed Up.
func (b *BlueZ) Down() error {
	b.mu.Lock()
	conn := b.conn
	if conn == nil {
		b.mu.Unlock()
		return nil
	}
	adapter := b.adapter
	scanning, advOn, appOn := b.scanning, b.advOn, b.appOn
	aliasSet, savedAlias, systemName := b.aliasSet, b.savedAlias, b.systemName
	poweredByUs := b.poweredByUs
	sigDone := b.sigDone
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
	ad := conn.Object(bluezBus, adapter)
	if aliasSet {
		// An alias equal to the system name was never really set; "" puts
		// the adapter back to following the system name.
		restore := savedAlias
		if restore == systemName {
			restore = ""
		}
		if err := ad.SetProperty(ifAdapter+".Alias", dbus.MakeVariant(restore)); err != nil {
			errs = append(errs, fmt.Errorf("restore alias: %w", err))
		} else {
			// BlueZ applies a reset alias asynchronously; wait so a
			// caller that reads it back after Stop sees the old name.
			_ = waitFor(ctx, 2*time.Second, func() bool {
				v, err := ad.GetProperty(ifAdapter + ".Alias")
				a, _ := v.Value().(string)
				return err == nil && a == savedAlias
			})
		}
	}
	// Power the adapter back off only if we turned it on and nothing else
	// (headphones, a keyboard) has connected since.
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
	b.devices, b.txPaths = nil, nil
	b.scanning, b.advOn, b.appOn, b.notifying, b.aliasSet, b.poweredByUs = false, false, false, false, false, false
	b.mu.Unlock()
	return errors.Join(errs...)
}

// Counts is a diagnostic: LE devices heard since Up, and how many of them
// advertise the Concord service.
func (b *BlueZ) Counts() (all, concord int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.seenAll), len(b.devices)
}

// AdvertisementData is what is currently registered, for diagnostics.
func (b *BlueZ) AdvertisementData() (data []byte, on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.advData...), b.advOn
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

func (b *BlueZ) signals(ch chan *dbus.Signal, done chan struct{}) {
	defer close(done)
	for sig := range ch {
		switch sig.Name {
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

// device handles a Device1's properties, full (InterfacesAdded) or changed
// only (PropertiesChanged).
func (b *BlueZ) device(path dbus.ObjectPath, props map[string]dbus.Variant) {
	var sd []byte
	sdHas, sdOurs := false, false
	if v, ok := props["ServiceData"]; ok {
		sdHas = true
		m, _ := v.Value().(map[string]dbus.Variant)
		for k, d := range m {
			if strings.EqualFold(k, ServiceUUID) {
				sd, _ = d.Value().([]byte)
				sdOurs = true
			}
		}
	}
	uuOurs := false
	if v, ok := props["UUIDs"]; ok {
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
	if len(b.seenAll) >= maxSeenAll {
		b.seenAll = map[dbus.ObjectPath]struct{}{}
	}
	b.seenAll[path] = struct{}{}
	st := b.devices[path]
	if st == nil {
		if (!sdOurs && !uuOurs) || len(b.devices) >= maxDevices {
			b.mu.Unlock()
			return
		}
		st = &devState{}
		b.devices[path] = st
	}
	if sdHas {
		st.ad = sd
	}
	dropped := hasConn && !connected && st.connected // a link just went down
	if hasConn {
		st.connected = connected
	}
	ad, c, ev := st.ad, st.connected, b.ev
	b.mu.Unlock()
	if dropped {
		if ev.Lost != nil {
			ev.Lost(string(path))
		}
		return
	}
	if ev.Seen != nil {
		ev.Seen(string(path), ad, c)
	}
}

func (b *BlueZ) forget(path dbus.ObjectPath) {
	b.mu.Lock()
	_, ours := b.devices[path]
	delete(b.devices, path)
	delete(b.seenAll, path)
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

// objects is the GATT application and advertisement as BlueZ sees them.
func (b *BlueZ) objects() map[dbus.ObjectPath]map[string]map[string]dbus.Variant {
	b.mu.Lock()
	adv := append([]byte(nil), b.advData...)
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
			// No encrypt-* flags: requiring encryption would make
			// BlueZ pair, and pairing hands over the identity
			// address and IRK. Frames are already MLS ciphertext.
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
			// Only the service data — no UUID list, no LocalName, no
			// Appearance, no TX power, no manufacturer data. The
			// Discoverable flag is needed for BlueZ scanners to
			// report us at all; it does not make the adapter
			// discoverable over classic Bluetooth.
			"ServiceData":  v(map[string]dbus.Variant{ServiceUUID: v(adv)}),
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
			// Long writes would split a frame across calls; peers
			// below MinATTMTU are not supported.
			return dbus.NewError("org.bluez.Error.InvalidOffset", nil)
		}
	}
	if len(value) == 0 || len(value) > MaxFrame {
		return dbus.NewError("org.bluez.Error.InvalidValueLength", nil)
	}
	c.b.mu.Lock()
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

func (c *bluezConn) Close() error {
	var err error
	c.once.Do(func() {
		c.b.mu.Lock()
		if c.b.txPaths != nil && c.tx != "" {
			delete(c.b.txPaths, c.tx)
		}
		c.b.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), bluezCallWait)
		defer cancel()
		if c.tx != "" {
			_ = c.conn.Object(bluezBus, c.tx).CallWithContext(ctx, ifChar+".StopNotify", 0).Err
		}
		err = c.conn.Object(bluezBus, c.dev).CallWithContext(ctx, ifDevice+".Disconnect", 0).Err
	})
	return err
}

// --- helpers ---

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
		var de dbus.Error
		if errors.As(err, &de) && de.Name == "org.freedesktop.DBus.Error.ServiceUnknown" {
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
		return true // unsure: leave the radio on
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
