package main

import (
	"context"
	"encoding/json"
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
	envClusterID                 = "WARD_CLUSTER_ID"
	envCollaborationPlan         = "WARD_COLLABORATION_PLAN"
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

func containerDispatchBrokerCapabilityCommand() *cli.Command {
	return &cli.Command{
		Name:      "dispatch-broker-capability",
		Hidden:    true,
		Usage:     "Mint a peer capability from inside the supervised broker service.",
		ArgsUsage: "<agent-id>",
		Action: func(_ context.Context, c *cli.Command) error {
			if strings.TrimSpace(os.Getenv(envContainerService)) != dispatchBrokerService {
				return fmt.Errorf("ward dispatch broker capability: available only inside the broker service")
			}
			agentID := strings.TrimSpace(c.Args().First())
			if !validDispatchAgentID(agentID) {
				return fmt.Errorf("ward dispatch broker capability: invalid agent id %q", agentID)
			}
			master := strings.TrimSpace(os.Getenv(envDispatchBrokerToken))
			if master == "" {
				return fmt.Errorf("ward dispatch broker capability: %s is not set", envDispatchBrokerToken)
			}
			writef(agentCommandWriter(c), "%s\n", dispatchBrokerAgentCapability(master, agentID))
			return nil
		},
	}
}

type dispatchBrokerPeerAdmissionResponse struct {
	ClusterID  string `json:"cluster_id"`
	PeerID     string `json:"peer_id"`
	RequestID  string `json:"request_id"`
	Capability string `json:"capability"`
}

func containerDispatchBrokerPeerAdmitCommand() *cli.Command {
	return &cli.Command{
		Name:   "dispatch-broker-peer-admit",
		Hidden: true,
		Usage:  "Broker-internal admission for a host-mounted collaboration peer.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "role", Required: true},
			&cli.StringFlag{Name: "request-id", Required: true},
			&cli.StringFlag{Name: "agent-id"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			response, err := admitHostMountedDispatchPeer(c.String("role"), c.String("request-id"), c.String("agent-id"))
			if err != nil {
				return err
			}
			return json.NewEncoder(agentCommandWriter(c)).Encode(response)
		},
	}
}

func admitHostMountedDispatchPeer(role, requestID, explicitID string) (dispatchBrokerPeerAdmissionResponse, error) { //nolint:gocyclo,cyclop // admission validates each authority input before journaling
	if strings.TrimSpace(os.Getenv(envContainerService)) != dispatchBrokerService {
		return dispatchBrokerPeerAdmissionResponse{}, fmt.Errorf("ward dispatch broker peer admission: available only inside the broker service")
	}
	clusterID := strings.TrimSpace(os.Getenv(envDispatchBrokerID))
	if !validClusterID(clusterID) {
		return dispatchBrokerPeerAdmissionResponse{}, fmt.Errorf("ward dispatch broker peer admission: invalid cluster id %q", clusterID)
	}
	role = strings.TrimSpace(role)
	if !validComposedRole(role) || role == roleEngineer || role == roleQA {
		return dispatchBrokerPeerAdmissionResponse{}, fmt.Errorf("ward dispatch broker peer admission: invalid generic role %q", role)
	}
	requestID = strings.TrimSpace(requestID)
	if !dispatchRequestIDPattern.MatchString(requestID) {
		return dispatchBrokerPeerAdmissionResponse{}, fmt.Errorf("ward dispatch broker peer admission: invalid request id %q", requestID)
	}
	explicitID = strings.TrimSpace(explicitID)
	if explicitID != "" && !validDispatchAgentID(explicitID) {
		return dispatchBrokerPeerAdmissionResponse{}, fmt.Errorf("ward dispatch broker peer admission: invalid compatibility agent id %q", explicitID)
	}
	argv := []string{"run", "--role", role}
	if explicitID != "" {
		argv = append(argv, "--agent-id", explicitID)
	}
	argv = append(argv, "host-mounted collaboration peer")
	req := dispatchBrokerRequest{
		Action: dispatchActionLaunch, RequestID: requestID, BrokerID: clusterID,
		Role: role, AgentID: explicitID, Argv: argv,
	}
	if err := validateDispatchBrokerLaunch(req); err != nil {
		return dispatchBrokerPeerAdmissionResponse{}, err
	}
	created, err := admitDispatchPeer(&req)
	if err != nil {
		return dispatchBrokerPeerAdmissionResponse{}, err
	}
	_, logf, _, _, err := acceptDispatchLaunch(req)
	if logf != nil {
		_ = logf.Close()
	}
	if err != nil {
		if created {
			_ = updateDispatchPeerStatus(req, dispatchPeerStatusFailed)
		}
		return dispatchBrokerPeerAdmissionResponse{}, err
	}
	master := strings.TrimSpace(os.Getenv(envDispatchBrokerToken))
	if master == "" {
		if created {
			_ = updateDispatchPeerStatus(req, dispatchPeerStatusFailed)
		}
		return dispatchBrokerPeerAdmissionResponse{}, fmt.Errorf("ward dispatch broker peer admission: %s is not set", envDispatchBrokerToken)
	}
	return dispatchBrokerPeerAdmissionResponse{
		ClusterID: clusterID, PeerID: req.AgentID, RequestID: requestID,
		Capability: dispatchBrokerAgentCapability(master, req.AgentID),
	}, nil
}

func containerDispatchBrokerPeerStatusCommand() *cli.Command {
	return &cli.Command{
		Name:   "dispatch-broker-peer-status",
		Hidden: true,
		Usage:  "Broker-internal terminal status for a host-mounted collaboration peer.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "role", Required: true},
			&cli.StringFlag{Name: "request-id", Required: true},
			&cli.StringFlag{Name: "agent-id", Required: true},
			&cli.StringFlag{Name: "status", Required: true},
			&cli.StringFlag{Name: "detail"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			return finishHostMountedDispatchPeer(c.String("role"), c.String("request-id"), c.String("agent-id"), c.String("status"), c.String("detail"))
		},
	}
}

func finishHostMountedDispatchPeer(role, requestID, agentID, status, detail string) error {
	if strings.TrimSpace(os.Getenv(envContainerService)) != dispatchBrokerService {
		return fmt.Errorf("ward dispatch broker peer status: available only inside the broker service")
	}
	clusterID := strings.TrimSpace(os.Getenv(envDispatchBrokerID))
	req := dispatchBrokerRequest{
		Action: dispatchActionLaunch, RequestID: strings.TrimSpace(requestID), BrokerID: clusterID,
		Role: strings.TrimSpace(role), AgentID: strings.TrimSpace(agentID),
	}
	journalPath, err := dispatchJournalPath(req.RequestID)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(status) {
	case dispatchPeerStatusActive:
		if err := updateDispatchJournal(journalPath, dispatchPhaseTerminal, "", dispatchOutcomeLaunched, nil); err != nil {
			return err
		}
	case dispatchPeerStatusFailed:
		launchErr := errors.New(emptyDefault(strings.TrimSpace(detail), "host-mounted collaboration launch failed"))
		if err := updateDispatchJournal(journalPath, dispatchPhaseTerminal, "", dispatchOutcomeFailed, launchErr); err != nil {
			return err
		}
	default:
		return fmt.Errorf("ward dispatch broker peer status: invalid terminal status %q", status)
	}
	return updateDispatchPeerStatus(req, strings.TrimSpace(status))
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
	if err := reconcileDispatchPeerAdmissions(brokerID); err != nil {
		return fmt.Errorf("ward dispatch broker service: reconcile peer identities: %w", err)
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
