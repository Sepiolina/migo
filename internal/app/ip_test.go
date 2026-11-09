package app

import (
	"net"
	"testing"
)

func ips(ss ...string) []net.IP {
	var out []net.IP
	for _, s := range ss {
		out = append(out, net.ParseIP(s))
	}
	return out
}

func TestPickIP(t *testing.T) {
	for _, c := range []struct {
		name  string
		route net.IP
		addrs []net.IP
		want  string
	}{
		// The reported case: a disconnected Windows adapter listed first.
		{"skips link-local", nil, ips("169.254.91.203", "192.168.1.20"), "192.168.1.20"},
		{"route wins", net.ParseIP("10.0.0.5"), ips("192.168.56.1", "10.0.0.5"), "10.0.0.5"},
		{"link-local route ignored", net.ParseIP("169.254.1.1"), ips("169.254.1.1", "172.16.4.2"), "172.16.4.2"},
		{"private before public", nil, ips("203.0.113.7", "10.1.2.3"), "10.1.2.3"},
		{"public when nothing private", nil, ips("169.254.9.9", "203.0.113.7"), "203.0.113.7"},
		{"nothing usable", nil, ips("169.254.9.9", "127.0.0.1"), "127.0.0.1"},
	} {
		if got := pickIP(c.route, c.addrs); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestLocalIPIsNeverLinkLocal(t *testing.T) {
	if ip := net.ParseIP(localIP()); ip == nil || ip.IsLinkLocalUnicast() {
		t.Fatalf("localIP() = %v", ip)
	}
	for _, s := range otherIPs(localIP()) {
		if net.ParseIP(s).IsLinkLocalUnicast() {
			t.Fatalf("otherIPs lists link-local %s", s)
		}
	}
}
