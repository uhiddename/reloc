package main

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"time"

	"github.com/uhiddename/reloc/internal/config"
)

// probeTimeout bounds one probe request: above API's readyTimeout, so a slow
// database comes back as 503 rather than a client timeout, and below the
// healthcheck timeout in compose.yaml.
const probeTimeout = 2 * time.Second

// probe asks the relocd listening on RELOCD_ADDR for path and fails unless
// it answers 200. It is for container healthchecks: the image has no curl.
func probe(ctx context.Context, path string) error {
	addr, err := config.LoadRelocdAddr()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	url := "http://" + dialAddr(addr).String() + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("probe %s: %w", path, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("probe %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probe %s: %s", path, resp.Status)
	}
	return nil
}

// dialAddr turns a listen address into one to connect to. The unspecified
// address means "every interface" and can't be dialed, so it becomes the
// loopback address of the same family.
func dialAddr(ap netip.AddrPort) netip.AddrPort {
	if !ap.Addr().IsUnspecified() {
		return ap
	}
	lo := netip.IPv6Loopback()
	if ap.Addr().Is4() {
		lo = netip.AddrFrom4([4]byte{127, 0, 0, 1})
	}
	return netip.AddrPortFrom(lo, ap.Port())
}
