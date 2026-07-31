package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"
)

const dispatchBrokerMessageMaxBytes = 64 * 1024

type dispatchBrokerMessage struct {
	ID           string    `json:"id"`
	Conversation string    `json:"conversation,omitempty"`
	From         string    `json:"from"`
	To           string    `json:"to"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
}

var dispatchBrokerMessageMu sync.Mutex

func agentMessageCommand() *cli.Command {
	return &cli.Command{
		Name:  "message",
		Usage: "Exchange authenticated messages with agents in the same broker group.",
		Commands: []*cli.Command{
			{
				Name:      "send",
				Usage:     "Send a message. The broker stamps the authenticated sender identity.",
				ArgsUsage: "<message>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "to", Required: true, Usage: "recipient agent id, or * for every agent"},
					&cli.StringFlag{Name: "conversation", Usage: "optional conversation id"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					body := strings.TrimSpace(strings.Join(c.Args().Slice(), " "))
					req := dispatchBrokerRequest{
						Action:       dispatchActionMessageSend,
						To:           strings.TrimSpace(c.String("to")),
						Message:      body,
						Conversation: strings.TrimSpace(c.String("conversation")),
					}
					resp, err := sendAgentMessageRequest(ctx, req)
					if err != nil {
						return err
					}
					if len(resp.Messages) == 1 {
						writef(agentCommandWriter(c), "%s\n", resp.Messages[0].ID)
					}
					return nil
				},
			},
			{
				Name:  "receive",
				Usage: "Read messages addressed to this authenticated agent.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "after", Usage: "return messages after this message id"},
					&cli.StringFlag{Name: "conversation", Usage: "filter by conversation id"},
					&cli.BoolFlag{Name: "json", Usage: "emit the message array as JSON"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					resp, err := sendAgentMessageRequest(ctx, dispatchBrokerRequest{
						Action:       dispatchActionMessageReceive,
						After:        strings.TrimSpace(c.String("after")),
						Conversation: strings.TrimSpace(c.String("conversation")),
					})
					if err != nil {
						return err
					}
					if c.Bool("json") {
						return json.NewEncoder(agentCommandWriter(c)).Encode(resp.Messages)
					}
					for _, message := range resp.Messages {
						writef(agentCommandWriter(c), "%s\t%s\t%s\n", message.ID, message.From, message.Body)
					}
					return nil
				},
			},
		},
	}
}

func agentCommandWriter(c *cli.Command) interface{ Write([]byte) (int, error) } {
	if c != nil && c.Root() != nil && c.Root().Writer != nil {
		return c.Root().Writer
	}
	return os.Stdout
}

func sendAgentMessageRequest(ctx context.Context, req dispatchBrokerRequest) (dispatchBrokerResponse, error) {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	token := strings.TrimSpace(os.Getenv(envDispatchBrokerToken))
	if addr == "" || token == "" {
		return dispatchBrokerResponse{}, fmt.Errorf("ward agent message: no dispatch broker capability is available")
	}
	req.Token = token
	conn, err := dialDispatchBroker(ctx, addr)
	if err != nil {
		return dispatchBrokerResponse{}, dispatchBrokerDialDiagnostic(addr, err)
	}
	defer func() { _ = conn.Close() }()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return dispatchBrokerResponse{}, fmt.Errorf("ward agent message: send request: %w", err)
	}
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return dispatchBrokerResponse{}, fmt.Errorf("ward agent message: read response: %w", err)
	}
	if !resp.OK {
		return resp, fmt.Errorf("ward agent message: %s", resp.Error)
	}
	return resp, nil
}

func validateDispatchBrokerMessageSend(req dispatchBrokerRequest) error {
	if len(req.Argv) != 0 || req.Target != "" {
		return fmt.Errorf("dispatch broker: message-send takes no launch argv or target")
	}
	if !validDispatchMessageRecipient(req.To) {
		return fmt.Errorf("dispatch broker: invalid message recipient %q", req.To)
	}
	if err := validateDispatchConversation(req.Conversation); err != nil {
		return err
	}
	if strings.TrimSpace(req.Message) == "" {
		return fmt.Errorf("dispatch broker: message body is required")
	}
	if len(req.Message) > dispatchBrokerMessageMaxBytes {
		return fmt.Errorf("dispatch broker: message body exceeds %d bytes", dispatchBrokerMessageMaxBytes)
	}
	return nil
}

func validateDispatchBrokerMessageReceive(req dispatchBrokerRequest) error {
	if len(req.Argv) != 0 || req.Target != "" || req.To != "" || req.Message != "" {
		return fmt.Errorf("dispatch broker: message-receive carries only after and conversation filters")
	}
	if req.After != "" && !dispatchRequestIDPattern.MatchString(req.After) {
		return fmt.Errorf("dispatch broker: invalid after message id %q", req.After)
	}
	return validateDispatchConversation(req.Conversation)
}

func validateDispatchConversation(conversation string) error {
	if conversation != "" && !validDispatchAgentID(conversation) {
		return fmt.Errorf("dispatch broker: invalid conversation id %q", conversation)
	}
	return nil
}

func validDispatchMessageRecipient(recipient string) bool {
	return recipient == "*" || validDispatchAgentID(recipient)
}

func (r *Runner) runDispatchBrokerMessageSend(conn net.Conn, req dispatchBrokerRequest) {
	if err := validateDispatchBrokerMessageSend(req); err != nil {
		writeDispatchBrokerMessagesResponse(conn, nil, err)
		return
	}
	message := dispatchBrokerMessage{
		ID:           newDispatchBrokerRequestID(),
		Conversation: strings.TrimSpace(req.Conversation),
		From:         req.Requester,
		To:           strings.TrimSpace(req.To),
		Body:         req.Message,
		CreatedAt:    time.Now().UTC(),
	}
	if err := appendDispatchBrokerMessage(req.BrokerID, message); err != nil {
		writeDispatchBrokerMessagesResponse(conn, nil, err)
		return
	}
	writeDispatchBrokerMessagesResponse(conn, []dispatchBrokerMessage{message}, nil)
}

func (r *Runner) runDispatchBrokerMessageReceive(conn net.Conn, req dispatchBrokerRequest) {
	if err := validateDispatchBrokerMessageReceive(req); err != nil {
		writeDispatchBrokerMessagesResponse(conn, nil, err)
		return
	}
	messages, err := readDispatchBrokerMessages(req.BrokerID, req.Requester, req.After, req.Conversation)
	writeDispatchBrokerMessagesResponse(conn, messages, err)
}

func writeDispatchBrokerMessagesResponse(conn net.Conn, messages []dispatchBrokerMessage, err error) {
	resp := dispatchBrokerResponse{OK: err == nil, Messages: messages}
	if err != nil {
		resp.Error = err.Error()
	}
	if data, marshalErr := json.Marshal(resp); marshalErr == nil {
		_, _ = conn.Write(data)
	}
}

func dispatchBrokerMessagesPath(brokerID string) string {
	sum := sha256.Sum256([]byte(emptyDefault(strings.TrimSpace(brokerID), "broker")))
	return filepath.Join(agentLogsDir(), "messages", hex.EncodeToString(sum[:12])+".jsonl")
}

func appendDispatchBrokerMessage(brokerID string, message dispatchBrokerMessage) error {
	dispatchBrokerMessageMu.Lock()
	defer dispatchBrokerMessageMu.Unlock()
	path := dispatchBrokerMessagesPath(brokerID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("dispatch broker: create message store: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- Ward-owned hashed state path
	if err != nil {
		return fmt.Errorf("dispatch broker: open message store: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := json.NewEncoder(file).Encode(message); err != nil {
		return fmt.Errorf("dispatch broker: append message: %w", err)
	}
	return file.Sync()
}

func readDispatchBrokerMessages(brokerID, recipient, after, conversation string) ([]dispatchBrokerMessage, error) {
	dispatchBrokerMessageMu.Lock()
	defer dispatchBrokerMessageMu.Unlock()
	file, err := os.Open(dispatchBrokerMessagesPath(brokerID)) // #nosec G304 -- Ward-owned hashed state path
	if os.IsNotExist(err) {
		return []dispatchBrokerMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dispatch broker: open message store: %w", err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), dispatchBrokerMessageMaxBytes*8)
	var messages []dispatchBrokerMessage
	pastAfter := after == ""
	for scanner.Scan() {
		var message dispatchBrokerMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			return nil, fmt.Errorf("dispatch broker: decode message store: %w", err)
		}
		if !pastAfter {
			pastAfter = message.ID == after
			continue
		}
		if message.To != recipient && message.To != "*" {
			continue
		}
		if conversation != "" && message.Conversation != conversation {
			continue
		}
		messages = append(messages, message)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dispatch broker: read message store: %w", err)
	}
	return messages, nil
}
