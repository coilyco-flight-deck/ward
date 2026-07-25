package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/urfave/cli/v3"
)

const (
	envContainerService          = "WARD_CONTAINER_SERVICE"
	envDispatchBrokerListen      = "WARD_DISPATCH_BROKER_LISTEN"
	envDispatchBrokerRequester   = "WARD_DISPATCH_BROKER_REQUESTER"
	envDispatchBrokerID          = "WARD_DISPATCH_BROKER_ID"
	envPersistentDispatchBroker  = "WARD_PERSISTENT_DISPATCH_BROKER"
	dispatchBrokerService        = "dispatch-broker"
	dispatchBrokerServicePort    = "7420"
	dispatchBrokerServiceListen  = "0.0.0.0:" + dispatchBrokerServicePort
	dispatchBrokerServiceAddress = "broker:" + dispatchBrokerServicePort
	dispatchBrokerProbeAddress   = "127.0.0.1:" + dispatchBrokerServicePort
)

// containerDispatchBrokerCommand runs the independently supervised broker
// service. The image entrypoint selects this leaf before agent bootstrap.
func containerDispatchBrokerCommand() *cli.Command {
	return &cli.Command{
		Name:   dispatchBrokerService,
		Hidden: true,
		Usage:  "Container-internal dispatch broker service for a director stack.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "listen",
				Usage:   "TCP address exposed to the director over the Compose network",
				Sources: cli.EnvVars(envDispatchBrokerListen),
				Value:   dispatchBrokerServiceListen,
			},
			&cli.StringFlag{
				Name:    "requester",
				Usage:   "director container name this broker serves",
				Sources: cli.EnvVars(envDispatchBrokerRequester),
			},
		},
		Action: runContainerDispatchBroker,
	}
}

func containerDispatchBrokerProbeCommand() *cli.Command {
	return &cli.Command{
		Name:   "dispatch-broker-probe",
		Hidden: true,
		Usage:  "Container-internal health probe for the dispatch broker service.",
		Action: func(ctx context.Context, _ *cli.Command) error {
			addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
			if addr == "" {
				addr = dispatchBrokerProbeAddress
			}
			token := strings.TrimSpace(os.Getenv(envDispatchBrokerToken))
			if token == "" {
				return fmt.Errorf("ward dispatch broker probe: %s must be set", envDispatchBrokerToken)
			}
			_, err := sendDispatchBrokerRequest(ctx, addr, dispatchBrokerRequest{
				Action:    dispatchActionPing,
				Requester: strings.TrimSpace(os.Getenv(envDispatchBrokerRequester)),
				Token:     token,
			})
			if err != nil {
				return fmt.Errorf("ward dispatch broker probe: %w", err)
			}
			return nil
		},
	}
}

func runContainerDispatchBroker(ctx context.Context, c *cli.Command) error {
	r := newRunner()
	token := strings.TrimSpace(os.Getenv(envDispatchBrokerToken))
	if token == "" {
		return fmt.Errorf("ward dispatch broker service: %s is not set", envDispatchBrokerToken)
	}
	listen := strings.TrimSpace(c.String("listen"))
	ln, err := net.Listen("tcp", listen) //nolint:gosec // Compose-private network, token-authenticated protocol
	if err != nil {
		return fmt.Errorf("ward dispatch broker service: listen on %s: %w", listen, err)
	}
	defer func() { _ = ln.Close() }()

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	brokerID := strings.TrimSpace(os.Getenv(envDispatchBrokerID))
	if err := r.reconcileDispatchJournals(ctx, brokerID); err != nil {
		return fmt.Errorf("ward dispatch broker service: reconcile accepted work: %w", err)
	}
	requester := strings.TrimSpace(c.String("requester"))
	fmt.Fprintf(os.Stderr, "ward dispatch broker service: ready on %s for %s\n",
		listen, emptyDefault(requester, "any-director"))
	r.serveHostDispatchBroker(ctx, ln, requester, token)
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	fmt.Fprintln(os.Stderr, "ward dispatch broker service: shut down cleanly")
	return nil
}
