/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package ironic provides a client for interacting with OpenStack Ironic bare metal API.
package ironic

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/baremetal/v1/nodes"
	"github.com/gophercloud/utils/v2/openstack/clientconfig"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// NodeClient is the interface for interacting with Ironic nodes.
type NodeClient interface {
	GetNode(ctx context.Context, nodeID string) (*nodes.Node, error)
	SetPowerState(ctx context.Context, nodeID string, target TargetPowerState) error
	IsNodePowerTransitioning(node *nodes.Node) bool
}

// Client talks to Ironic over REST via gophercloud.
type (
	Client struct {
		serviceClient    *gophercloud.ServiceClient
		newServiceClient func(ctx context.Context) (*gophercloud.ServiceClient, error)
	}

	TargetPowerState struct {
		powerstate nodes.TargetPowerState
	}
)

func (t TargetPowerState) String() string {
	return string(t.powerstate)
}

var (
	PowerOn       = TargetPowerState{nodes.PowerOn}
	PowerOff      = TargetPowerState{nodes.PowerOff}
	Rebooting     = TargetPowerState{nodes.Rebooting}
	SoftPowerOff  = TargetPowerState{nodes.SoftPowerOff}
	SoftRebooting = TargetPowerState{nodes.SoftRebooting}
)

// NewClient creates an Ironic client configured via clouds.yaml or OS_* environment variables.
func NewClient(ctx context.Context) (*Client, error) {
	factory := func(ctx context.Context) (*gophercloud.ServiceClient, error) {
		return clientconfig.NewServiceClient(ctx, "baremetal", nil)
	}
	sc, err := factory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create baremetal client: %w", err)
	}
	if sc == nil {
		return nil, fmt.Errorf("failed to create baremetal client: nil service client")
	}
	if sc.Endpoint == "" {
		return nil, fmt.Errorf("baremetal client has no endpoint configured")
	}
	return &Client{
		serviceClient:    sc,
		newServiceClient: factory,
	}, nil
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	if gophercloud.ResponseCodeIs(err, http.StatusUnauthorized) {
		return true
	}
	var errReauth *gophercloud.ErrUnableToReauthenticate
	if errors.As(err, &errReauth) {
		return true
	}
	var errAfterReauth *gophercloud.ErrErrorAfterReauthentication
	return errors.As(err, &errAfterReauth)
}

func (c *Client) reconnect(ctx context.Context) error {
	log := ctrllog.FromContext(ctx)
	log.Info("recreating ironic service client after authentication failure")
	sc, err := c.newServiceClient(ctx)
	if err != nil {
		log.Error(err, "failed to recreate ironic service client")
		return fmt.Errorf("failed to recreate baremetal client: %w", err)
	}
	if sc == nil {
		return fmt.Errorf("failed to recreate baremetal client: nil service client")
	}
	if sc.Endpoint == "" {
		return fmt.Errorf("recreated baremetal client has no endpoint configured")
	}
	c.serviceClient = sc
	log.Info("ironic service client reconnected successfully", "endpoint", sc.Endpoint)
	return nil
}

// GetNode fetches a node by UUID or name from Ironic.
func (c *Client) GetNode(ctx context.Context, nodeID string) (*nodes.Node, error) {
	log := ctrllog.FromContext(ctx)
	node, err := nodes.Get(ctx, c.serviceClient, nodeID).Extract()
	if err != nil {
		if isAuthError(err) {
			log.Info("auth error on GetNode, attempting reconnect", "nodeID", nodeID, "error", err)
			if reconnErr := c.reconnect(ctx); reconnErr != nil {
				return nil, fmt.Errorf("get node %s: reconnect failed: %w", nodeID, reconnErr)
			}
			node, err = nodes.Get(ctx, c.serviceClient, nodeID).Extract()
			if err != nil {
				return nil, fmt.Errorf("get node %s after reconnect: %w", nodeID, err)
			}
			return node, nil
		}
		return nil, fmt.Errorf("get node %s: %w", nodeID, err)
	}
	return node, nil
}

func (c *Client) GetEndpoint() string {
	return c.serviceClient.Endpoint
}

// IsNodePowerTransitioning returns true if the node is currently transitioning
// to a new power state (i.e., TargetPowerState is non-empty).
func (c *Client) IsNodePowerTransitioning(node *nodes.Node) bool {
	return node.TargetPowerState != ""
}

// SetPowerState requests power on or off for the node via Ironic.
func (c *Client) SetPowerState(ctx context.Context, nodeID string, target TargetPowerState) error {
	log := ctrllog.FromContext(ctx)
	res := nodes.ChangePowerState(ctx, c.serviceClient, nodeID, nodes.PowerStateOpts{Target: target.powerstate})
	if err := res.ExtractErr(); err != nil {
		if isAuthError(err) {
			log.Info("auth error on SetPowerState, attempting reconnect", "nodeID", nodeID, "target", target.String(), "error", err)
			if reconnErr := c.reconnect(ctx); reconnErr != nil {
				return fmt.Errorf("failed to set power state on node %s: reconnect failed: %w", nodeID, reconnErr)
			}
			res = nodes.ChangePowerState(ctx, c.serviceClient, nodeID, nodes.PowerStateOpts{Target: target.powerstate})
			if err := res.ExtractErr(); err != nil {
				return fmt.Errorf("failed to set power state on node %s after reconnect: %w", nodeID, err)
			}
			return nil
		}
		return fmt.Errorf("failed to set power state on node %s: %w", nodeID, err)
	}
	return nil
}
