package network

import (
	"runtime"
	"testing"
)

const missingIface = "newttest-none0"

// hostChangeCalls would each change the host's interfaces or routes. Every
// one names an interface that does not exist or an address that does not
// parse, so a call that reaches the OS returns an error - and cannot change
// anything. goos is the platform whose code the call reaches, or "" for the
// cross-platform entry points.
var hostChangeCalls = []struct {
	name string
	goos string
	call func() error
}{
	{"ConfigureInterface", "", func() error { return ConfigureInterface(missingIface, "100.64.0.2/24", 1280) }},
	{"AddSecondaryAddress", "", func() error { return AddSecondaryAddress(missingIface, "100.64.0.3/32") }},
	{"RemoveSecondaryAddress", "", func() error { return RemoveSecondaryAddress(missingIface, "100.64.0.3/32") }},
	{"AddBypassRouteForDestination", "", func() error { return AddBypassRouteForDestination("not-an-ip", missingIface) }},
	{"RemoveBypassRouteForDestination", "", func() error { return RemoveBypassRouteForDestination("not-an-ip") }},
	{"LinuxAddRoute", "linux", func() error { return LinuxAddRoute("192.0.2.0/24", "", missingIface) }},
	{"LinuxRemoveRoute", "linux", func() error { return LinuxRemoveRoute("192.0.2.0/24", missingIface) }},
	{"LinuxAddBypassRoute", "linux", func() error { return LinuxAddBypassRoute("not-an-ip", missingIface) }},
	{"LinuxRemoveBypassRoute", "linux", func() error { return LinuxRemoveBypassRoute("not-an-ip") }},
	{"DarwinAddRouteWithSource", "darwin", func() error { return DarwinAddRouteWithSource("192.0.2.0/24", "", missingIface, "") }},
	{"DarwinRemoveRoute", "darwin", func() error { return DarwinRemoveRoute("192.0.2.0/24") }},
	{"DarwinAddBypassRoute", "darwin", func() error { return DarwinAddBypassRoute("not-an-ip", missingIface) }},
	{"DarwinRemoveBypassRoute", "darwin", func() error { return DarwinRemoveBypassRoute("not-an-ip") }},
	{"WindowsAddRoute", "windows", func() error { return WindowsAddRoute("192.0.2.0/24", "", missingIface) }},
	{"WindowsRemoveRoute", "windows", func() error { return WindowsRemoveRoute("192.0.2.0/24", missingIface) }},
	{"WindowsAddBypassRoute", "windows", func() error { return WindowsAddBypassRoute("not-an-ip", missingIface) }},
	{"WindowsRemoveBypassRoute", "windows", func() error { return WindowsRemoveBypassRoute("not-an-ip") }},
}

// withNativeConfigDisabled sets NativeConfigDisabled for one test, starting
// from NetworkSettings that already hold a primary tunnel address.
func withNativeConfigDisabled(t *testing.T, disabled bool) {
	t.Helper()
	ClearNetworkSettings()
	SetIPv4Settings([]string{"100.64.0.2"}, []string{"255.255.255.0"})
	NativeConfigDisabled = disabled
	t.Cleanup(func() {
		NativeConfigDisabled = false
		ClearNetworkSettings()
	})
}

func TestNativeConfigDisabledSkipsHostChanges(t *testing.T) {
	withNativeConfigDisabled(t, true)

	for _, c := range hostChangeCalls {
		if err := c.call(); err != nil {
			t.Errorf("%s: %v, want nil without reaching the OS", c.name, err)
		}
	}
	if ManagesHostRoutes() {
		t.Error("ManagesHostRoutes() = true, want false")
	}
	if changed, err := ReconcileBypassRoute("not-an-ip", missingIface); changed || err != nil {
		t.Errorf("ReconcileBypassRoute = %v, %v; want false, nil", changed, err)
	}

	// NetworkSettings is still kept for a host app that applies it itself.
	if got := GetSettings().IPv4ExcludedRoutes; len(got) != 0 {
		t.Errorf("IPv4ExcludedRoutes = %v, want the bypass route added and removed again", got)
	}
	if got := GetSettings().IPv4Addresses; len(got) != 1 || got[0] != "100.64.0.2" {
		t.Errorf("IPv4Addresses = %v, want [100.64.0.2]", got)
	}
}

// TestHostChangeCallsReachTheOSByDefault shows that the calls above fail when
// they do reach the OS, so TestNativeConfigDisabledSkipsHostChanges would
// notice a missing check. Linux only: there every one of these failures is an
// interface lookup or a parse error, so nothing on the host can change.
func TestHostChangeCallsReachTheOSByDefault(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only checked on linux")
	}
	withNativeConfigDisabled(t, false)

	for _, c := range hostChangeCalls {
		if c.goos != "" && c.goos != runtime.GOOS {
			continue
		}
		if err := c.call(); err == nil {
			t.Errorf("%s returned nil with NativeConfigDisabled unset, want an error from the OS", c.name)
		}
	}
	if !ManagesHostRoutes() {
		t.Error("ManagesHostRoutes() = false, want true")
	}
}
