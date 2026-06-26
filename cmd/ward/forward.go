package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// forward.go wires the hidden `ward container forward` leaf: a no-capability SOCKS5
// loopback exposing the tower at 127.0.0.1:11434, no --proxy (ward#359).

const (
	// forwardListenAddr is the loopback endpoint the forwarder listens on inside the
	// carry; it shadows the tower's Ollama port so localhost:11434 IS the tower.
	forwardListenAddr = "127.0.0.1:" + towerOllamaPort

	// towerOllamaLocalURL is the no-proxy endpoint a --ts-sidecar carry dials once the
	// forwarder is up; exported as WARD_TOWER_OLLAMA_LOCAL alongside the --proxy vars.
	towerOllamaLocalURL = "http://localhost:" + towerOllamaPort

	// forwardDialTimeout bounds a single SOCKS5 proxy dial + CONNECT handshake.
	forwardDialTimeout = 10 * time.Second
)

// containerForwardCommand is the Hidden `ward container forward` leaf the
// entrypoint backgrounds in a --ts-sidecar carry; not a hand-run verb (ward#359).
func containerForwardCommand() *cli.Command {
	return &cli.Command{
		Name:   "forward",
		Hidden: true, // entrypoint-internal, not a hand-run verb
		Usage:  "Entrypoint-internal SOCKS5 loopback forwarder: expose the tailnet Ollama tower at 127.0.0.1:11434 inside a --ts-sidecar carry so tools dial it with no --proxy.",
		Description: `forward is the no-capability slice of the full-tunnel epic (ward#359). In a
--ts-sidecar carry it listens on 127.0.0.1:11434 and bridges each TCP connection
to ` + towerMagicDNSName + `:` + towerOllamaPort + ` through the standing mac-proxy
SOCKS5 box named by $` + "WARD_TS_SOCKS5" + ` (socks5h: the proxy resolves the tower's
MagicDNS name tailnet-side). LLM clients then auto-route at localhost:11434 with no
proxy awareness. The explicit --proxy "$WARD_TS_SOCKS5" path stays valid. It needs
no NET_ADMIN, no /dev/net/tun, no ALL_PROXY. See docs/agent-ts-sidecar.md.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "listen",
				Usage: "loopback address to listen on",
				Value: forwardListenAddr,
			},
			&cli.StringFlag{
				Name:    "socks5",
				Usage:   "SOCKS5 proxy to bridge through (default $WARD_TS_SOCKS5, e.g. socks5h://mac-proxy:1055)",
				Sources: cli.EnvVars("WARD_TS_SOCKS5"),
			},
			&cli.StringFlag{
				Name:  "target",
				Usage: "host:port to CONNECT to through the proxy (resolved proxy-side)",
				Value: towerMagicDNSName + ":" + towerOllamaPort,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runContainerForward(ctx, c)
		},
	}
}

// runContainerForward resolves the proxy + target, opens the loopback listener,
// and serves connections until the context is cancelled (the carry tears down).
func runContainerForward(ctx context.Context, c *cli.Command) error {
	socks5 := strings.TrimSpace(c.String("socks5"))
	if socks5 == "" {
		return fmt.Errorf("ward container forward: no SOCKS5 proxy set (pass --socks5 or $WARD_TS_SOCKS5); the forwarder only runs in a --ts-sidecar carry (ward#359)")
	}
	proxyAddr, err := parseSocks5ProxyAddr(socks5)
	if err != nil {
		return fmt.Errorf("ward container forward: %w", err)
	}
	target := strings.TrimSpace(c.String("target"))
	if _, _, err := net.SplitHostPort(target); err != nil {
		return fmt.Errorf("ward container forward: --target %q is not host:port: %w", target, err)
	}

	ln, err := net.Listen("tcp", c.String("listen"))
	if err != nil {
		return fmt.Errorf("ward container forward: listen on %s: %w", c.String("listen"), err)
	}
	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "ward-forward: "+format+"\n", args...)
	}
	logf("up: %s -> %s via %s (no --proxy needed; ward#359)", ln.Addr(), target, proxyAddr)
	return serveForward(ctx, ln, proxyAddr, target, logf)
}

// parseSocks5ProxyAddr strips the socks5h://|socks5:// scheme (both accepted, the
// forwarder always resolves proxy-side) and returns the proxy's host:port.
func parseSocks5ProxyAddr(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	for _, scheme := range []string{"socks5h://", "socks5://"} {
		if rest, ok := strings.CutPrefix(s, scheme); ok {
			s = rest
			break
		}
	}
	// Trim any trailing path the URL form may carry (socks5h://host:port/...).
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "", fmt.Errorf("empty SOCKS5 proxy address in %q", raw)
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		return "", fmt.Errorf("SOCKS5 proxy %q is not host:port: %w", raw, err)
	}
	return s, nil
}

// serveForward accepts loopback connections and bridges each one to target
// through the SOCKS5 proxy until ctx is cancelled. It owns and closes ln.
func serveForward(ctx context.Context, ln net.Listener, proxyAddr, target string, logf func(string, ...any)) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // cancelled: a clean teardown, not a failure
			}
			return fmt.Errorf("ward container forward: accept: %w", err)
		}
		go handleForwardConn(conn, proxyAddr, target, logf)
	}
}

// handleForwardConn bridges one accepted loopback connection to target through
// the SOCKS5 proxy, copying both directions until either side closes.
func handleForwardConn(client net.Conn, proxyAddr, target string, logf func(string, ...any)) {
	defer client.Close()
	upstream, err := dialThroughSOCKS5(proxyAddr, target, forwardDialTimeout)
	if err != nil {
		logf("dial %s via %s: %v", target, proxyAddr, err)
		return
	}
	defer upstream.Close()
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		// Half-close the write side so the peer sees EOF and can drain + close.
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}
	go pipe(upstream, client)
	go pipe(client, upstream)
	<-done
	<-done
}

// dialThroughSOCKS5 dials proxyAddr and runs a SOCKS5 (RFC 1928) no-auth CONNECT to
// target as a domain name (socks5h: proxy-side resolve), returning the live tunnel.
func dialThroughSOCKS5(proxyAddr, target string, timeout time.Duration) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("target %q is not host:port: %w", target, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("target %q has a bad port %q", target, portStr)
	}
	if len(host) == 0 || len(host) > 255 {
		return nil, fmt.Errorf("target host %q must be 1..255 bytes for a SOCKS5 domain address", host)
	}
	conn, err := net.DialTimeout("tcp", proxyAddr, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial proxy %s: %w", proxyAddr, err)
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := socks5Connect(conn, host, port); err != nil {
		_ = conn.Close()
		return nil, err
	}
	// Clear the handshake deadline; the bridged stream is long-lived.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// socks5Connect runs the no-auth SOCKS5 greeting then a CONNECT request to
// host:port encoded as a domain address, and verifies the proxy's success reply.
func socks5Connect(conn net.Conn, host string, port int) error {
	// Greeting: VER=5, NMETHODS=1, METHOD=0x00 (no auth).
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return fmt.Errorf("socks5 greeting write: %w", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return fmt.Errorf("socks5 greeting read: %w", err)
	}
	if reply[0] != 0x05 {
		return fmt.Errorf("socks5 proxy answered version 0x%02x, want 0x05", reply[0])
	}
	if reply[1] != 0x00 {
		return fmt.Errorf("socks5 proxy refused no-auth (chose method 0x%02x); only no-auth is offered", reply[1])
	}
	// Request: VER=5, CMD=1 (CONNECT), RSV=0, ATYP=3 (domain), LEN, host, PORT (BE).
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, host...)
	req = append(req, byte(port>>8), byte(port&0xff))
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("socks5 connect write: %w", err)
	}
	// Reply: VER, REP, RSV, ATYP, BND.ADDR, BND.PORT. Read the fixed head first.
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("socks5 connect reply read: %w", err)
	}
	if head[0] != 0x05 {
		return fmt.Errorf("socks5 connect reply version 0x%02x, want 0x05", head[0])
	}
	if head[1] != 0x00 {
		return fmt.Errorf("socks5 proxy rejected CONNECT: reply code 0x%02x (%s)", head[1], socks5ReplyText(head[1]))
	}
	// Drain the bound address so the connection is left at the payload boundary.
	var addrLen int
	switch head[3] {
	case 0x01: // IPv4
		addrLen = 4
	case 0x04: // IPv6
		addrLen = 16
	case 0x03: // domain: a length byte then that many bytes
		lb := make([]byte, 1)
		if _, err := io.ReadFull(conn, lb); err != nil {
			return fmt.Errorf("socks5 connect reply domain len read: %w", err)
		}
		addrLen = int(lb[0])
	default:
		return fmt.Errorf("socks5 connect reply has unknown address type 0x%02x", head[3])
	}
	if _, err := io.ReadFull(conn, make([]byte, addrLen+2)); err != nil { // +2 for BND.PORT
		return fmt.Errorf("socks5 connect reply addr drain: %w", err)
	}
	return nil
}

// socks5ReplyText maps the RFC 1928 reply codes to a short reason for errors.
func socks5ReplyText(code byte) string {
	switch code {
	case 0x01:
		return "general SOCKS server failure"
	case 0x02:
		return "connection not allowed by ruleset"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	default:
		return "unknown"
	}
}
