package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestDialAddr(t *testing.T) {
	tests := []struct{ listen, want string }{
		// every interface: dial the loopback address of the same family
		{"0.0.0.0:8080", "127.0.0.1:8080"},
		{"[::]:8080", "[::1]:8080"},
		// a specific address is dialed as is
		{"127.0.0.1:8080", "127.0.0.1:8080"},
		{"192.0.2.10:443", "192.0.2.10:443"},
		{"[::1]:8080", "[::1]:8080"},
		{"[2001:db8::1]:8080", "[2001:db8::1]:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.listen, func(t *testing.T) {
			got := dialAddr(netip.MustParseAddrPort(tt.listen))
			if got.String() != tt.want {
				t.Errorf("dialAddr(%s) = %s, want %s", tt.listen, got, tt.want)
			}
		})
	}
}

// serve starts a server answering every request with code and points
// RELOCD_ADDR at it. It returns the requests the server got and its address.
func serve(t *testing.T, code int) (*[]string, netip.AddrPort) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	addr := netip.MustParseAddrPort(srv.Listener.Addr().String())
	t.Setenv("RELOCD_ADDR", addr.String())
	return &paths, addr
}

func TestProbe(t *testing.T) {
	paths, _ := serve(t, http.StatusOK)
	if err := probe(context.Background(), "/readyz"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*paths, ","); got != "GET /readyz" {
		t.Errorf("requests = %q, want one GET /readyz", got)
	}
}

func TestProbeUnspecifiedAddr(t *testing.T) {
	// relocd in a container listens on 0.0.0.0; the probe must still reach it
	_, addr := serve(t, http.StatusOK)
	t.Setenv("RELOCD_ADDR", netip.AddrPortFrom(netip.IPv4Unspecified(), addr.Port()).String())

	if err := probe(context.Background(), "/livez"); err != nil {
		t.Fatal(err)
	}
}

func TestProbeNotOK(t *testing.T) {
	for _, code := range []int{http.StatusServiceUnavailable, http.StatusUnauthorized, http.StatusNotFound, http.StatusNoContent} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			_, _ = serve(t, code)
			err := probe(context.Background(), "/readyz")
			if err == nil || !strings.Contains(err.Error(), "probe /readyz: ") {
				t.Fatalf("err = %v, want a probe /readyz error", err)
			}
		})
	}
}

func TestProbeNothingListens(t *testing.T) {
	// take a free port and release it: nothing listens there afterwards
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RELOCD_ADDR", ln.Addr().String())
	ln.Close()

	err = probe(context.Background(), "/livez")
	if err == nil || !strings.Contains(err.Error(), "probe /livez: ") {
		t.Fatalf("err = %v, want a probe /livez error", err)
	}
}

// A server that accepts but never answers must not hold the healthcheck
// past probeTimeout.
func TestProbeTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { conn.Close() })
		}
	}()
	t.Setenv("RELOCD_ADDR", ln.Addr().String())

	start := time.Now()
	err = probe(context.Background(), "/readyz")
	if err == nil {
		t.Fatal("probe of a silent server succeeded")
	}
	if elapsed := time.Since(start); elapsed > probeTimeout+time.Second {
		t.Errorf("probe took %v, want about probeTimeout (%v)", elapsed, probeTimeout)
	}
}

func TestProbeBadAddr(t *testing.T) {
	t.Setenv("RELOCD_ADDR", "localhost:8080")
	err := probe(context.Background(), "/livez")
	if err == nil || !strings.HasPrefix(err.Error(), "config: RELOCD_ADDR:") {
		t.Fatalf("err = %v, want the config error", err)
	}
}
