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
	"encoding/json"
	"errors"
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/rwdxchain/go-rwdxchaina/log"
	"github.com/rwdxchain/go-rwdxchaina/p2p/discover"
	"github.com/rwdxchain/go-rwdxchaina/p2p/simulations"
	"github.com/rwdxchain/go-rwdxchaina/p2p/simulations/adapters"
)

// NodeIDs returns NodeIDs for all nodes in the network.
func (s *Simulation) NodeIDs() (ids []discover.NodeID) {
	nodes := s.Net.GetNodes()
	ids = make([]discover.NodeID, len(nodes))
	for i, node := range nodes {
		ids[i] = node.ID()
	}
	return ids
}

// UpNodeIDs returns NodeIDs for nodes that are up in the network.
func (s *Simulation) UpNodeIDs() (ids []discover.NodeID) {
	nodes := s.Net.GetNodes()
	for _, node := range nodes {
		if node.Up {
			ids = append(ids, node.ID())
		}
	}
	return ids
}

// DownNodeIDs returns NodeIDs for nodes that are stopped in the network.
func (s *Simulation) DownNodeIDs() (ids []discover.NodeID) {
	nodes := s.Net.GetNodes()
	for _, node := range nodes {
		if !node.Up {
			ids = append(ids, node.ID())
		}
	}
	return ids
}

// AddNodeOption defines the option that can be passed
// to Simulation.AddNode method.
type AddNodeOption func(*adapters.NodeConfig)

// AddNodeWithMsgEvents sets the EnableMsgEvents option
// to NodeConfig.
func AddNodeWithMsgEvents(enable bool) AddNodeOption {
	return func(o *adapters.NodeConfig) {
		o.EnableMsgEvents = enable
	}
}

// AddNodeWithService specifies a service that should be
// started on a node. This option can be repeated as variadic
// argument toe AddNode and other add node related methods.
// If AddNodeWithService is not specified, all services will be started.
func AddNodeWithService(serviceName string) AddNodeOption {
	return func(o *adapters.NodeConfig) {
		o.Services = append(o.Services, serviceName)
	}
}

// AddNode creates a new node with random configuration,
// applies provided options to the config and adds the node to network.
// By default all services will be started on a node. If one or more
// AddNodeWithService option are provided, only specified services will be started.
func (s *Simulation) AddNode(opts ...AddNodeOption) (id discover.NodeID, err error) {
	conf := adapters.RandomNodeConfig()
	for _, o := range opts {
		o(conf)
	}
	if len(conf.Services) == 0 {
		conf.Services = s.serviceNames
	}
	node, err := s.Net.NewNodeWithConfig(conf)
	if err != nil {
		return id, err
	}
	return node.ID(), s.Net.Start(node.ID())
}

// AddNodes creates new nodes with random configurations,
// applies provided options to the config and adds nodes to network.
func (s *Simulation) AddNodes(count int, opts ...AddNodeOption) (ids []discover.NodeID, err error) {
	ids = make([]discover.NodeID, 0, count)
	for i := 0; i < count; i++ {
		id, err := s.AddNode(opts...)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// AddNodesAndConnectFull is a helpper method that combines
// AddNodes and ConnectNodesFull. Only new nodes will be connected.
func (s *Simulation) AddNodesAndConnectFull(count int, opts ...AddNodeOption) (ids []discover.NodeID, err error) {
	if count < 2 {
		return nil, errors.New("count of nodes must be at least 2")
	}
	ids, err = s.AddNodes(count, opts...)
	if err != nil {
		return nil, err
	}
	err = s.ConnectNodesFull(ids)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// AddNodesAndConnectChain is a helpper method that combines
// AddNodes and ConnectNodesChain. The chain will be continued from the last
// added node, if there is one in simulation using ConnectToLastNode method.
func (s *Simulation) AddNodesAndConnectChain(count int, opts ...AddNodeOption) (ids []discover.NodeID, err error) {
	if count < 2 {
		return nil, errors.New("count of nodes must be at least 2")
	}
	id, err := s.AddNode(opts...)
	if err != nil {
		return nil, err
	}
	err = s.ConnectToLastNode(id)
	if err != nil {
		return nil, err
	}
	ids, err = s.AddNodes(count-1, opts...)
	if err != nil {
		return nil, err
	}
	ids = append([]discover.NodeID{id}, ids...)
	err = s.ConnectNodesChain(ids)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// AddNodesAndConnectRing is a helpper method that combines
// AddNodes and ConnectNodesRing.
func (s *Simulation) AddNodesAndConnectRing(count int, opts ...AddNodeOption) (ids []discover.NodeID, err error) {
	if count < 2 {
		return nil, errors.New("count of nodes must be at least 2")
	}
	ids, err = s.AddNodes(count, opts...)
	if err != nil {
		return nil, err
	}
	err = s.ConnectNodesRing(ids)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// AddNodesAndConnectStar is a helpper method that combines
// AddNodes and ConnectNodesStar.
func (s *Simulation) AddNodesAndConnectStar(count int, opts ...AddNodeOption) (ids []discover.NodeID, err error) {
	if count < 2 {
		return nil, errors.New("count of nodes must be at least 2")
	}
	ids, err = s.AddNodes(count, opts...)
	if err != nil {
		return nil, err
	}
	err = s.ConnectNodesStar(ids[0], ids[1:])
	if err != nil {
		return nil, err
	}
	return ids, nil
}

//UploadSnapshot uploads a snapshot to the simulation
//This method tries to open the json file provided, applies the config to all nodes
//and then loads the snapshot into the Simulation network
func (s *Simulation) UploadSnapshot(snapshotFile string, opts ...AddNodeOption) error {
	f, err := os.Open(snapshotFile)
	if err != nil {
		return err
	}
	defer func() {
		err := f.Close()
		if err != nil {
			log.Error("Error closing snapshot file", "err", err)
		}
	}()
	jsonbyte, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	var snap simulations.Snapshot
	err = json.Unmarshal(jsonbyte, &snap)
	if err != nil {
		return err
	}

	//the snapshot probably has the property EnableMsgEvents not set
	//just in case, set it to true!
	//(we need this to wait for messages before uploading)
	for _, n := range snap.Nodes {
		n.Node.Config.EnableMsgEvents = true
		n.Node.Config.Services = s.serviceNames
		for _, o := range opts {
			o(n.Node.Config)
		}
	}

	log.Info("Waiting for p2p connections to be established...")

	//now we can load the snapshot
	err = s.Net.Load(&snap)
	if err != nil {
		return err
	}
	log.Info("Snapshot loaded")
	return nil
}

// SetPivotNode sets the NodeID of the network's pivot node.
// Pivot node is just a specific node that should be treated
// differently then other nodes in test. SetPivotNode and
// PivotNodeID are just a convenient functions to set and
// retrieve it.
func (s *Simulation) SetPivotNode(id discover.NodeID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pivotNodeID = &id
}

// PivotNodeID returns NodeID of the pivot node set by
// Simulation.SetPivotNode method.
func (s *Simulation) PivotNodeID() (id *discover.NodeID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pivotNodeID
}

// StartNode starts a node by NodeID.
func (s *Simulation) StartNode(id discover.NodeID) (err error) {
	return s.Net.Start(id)
}

// StartRandomNode starts a random node.
func (s *Simulation) StartRandomNode() (id discover.NodeID, err error) {
	n := s.randomDownNode()
	if n == nil {
		return id, ErrNodeNotFound
	}
	return n.ID, s.Net.Start(n.ID)
}

// StartRandomNodes starts random nodes.
func (s *Simulation) StartRandomNodes(count int) (ids []discover.NodeID, err error) {
	ids = make([]discover.NodeID, 0, count)
	downIDs := s.DownNodeIDs()
	for i := 0; i < count; i++ {
		n := s.randomNode(downIDs, ids...)
		if n == nil {
			return nil, ErrNodeNotFound
		}
		err = s.Net.Start(n.ID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, n.ID)
	}
	return ids, nil
}

// StopNode stops a node by NodeID.
func (s *Simulation) StopNode(id discover.NodeID) (err error) {
	return s.Net.Stop(id)
}

// StopRandomNode stops a random node.
func (s *Simulation) StopRandomNode() (id discover.NodeID, err error) {
	n := s.RandomUpNode()
	if n == nil {
		return id, ErrNodeNotFound
	}
	return n.ID, s.Net.Stop(n.ID)
}

// StopRandomNodes stops random nodes.
func (s *Simulation) StopRandomNodes(count int) (ids []discover.NodeID, err error) {
	ids = make([]discover.NodeID, 0, count)
	upIDs := s.UpNodeIDs()
	for i := 0; i < count; i++ {
		n := s.randomNode(upIDs, ids...)
		if n == nil {
			return nil, ErrNodeNotFound
		}
		err = s.Net.Stop(n.ID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, n.ID)
	}
	return ids, nil
}

// seed the random generator for Simulation.randomNode.
func init() {
	rand.Seed(time.Now().UnixNano())
}

// RandomUpNode returns a random SimNode that is up.
// Arguments are NodeIDs for nodes that should not be returned.
func (s *Simulation) RandomUpNode(exclude ...discover.NodeID) *adapters.SimNode {
	return s.randomNode(s.UpNodeIDs(), exclude...)
}

// randomDownNode returns a random SimNode that is not up.
func (s *Simulation) randomDownNode(exclude ...discover.NodeID) *adapters.SimNode {
	return s.randomNode(s.DownNodeIDs(), exclude...)
}

// randomNode returns a random SimNode from the slice of NodeIDs.
func (s *Simulation) randomNode(ids []discover.NodeID, exclude ...discover.NodeID) *adapters.SimNode {
	for _, e := range exclude {
		var i int
		for _, id := range ids {
			if id == e {
				ids = append(ids[:i], ids[i+1:]...)
			} else {
				i++
			}
		}
	}
	l := len(ids)
	if l == 0 {
		return nil
	}
	n := s.Net.GetNode(ids[rand.Intn(l)])
	node, _ := n.Node.(*adapters.SimNode)
	return node
}
