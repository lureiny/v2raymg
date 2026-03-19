// Package grpc provides xray gRPC API client.
package grpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lureiny/v2raymg/pkg/xrayapi/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Client is a minimal gRPC client for xray runtime operations.
type Client struct {
	address string
}

// NewClient creates a new xray gRPC client.
func NewClient(address string) *Client {
	if address == "" {
		address = "127.0.0.1:62789"
	}
	return &Client{address: address}
}

// AddInboundWithProto adds an inbound using a pre-built InboundHandlerConfig.
// This method uses proto serialization for the TypedMessage values inside,
// but the outer structure is still JSON encoded for xray gRPC API compatibility.
func (c *Client) AddInboundWithProto(inboundHandlerConfig *types.InboundHandlerConfig) error {
	// Serialize using JSON (xray gRPC API expects JSON-encoded Any messages)
	jsonBytes, err := json.Marshal(inboundHandlerConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal inbound config: %w", err)
	}

	conn, err := grpc.Dial(c.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs())

	// Use raw gRPC invoke
	var response []byte
	err = conn.Invoke(ctx, "/xray.app.proxyman.command.HandlerService/AddInbound", jsonBytes, &response)
	if err != nil {
		return fmt.Errorf("gRPC call failed: %w", err)
	}
	return nil
}

// RemoveInbound removes an inbound via gRPC.
func (c *Client) RemoveInbound(tag string) error {
	type RemoveRequest struct {
		Tag string `json:"tag"`
	}
	reqData, _ := json.Marshal(RemoveRequest{Tag: tag})

	conn, err := grpc.Dial(c.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs())

	var response []byte
	err = conn.Invoke(ctx, "/xray.app.proxyman.command.HandlerService/RemoveInbound", reqData, &response)
	return err
}
