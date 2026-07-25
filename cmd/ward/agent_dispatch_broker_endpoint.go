package main

import (
	"context"
	"fmt"
	"net"
	"path"
	"strings"
)

const dispatchBrokerUnixPrefix = "unix://"

// dispatchBrokerEndpoint resolves the Compose broker's stable TCP service
// address plus the legacy Unix-socket compatibility form.
func dispatchBrokerEndpoint(raw string) (network, addr string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("dispatch broker endpoint is empty")
	}
	if strings.HasPrefix(raw, dispatchBrokerUnixPrefix) {
		addr = strings.TrimPrefix(raw, dispatchBrokerUnixPrefix)
		if !path.IsAbs(addr) {
			return "", "", fmt.Errorf("dispatch broker Unix socket %q is not absolute", addr)
		}
		return "unix", addr, nil
	}
	return "tcp", raw, nil
}

func dialDispatchBroker(ctx context.Context, raw string) (net.Conn, error) {
	network, addr, err := dispatchBrokerEndpoint(raw)
	if err != nil {
		return nil, err
	}
	return dispatchBrokerDialContext(ctx, network, addr)
}
