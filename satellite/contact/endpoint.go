// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package contact

import (
	"context"
	"time"

	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/common/identity"
	"storj.io/common/pb"
	"storj.io/common/rpc/rpcstatus"
	"storj.io/common/storj"
	"storj.io/storj/private/nodeoperator"
	"storj.io/storj/satellite/overlay"
)

var (
	errPingBackDial    = errs.Class("pingback dialing error")
	errCheckInIdentity = errs.Class("check-in identity error")
	errCheckInNetwork  = errs.Class("check-in network error")
)

// Endpoint implements the contact service Endpoints.
type Endpoint struct {
	pb.DRPCNodeUnimplementedServer
	log     *zap.Logger
	service *Service
}

// NewEndpoint returns a new contact service endpoint.
func NewEndpoint(log *zap.Logger, service *Service) *Endpoint {
	return &Endpoint{
		log:     log,
		service: service,
	}
}

// CheckIn is periodically called by storage nodes to keep the satellite informed of its existence,
// address, and operator information. In return, this satellite keeps the node informed of its
// reachability.
// When a node checks-in with the satellite, the satellite pings the node back to confirm they can
// successfully connect.
func (endpoint *Endpoint) CheckIn(ctx context.Context, req *pb.CheckInRequest) (_ *pb.CheckInResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	peerID, err := identity.PeerIdentityFromContext(ctx)
	if err != nil {
		endpoint.log.Info("failed to get node ID from context", zap.String("node address", req.Address), zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.Unknown, errCheckInIdentity.New("failed to get ID from context: %v", err).Error())
	}
	nodeID := peerID.ID

	err = endpoint.service.peerIDs.Set(ctx, nodeID, peerID)
	if err != nil {
		endpoint.log.Info("failed to add peer identity entry for ID", zap.String("node address", req.Address), zap.Stringer("Node ID", nodeID), zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.FailedPrecondition, errCheckInIdentity.New("failed to add peer identity entry for ID: %v", err).Error())
	}

	resolvedIPPort, resolvedNetwork, err := overlay.ResolveIPAndNetwork(ctx, req.Address)
	if err != nil {
		endpoint.log.Info("failed to resolve IP from address", zap.String("node address", req.Address), zap.Stringer("Node ID", nodeID), zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, errCheckInNetwork.New("failed to resolve IP from address: %s, err: %v", req.Address, err).Error())
	}

	nodeurl := storj.NodeURL{
		ID:      nodeID,
		Address: req.Address,
	}
	pingNodeSuccess, pingErrorMessage, err := endpoint.service.PingBack(ctx, nodeurl)
	if err != nil {
		endpoint.log.Info("failed to ping back address", zap.String("node address", req.Address), zap.Stringer("Node ID", nodeID), zap.Error(err))
		if errPingBackDial.Has(err) {
			err = errCheckInNetwork.New("failed dialing address when attempting to ping node (ID: %s): %s, err: %v", nodeID, req.Address, err)
			return nil, rpcstatus.Error(rpcstatus.NotFound, err.Error())
		}
		err = errCheckInNetwork.New("failed to ping node (ID: %s) at address: %s, err: %v", nodeID, req.Address, err)
		return nil, rpcstatus.Error(rpcstatus.NotFound, err.Error())
	}

	// check wallet features
	if req.Operator != nil {
		if err := nodeoperator.DefaultWalletFeaturesValidation.Validate(req.Operator.WalletFeatures); err != nil {
			endpoint.log.Debug("ignoring invalid wallet features",
				zap.Stringer("Node ID", nodeID),
				zap.Strings("Wallet Features", req.Operator.WalletFeatures))

			// TODO: Update CheckInResponse to include wallet feature validation error
			req.Operator.WalletFeatures = nil
		}
	}

	nodeInfo := overlay.NodeCheckInInfo{
		NodeID: peerID.ID,
		Address: &pb.NodeAddress{
			Address:   req.Address,
			Transport: pb.NodeTransport_TCP_TLS_GRPC,
		},
		LastNet:    resolvedNetwork,
		LastIPPort: resolvedIPPort,
		IsUp:       pingNodeSuccess,
		Capacity:   req.Capacity,
		Operator:   req.Operator,
		Version:    req.Version,
	}

	err = endpoint.service.overlay.UpdateCheckIn(ctx, nodeInfo, time.Now().UTC())
	if err != nil {
		endpoint.log.Info("failed to update check in", zap.String("node address", req.Address), zap.Stringer("Node ID", nodeID), zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.Internal, Error.Wrap(err).Error())
	}

	endpoint.log.Debug("checking in", zap.String("node addr", req.Address), zap.Bool("ping node success", pingNodeSuccess), zap.String("ping node err msg", pingErrorMessage))
	return &pb.CheckInResponse{
		PingNodeSuccess:  pingNodeSuccess,
		PingErrorMessage: pingErrorMessage,
	}, nil
}

// GetTime returns current timestamp.
func (endpoint *Endpoint) GetTime(ctx context.Context, req *pb.GetTimeRequest) (_ *pb.GetTimeResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	peerID, err := identity.PeerIdentityFromContext(ctx)
	if err != nil {
		endpoint.log.Info("failed to get node ID from context", zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, errCheckInIdentity.New("failed to get ID from context: %v", err).Error())
	}

	currentTimestamp := time.Now().UTC()
	endpoint.log.Debug("get system current time", zap.Stringer("timestamp", currentTimestamp), zap.Stringer("node id", peerID.ID))
	return &pb.GetTimeResponse{
		Timestamp: currentTimestamp,
	}, nil
}
