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

package swarm

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rwdxchain/go-rwdxchaina/crypto"
	"github.com/rwdxchain/go-rwdxchaina/log"
	"github.com/rwdxchain/go-rwdxchaina/node"
	"github.com/rwdxchain/go-rwdxchaina/p2p/discover"
	"github.com/rwdxchain/go-rwdxchaina/p2p/simulations/adapters"
	"github.com/rwdxchain/go-rwdxchaina/swarm/api"
	"github.com/rwdxchain/go-rwdxchaina/swarm/network"
	"github.com/rwdxchain/go-rwdxchaina/swarm/network/simulation"
	"github.com/rwdxchain/go-rwdxchaina/swarm/storage"
	colorable "github.com/mattn/go-colorable"
)

var (
	loglevel     = flag.Int("loglevel", 2, "verbosity of logs")
	longrunning  = flag.Bool("longrunning", false, "do run long-running tests")
	waitKademlia = flag.Bool("waitkademlia", false, "wait for healthy kademlia before checking files availability")
)

func init() {
	rand.Seed(time.Now().UnixNano())

	flag.Parse()

	log.Root().SetHandler(log.LvlFilterHandler(log.Lvl(*loglevel), log.StreamHandler(colorable.NewColorableStderr(), log.TerminalFormat(true))))
}

// TestSwarmNetwork runs a series of test simulations with
// static and dynamic Swarm nodes in network simulation, by
// uploading files to every node and retrieving them.
func TestSwarmNetwork(t *testing.T) {
	for _, tc := range []struct {
		name     string
		steps    []testSwarmNetworkStep
		options  *testSwarmNetworkOptions
		disabled bool
	}{
		{
			name: "10_nodes",
			steps: []testSwarmNetworkStep{
				{
					nodeCount: 10,
				},
			},
			options: &testSwarmNetworkOptions{
				Timeout: 45 * time.Second,
			},
		},
		{
			name: "10_nodes_skip_check",
			steps: []testSwarmNetworkStep{
				{
					nodeCount: 10,
				},
			},
			options: &testSwarmNetworkOptions{
				Timeout:   45 * time.Second,
				SkipCheck: true,
			},
		},
		{
			name: "100_nodes",
			steps: []testSwarmNetworkStep{
				{
					nodeCount: 100,
				},
			},
			options: &testSwarmNetworkOptions{
				Timeout: 3 * time.Minute,
			},
			disabled: !*longrunning,
		},
		{
			name: "100_nodes_skip_check",
			steps: []testSwarmNetworkStep{
				{
					nodeCount: 100,
				},
			},
			options: &testSwarmNetworkOptions{
				Timeout:   3 * time.Minute,
				SkipCheck: true,
			},
			disabled: !*longrunning,
		},
		{
			name: "inc_node_count",
			steps: []testSwarmNetworkStep{
				{
					nodeCount: 2,
				},
				{
					nodeCount: 5,
				},
				{
					nodeCount: 10,
				},
			},
			options: &testSwarmNetworkOptions{
				Timeout: 90 * time.Second,
			},
			disabled: !*longrunning,
		},
		{
			name: "dec_node_count",
			steps: []testSwarmNetworkStep{
				{
					nodeCount: 10,
				},
				{
					nodeCount: 6,
				},
				{
					nodeCount: 3,
				},
			},
			options: &testSwarmNetworkOptions{
				Timeout: 90 * time.Second,
			},
			disabled: !*longrunning,
		},
		{
			name: "dec_inc_node_count",
			steps: []testSwarmNetworkStep{
				{
					nodeCount: 5,
				},
				{
					nodeCount: 3,
				},
				{
					nodeCount: 10,
				},
			},
			options: &testSwarmNetworkOptions{
				Timeout: 90 * time.Second,
			},
		},
		{
			name: "inc_dec_node_count",
			steps: []testSwarmNetworkStep{
				{
					nodeCount: 3,
				},
				{
					nodeCount: 5,
				},
				{
					nodeCount: 25,
				},
				{
					nodeCount: 10,
				},
				{
					nodeCount: 4,
				},
			},
			options: &testSwarmNetworkOptions{
				Timeout: 5 * time.Minute,
			},
			disabled: !*longrunning,
		},
		{
			name: "inc_dec_node_count_skip_check",
			steps: []testSwarmNetworkStep{
				{
					nodeCount: 3,
				},
				{
					nodeCount: 5,
				},
				{
					nodeCount: 25,
				},
				{
					nodeCount: 10,
				},
				{
					nodeCount: 4,
				},
			},
			options: &testSwarmNetworkOptions{
				Timeout:   5 * time.Minute,
				SkipCheck: true,
			},
			disabled: !*longrunning,
		},
	} {
		if tc.disabled {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			testSwarmNetwork(t, tc.options, tc.steps...)
		})
	}
}

// testSwarmNetworkStep is the configuration
// for the state of the simulation network.
type testSwarmNetworkStep struct {
	// number of swarm nodes that must be in the Up state
	nodeCount int
}

// file represents the file uploaded on a particular node.
type file struct {
	addr   storage.Address
	data   string
	nodeID discover.NodeID
}

// check represents a reference to a file that is retrieved
// from a particular node.
type check struct {
	key    string
	nodeID discover.NodeID
}

// testSwarmNetworkOptions contains optional parameters for running
// testSwarmNetwork.
type testSwarmNetworkOptions struct {
	Timeout   time.Duration
	SkipCheck bool
}

// testSwarmNetwork is a helper function used for testing different
// static and dynamic Swarm network simulations.
// It is responsible for:
//  - Setting up a Swarm network simulation, and updates the number of nodes within the network on every step according to steps.
//  - Uploading a unique file to every node on every step.
//  - May wait for Kademlia on every node to be healthy.
//  - Checking if a file is retrievable from all nodes.
func testSwarmNetwork(t *testing.T, o *testSwarmNetworkOptions, steps ...testSwarmNetworkStep) {
	if o == nil {
		o = new(testSwarmNetworkOptions)
	}

	sim := simulation.New(map[string]simulation.ServiceFunc{
		"swarm": func(ctx *adapters.ServiceContext, bucket *sync.Map) (s node.Service, cleanup func(), err error) {
			config := api.NewConfig()

			dir, err := ioutil.TempDir("", "swarm-network-test-node")
			if err != nil {
				return nil, nil, err
			}
			cleanup = func() {
				err := os.RemoveAll(dir)
				if err != nil {
					log.Error("cleaning up swarm temp dir", "err", err)
				}
			}

			config.Path = dir

			privkey, err := crypto.GenerateKey()
			if err != nil {
				return nil, cleanup, err
			}

			config.Init(privkey)
			config.DeliverySkipCheck = o.SkipCheck

			swarm, err := NewSwarm(config, nil)
			if err != nil {
				return nil, cleanup, err
			}
			bucket.Store(simulation.BucketKeyKademlia, swarm.bzz.Hive.Overlay.(*network.Kademlia))
			log.Info("new swarm", "bzzKey", config.BzzKey, "baseAddr", fmt.Sprintf("%x", swarm.bzz.BaseAddr()))
			return swarm, cleanup, nil
		},
	})
	defer sim.Close()

	ctx := context.Background()
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}

	files := make([]file, 0)

	for i, step := range steps {
		log.Debug("test sync step", "n", i+1, "nodes", step.nodeCount)

		change := step.nodeCount - len(sim.UpNodeIDs())

		if change > 0 {
			_, err := sim.AddNodesAndConnectChain(change)
			if err != nil {
				t.Fatal(err)
			}
		} else if change < 0 {
			_, err := sim.StopRandomNodes(-change)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			t.Logf("step %v: no change in nodes", i)
			continue
		}

		var checkStatusM sync.Map
		var nodeStatusM sync.Map
		var totalFoundCount uint64

		result := sim.Run(ctx, func(ctx context.Context, sim *simulation.Simulation) error {
			nodeIDs := sim.UpNodeIDs()
			shuffle(len(nodeIDs), func(i, j int) {
				nodeIDs[i], nodeIDs[j] = nodeIDs[j], nodeIDs[i]
			})
			for _, id := range nodeIDs {
				key, data, err := uploadFile(sim.Service("swarm", id).(*Swarm))
				if err != nil {
					return err
				}
				log.Trace("file uploaded", "node", id, "key", key.String())
				files = append(files, file{
					addr:   key,
					data:   data,
					nodeID: id,
				})
			}

			if *waitKademlia {
				if _, err := sim.WaitTillHealthy(ctx, 2); err != nil {
					return err
				}
			}

			// File retrieval check is repeated until all uploaded files are retrieved from all nodes
			// or until the timeout is reached.
			for {
				if retrieve(sim, files, &checkStatusM, &nodeStatusM, &totalFoundCount) == 0 {
					return nil
				}
			}
		})

		if result.Error != nil {
			t.Fatal(result.Error)
		}
		log.Debug("done: test sync step", "n", i+1, "nodes", step.nodeCount)
	}
}

// uploadFile, uploads a short file to the swarm instance
// using the api.Put method.
func uploadFile(swarm *Swarm) (storage.Address, string, error) {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		return nil, "", err
	}
	// File data is very short, but it is ensured that its
	// uniqueness is very certain.
	data := fmt.Sprintf("test content %s %x", time.Now().Round(0), b)
	ctx := context.TODO()
	k, wait, err := swarm.api.Put(ctx, data, "text/plain", false)
	if err != nil {
		return nil, "", err
	}
	if wait != nil {
		err = wait(ctx)
	}
	return k, data, err
}

// retrieve is the function that is used for checking the availability of
// uploaded files in testSwarmNetwork test helper function.
func retrieve(
	sim *simulation.Simulation,
	files []file,
	checkStatusM *sync.Map,
	nodeStatusM *sync.Map,
	totalFoundCount *uint64,
) (missing uint64) {
	shuffle(len(files), func(i, j int) {
		files[i], files[j] = files[j], files[i]
	})

	var totalWg sync.WaitGroup
	errc := make(chan error)

	nodeIDs := sim.UpNodeIDs()

	totalCheckCount := len(nodeIDs) * len(files)

	for _, id := range nodeIDs {
		if _, ok := nodeStatusM.Load(id); ok {
			continue
		}
		start := time.Now()
		var checkCount uint64
		var foundCount uint64

		totalWg.Add(1)

		var wg sync.WaitGroup

		swarm := sim.Service("swarm", id).(*Swarm)
		for _, f := range files {

			checkKey := check{
				key:    f.addr.String(),
				nodeID: id,
			}
			if n, ok := checkStatusM.Load(checkKey); ok && n.(int) == 0 {
				continue
			}

			checkCount++
			wg.Add(1)
			go func(f file, id discover.NodeID) {
				defer wg.Done()

				log.Debug("api get: check file", "node", id.String(), "key", f.addr.String(), "total files found", atomic.LoadUint64(totalFoundCount))

				r, _, _, _, err := swarm.api.Get(context.TODO(), api.NOOPDecrypt, f.addr, "/")
				if err != nil {
					errc <- fmt.Errorf("api get: node %s, key %s, kademlia %s: %v", id, f.addr, swarm.bzz.Hive, err)
					return
				}
				d, err := ioutil.ReadAll(r)
				if err != nil {
					errc <- fmt.Errorf("api get: read response: node %s, key %s: kademlia %s: %v", id, f.addr, swarm.bzz.Hive, err)
					return
				}
				data := string(d)
				if data != f.data {
					errc <- fmt.Errorf("file contend missmatch: node %s, key %s, expected %q, got %q", id, f.addr, f.data, data)
					return
				}
				checkStatusM.Store(checkKey, 0)
				atomic.AddUint64(&foundCount, 1)
				log.Info("api get: file found", "node", id.String(), "key", f.addr.String(), "content", data, "files found", atomic.LoadUint64(&foundCount))
			}(f, id)
		}

		go func(id discover.NodeID) {
			defer totalWg.Done()
			wg.Wait()

			atomic.AddUint64(totalFoundCount, foundCount)

			if foundCount == checkCount {
				log.Info("all files are found for node", "id", id.String(), "duration", time.Since(start))
				nodeStatusM.Store(id, 0)
				return
			}
			log.Debug("files missing for node", "id", id.String(), "check", checkCount, "found", foundCount)
		}(id)

	}

	go func() {
		totalWg.Wait()
		close(errc)
	}()

	var errCount int
	for err := range errc {
		if err != nil {
			errCount++
		}
		log.Warn(err.Error())
	}

	log.Info("check stats", "total check count", totalCheckCount, "total files found", atomic.LoadUint64(totalFoundCount), "total errors", errCount)

	return uint64(totalCheckCount) - atomic.LoadUint64(totalFoundCount)
}

// Backported from stdlib https://golang.org/src/math/rand/rand.go?s=11175:11215#L333
//
// Replace with rand.Shuffle from go 1.10 when go 1.9 support is dropped.
//
// shuffle pseudo-randomizes the order of elements.
// n is the number of elements. Shuffle panics if n < 0.
// swap swaps the elements with indexes i and j.
func shuffle(n int, swap func(i, j int)) {
	if n < 0 {
		panic("invalid argument to Shuffle")
	}

	// Fisher-Yates shuffle: https://en.wikipedia.org/wiki/Fisher%E2%80%93Yates_shuffle
	// Shuffle really ought not be called with n that doesn't fit in 32 bits.
	// Not only will it take a very long time, but with 2????! possible permutations,
	// there's no way that any PRNG can have a big enough internal state to
	// generate even a minuscule percentage of the possible permutations.
	// Nevertheless, the right API signature accepts an int n, so handle it as best we can.
	i := n - 1
	for ; i > 1<<31-1-1; i-- {
		j := int(rand.Int63n(int64(i + 1)))
		swap(i, j)
	}
	for ; i > 0; i-- {
		j := int(rand.Int31n(int32(i + 1)))
		swap(i, j)
	}
}
