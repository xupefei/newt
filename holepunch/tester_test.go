package holepunch

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/fosrl/newt/bind"
)

func TestCheckRouteLoopback(t *testing.T) {
	if err := checkRoute(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}); err != nil {
		t.Fatalf("checkRoute(127.0.0.1:9) = %v, want nil", err)
	}
}

// TestUnroutableEndpointsNetns checks that TestEndpoint and
// TestConnectionWithBind report ErrNoRoute for endpoints the host cannot
// route to, before sending anything through the shared socket. It needs a
// network namespace with only loopback, so that no address but 127.0.0.0/8
// is routable:
//
//	NETNS_TEST=1 unshare -rn go test ./holepunch -run Netns -v
func TestUnroutableEndpointsNetns(t *testing.T) {
	if os.Getenv("NETNS_TEST") == "" {
		t.Skip()
	}
	if out, err := exec.Command("ip", "link", "set", "lo", "up").CombinedOutput(); err != nil {
		t.Fatalf("ip link set lo up: %v %s", err, out)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	sb, err := bind.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close()
	tester := NewHolepunchTester(sb, nil)
	if err := tester.Start(); err != nil {
		t.Fatal(err)
	}
	defer tester.Stop()

	for _, ep := range []string{"[2001:db8::1]:51820", "192.0.2.1:51820"} {
		start := time.Now()
		res := tester.TestEndpoint(ep, 2*time.Second)
		if !errors.Is(res.Error, ErrNoRoute) {
			t.Errorf("TestEndpoint(%s) error = %v, want ErrNoRoute", ep, res.Error)
		}
		if d := time.Since(start); d > time.Second {
			t.Errorf("TestEndpoint(%s) took %v, want an immediate error", ep, d)
		}
	}
	if res := TestConnectionWithBind(sb, "[2001:db8::1]:51820", nil); !errors.Is(res.Error, ErrNoRoute) {
		t.Errorf("TestConnectionWithBind error = %v, want ErrNoRoute", res.Error)
	}

	// A routable endpoint is still tested. Nothing answers on 127.0.0.1:9,
	// so this times out, after sending.
	if res := tester.TestEndpoint("127.0.0.1:9", 300*time.Millisecond); errors.Is(res.Error, ErrNoRoute) || res.Error == nil {
		t.Errorf("TestEndpoint(127.0.0.1:9) error = %v, want a timeout", res.Error)
	}
}
