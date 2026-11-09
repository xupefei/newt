//go:build linux

package network

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestNativeConfigDisabledNetns checks against a real kernel that, with
// NativeConfigDisabled set, configuring a tunnel interface with its routes, a
// bypass route and a gateway route - and removing them again - changes no
// address or route, and that WatchRouteChanges does not watch. It then clears
// the flag to show the same calls do change the namespace. It modifies the
// routing table, so it only runs inside a throwaway network namespace:
//
//	NETNS_TEST=1 unshare -rn go test ./network -run TestNativeConfigDisabledNetns -v
func TestNativeConfigDisabledNetns(t *testing.T) {
	if os.Getenv("NETNS_TEST") == "" {
		t.Skip()
	}
	// Own link names and replace rather than add, so this can share a
	// namespace with TestBypassReconcileNetns.
	for _, link := range []string{"ncphys0", "nctun0", "nctun1"} {
		ipCmd(t, "link", "add", link, "type", "dummy")
		ipCmd(t, "link", "set", link, "up")
		t.Cleanup(func() { _ = exec.Command("ip", "link", "del", link).Run() })
	}
	ipCmd(t, "addr", "add", "10.77.0.2/24", "dev", "ncphys0")
	ipCmd(t, "route", "replace", "default", "via", "10.77.0.1", "dev", "ncphys0")
	hostState := func() string {
		return ipCmd(t, "addr") + "\n" + ipCmd(t, "route", "show", "table", "all")
	}
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	ClearNetworkSettings()
	NativeConfigDisabled = true
	t.Cleanup(func() {
		NativeConfigDisabled = false
		ClearNetworkSettings()
	})

	var watched atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	must("WatchRouteChanges", WatchRouteChanges(ctx, func() { watched.Add(1) }))

	before := hostState()
	must("ConfigureInterface", ConfigureInterface("nctun0", "100.64.0.2/24", 1280))
	must("AddSecondaryAddress", AddSecondaryAddress("nctun0", "100.64.0.3/32"))
	must("AddRoutesWithSource", AddRoutesWithSource([]string{"192.0.2.0/24"}, "nctun0", "100.64.0.2"))
	must("AddBypassRouteForDestination", AddBypassRouteForDestination("198.51.100.1", "nctun0"))
	must("AddGatewayDefaultRoute", AddGatewayDefaultRoute("nctun0", "100.64.0.2"))
	if changed, err := ReconcileBypassRoute("198.51.100.1", "nctun0"); changed || err != nil {
		t.Fatalf("ReconcileBypassRoute = %v, %v; want false, nil", changed, err)
	}
	if after := hostState(); after != before {
		t.Fatalf("adding changed the namespace with NativeConfigDisabled set:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got := GetSettings().IPv4Addresses; len(got) != 2 {
		t.Errorf("IPv4Addresses = %v, want the tunnel's two addresses kept in NetworkSettings", got)
	}

	// Put in place what the calls above would have added, and check that the
	// matching removals leave it alone. Adding it is also a route change that
	// a running watcher would report.
	ipCmd(t, "addr", "add", "100.64.0.3/32", "dev", "nctun0")
	ipCmd(t, "route", "add", "192.0.2.0/24", "dev", "nctun0")
	ipCmd(t, "route", "add", "198.51.100.1/32", "via", "10.77.0.1", "dev", "ncphys0")
	ipCmd(t, "route", "replace", "0.0.0.0/1", "dev", "nctun0")
	ipCmd(t, "route", "replace", "128.0.0.0/1", "dev", "nctun0")
	before = hostState()
	must("RemoveGatewayDefaultRoute", RemoveGatewayDefaultRoute("nctun0"))
	must("RemoveBypassRouteForDestination", RemoveBypassRouteForDestination("198.51.100.1"))
	must("RemoveRoutes", RemoveRoutes([]string{"192.0.2.0/24"}, "nctun0"))
	must("RemoveSecondaryAddress", RemoveSecondaryAddress("nctun0", "100.64.0.3/32"))
	if after := hostState(); after != before {
		t.Fatalf("removing changed the namespace with NativeConfigDisabled set:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	time.Sleep(routeChangeSettle + time.Second)
	if n := watched.Load(); n != 0 {
		t.Fatalf("WatchRouteChanges reported %d changes with NativeConfigDisabled set, want 0", n)
	}

	// Without the flag the same calls do change the namespace.
	NativeConfigDisabled = false
	must("ConfigureInterface", ConfigureInterface("nctun1", "100.65.0.2/24", 1280))
	must("AddRoutesWithSource", AddRoutesWithSource([]string{"203.0.113.0/24"}, "nctun1", "100.65.0.2"))
	if got := ipCmd(t, "addr", "show", "dev", "nctun1"); !strings.Contains(got, "100.65.0.2/24") {
		t.Errorf("nctun1 addresses without NativeConfigDisabled:\n%s\nwant 100.65.0.2/24", got)
	}
	if got := ipCmd(t, "route", "show", "203.0.113.0/24"); !strings.Contains(got, "dev nctun1") {
		t.Errorf("route to 203.0.113.0/24 without NativeConfigDisabled: %q, want one on nctun1", got)
	}
}
