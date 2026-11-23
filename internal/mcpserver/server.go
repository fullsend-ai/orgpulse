package mcpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

const protocolVersion = "2024-11-05"

type Server struct {
	client    *apiClient
	transport *transport
	handlers  map[string]toolHandler
	tools     []toolDefinition
}

func New(baseURL, apiKey string, stdin io.Reader, stdout io.Writer) *Server {
	return &Server{
		client:    newAPIClient(baseURL, apiKey),
		transport: newTransport(stdin, stdout),
		handlers:  toolHandlers(),
		tools:     allTools(),
	}
}

func (s *Server) Run() error {
	for {
		msg, err := s.transport.readMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read message: %w", err)
		}
		if err := s.handleMessage(msg); err != nil {
			slog.Error("handle message", "method", msg.Method, "error", err)
		}
	}
}

func (s *Server) handleMessage(msg *jsonRPCRequest) error {
	switch msg.Method {
	case "initialize":
		return s.handleInitialize(msg)
	case "notifications/initialized":
		return nil
	case "tools/list":
		return s.handleToolsList(msg)
	case "tools/call":
		return s.handleToolsCall(msg)
	case "ping":
		return s.transport.writeResponse(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result:  map[string]any{},
		})
	default:
		return s.transport.writeResponse(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error:   &jsonRPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", msg.Method)},
		})
	}
}

func (s *Server) handleInitialize(msg *jsonRPCRequest) error {
	return s.transport.writeResponse(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result: map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "orgpulse",
				"version": "1.0.0",
			},
		},
	})
}

func (s *Server) handleToolsList(msg *jsonRPCRequest) error {
	return s.transport.writeResponse(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result: map[string]any{
			"tools": s.tools,
		},
	})
}

func (s *Server) handleToolsCall(msg *jsonRPCRequest) error {
	var params toolsCallParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.transport.writeResponse(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error:   &jsonRPCError{Code: -32602, Message: "invalid params"},
		})
	}

	handler, ok := s.handlers[params.Name]
	if !ok {
		return s.transport.writeResponse(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result:  formatResult(nil, fmt.Errorf("unknown tool: %s", params.Name)),
		})
	}

	result, err := handler(s.client, params.Arguments)
	return s.transport.writeResponse(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result:  formatResult(result, err),
	})
}
