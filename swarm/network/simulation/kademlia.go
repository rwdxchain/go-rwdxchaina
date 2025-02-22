// Copyright 2018 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package simulation

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/rwdxchain/go-rwdxchaina/p2p/discover"

	"github.com/rwdxchain/go-rwdxchaina/common"
	"github.com/rwdxchain/go-rwdxchaina/log"
	"github.com/rwdxchain/go-rwdxchaina/swarm/network"
)

// BucketKeyKademlia is the key to be used for storing the kademlia
// instance for particuar node, usually inside the ServiceFunc function.
var BucketKeyKademlia BucketKey = "kademlia"

// WaitTillHealthy is blocking until the health of all kademlias is true.
// If error is not nil, a map of kademlia that was found not healthy is returned.
func (s *Simulation) WaitTillHealthy(ctx context.Context, kadMinProxSize int) (ill map[discover.NodeID]*network.Kademlia, err error) {
	// Prepare PeerPot map for checking Kademlia health
	var ppmap map[string]*network.PeerPot
	kademlias := s.kademlias()
	addrs := make([][]byte, 0, len(kademlias))
	for _, k := range kademlias {
		addrs = append(addrs, k.BaseAddr())
	}
	ppmap = network.NewPeerPotMap(kadMinProxSize, addrs)

	// Wait for healthy Kademlia on every node before checking files
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	ill = make(map[discover.NodeID]*network.Kademlia)
	for {
		select {
		case <-ctx.Done():
			return ill, ctx.Err()
		case <-ticker.C:
			for k := range ill {
				delete(ill, k)
			}
			log.Debug("kademlia health check", "addr count", len(addrs))
			for id, k := range kademlias {
				//PeerPot for this node
				addr := common.Bytes2Hex(k.BaseAddr())
				pp := ppmap[addr]
				//call Healthy RPC
				h := k.Healthy(pp)
				//print info
				log.Debug(k.String())
				log.Debug("kademlia", "empty bins", pp.EmptyBins, "gotNN", h.GotNN, "knowNN", h.KnowNN, "full", h.Full)
				log.Debug("kademlia", "health", h.GotNN && h.KnowNN && h.Full, "addr", hex.EncodeToString(k.BaseAddr()), "node", id)
				log.Debug("kademlia", "ill condition", !h.GotNN || !h.Full, "addr", hex.EncodeToString(k.BaseAddr()), "node", id)
				if !h.GotNN || !h.Full {
					ill[id] = k
				}
			}
			if len(ill) == 0 {
				return nil, nil
			}
		}
	}
}

// kademlias returns all Kademlia instances that are set
// in simulation bucket.
func (s *Simulation) kademlias() (ks map[discover.NodeID]*network.Kademlia) {
	items := s.UpNodesItems(BucketKeyKademlia)
	ks = make(map[discover.NodeID]*network.Kademlia, len(items))
	for id, v := range items {
		k, ok := v.(*network.Kademlia)
		if !ok {
			continue
		}
		ks[id] = k
	}
	return ks
}
