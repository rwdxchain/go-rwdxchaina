// Copyright 2016 The go-ethereum Authors
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
	"bytes"
	"context"
	"crypto/ecdsa"
	"fmt"
	"io"
	"math/big"
	"net"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/rwdxchain/go-rwdxchaina/accounts/abi/bind"
	"github.com/rwdxchain/go-rwdxchaina/common"
	"github.com/rwdxchain/go-rwdxchaina/contracts/chequebook"
	"github.com/rwdxchain/go-rwdxchaina/contracts/ens"
	"github.com/rwdxchain/go-rwdxchaina/ethclient"
	"github.com/rwdxchain/go-rwdxchaina/metrics"
	"github.com/rwdxchain/go-rwdxchaina/p2p"
	"github.com/rwdxchain/go-rwdxchaina/p2p/discover"
	"github.com/rwdxchain/go-rwdxchaina/p2p/protocols"
	"github.com/rwdxchain/go-rwdxchaina/params"
	"github.com/rwdxchain/go-rwdxchaina/rpc"
	"github.com/rwdxchain/go-rwdxchaina/swarm/api"
	httpapi "github.com/rwdxchain/go-rwdxchaina/swarm/api/http"
	"github.com/rwdxchain/go-rwdxchaina/swarm/fuse"
	"github.com/rwdxchain/go-rwdxchaina/swarm/log"
	"github.com/rwdxchain/go-rwdxchaina/swarm/network"
	"github.com/rwdxchain/go-rwdxchaina/swarm/network/stream"
	"github.com/rwdxchain/go-rwdxchaina/swarm/pss"
	"github.com/rwdxchain/go-rwdxchaina/swarm/state"
	"github.com/rwdxchain/go-rwdxchaina/swarm/storage"
	"github.com/rwdxchain/go-rwdxchaina/swarm/storage/mock"
	"github.com/rwdxchain/go-rwdxchaina/swarm/storage/mru"
	"github.com/rwdxchain/go-rwdxchaina/swarm/tracing"
)

var (
	startTime          time.Time
	updateGaugesPeriod = 5 * time.Second
	startCounter       = metrics.NewRegisteredCounter("stack,start", nil)
	stopCounter        = metrics.NewRegisteredCounter("stack,stop", nil)
	uptimeGauge        = metrics.NewRegisteredGauge("stack.uptime", nil)
	requestsCacheGauge = metrics.NewRegisteredGauge("storage.cache.requests.size", nil)
)

// the swarm stack
type Swarm struct {
	config      *api.Config        // swarm configuration
	api         *api.API           // high level api layer (fs/manifest)
	dns         api.Resolver       // DNS registrar
	fileStore   *storage.FileStore // distributed preimage archive, the local API to the storage with document level storage/retrieval support
	streamer    *stream.Registry
	bzz         *network.Bzz       // the logistic manager
	backend     chequebook.Backend // simple blockchain Backend
	privateKey  *ecdsa.PrivateKey
	corsString  string
	swapEnabled bool
	lstore      *storage.LocalStore // local store, needs to store for releasing resources after node stopped
	sfs         *fuse.SwarmFS       // need this to cleanup all the active mounts on node exit
	ps          *pss.Pss

	tracerClose io.Closer
}

type SwarmAPI struct {
	Api     *api.API
	Backend chequebook.Backend
}

func (self *Swarm) API() *SwarmAPI {
	return &SwarmAPI{
		Api:     self.api,
		Backend: self.backend,
	}
}

// creates a new swarm service instance
// implements node.Service
// If mockStore is not nil, it will be used as the storage for chunk data.
// MockStore should be used only for testing.
func NewSwarm(config *api.Config, mockStore *mock.NodeStore) (self *Swarm, err error) {

	if bytes.Equal(common.FromHex(config.PublicKey), storage.ZeroAddr) {
		return nil, fmt.Errorf("empty public key")
	}
	if bytes.Equal(common.FromHex(config.BzzKey), storage.ZeroAddr) {
		return nil, fmt.Errorf("empty bzz key")
	}

	var backend chequebook.Backend
	if config.SwapAPI != "" && config.SwapEnabled {
		log.Info("connecting to SWAP API", "url", config.SwapAPI)
		backend, err = ethclient.Dial(config.SwapAPI)
		if err != nil {
			return nil, fmt.Errorf("error connecting to SWAP API %s: %s", config.SwapAPI, err)
		}
	}

	self = &Swarm{
		config:     config,
		backend:    backend,
		privateKey: config.ShiftPrivateKey(),
	}
	log.Debug("Setting up Swarm service components")

	config.HiveParams.Discovery = true

	nodeID, err := discover.HexID(config.NodeID)
	if err != nil {
		return nil, err
	}
	addr := &network.BzzAddr{
		OAddr: common.FromHex(config.BzzKey),
		UAddr: []byte(discover.NewNode(nodeID, net.IP{127, 0, 0, 1}, 33760, 33760).String()),
	}

	bzzconfig := &network.BzzConfig{
		NetworkID:    config.NetworkID,
		OverlayAddr:  addr.OAddr,
		UnderlayAddr: addr.UAddr,
		HiveParams:   config.HiveParams,
		LightNode:    config.LightNodeEnabled,
	}

	stateStore, err := state.NewDBStore(filepath.Join(config.Path, "state-store.db"))
	if err != nil {
		return
	}

	// set up high level api
	var resolver *api.MultiResolver
	if len(config.EnsAPIs) > 0 {
		opts := []api.MultiResolverOption{}
		for _, c := range config.EnsAPIs {
			tld, endpoint, addr := parseEnsAPIAddress(c)
			r, err := newEnsClient(endpoint, addr, config, self.privateKey)
			if err != nil {
				return nil, err
			}
			opts = append(opts, api.MultiResolverOptionWithResolver(r, tld))

		}
		resolver = api.NewMultiResolver(opts...)
		self.dns = resolver
	}

	self.lstore, err = storage.NewLocalStore(config.LocalStoreParams, mockStore)
	if err != nil {
		return
	}

	db := storage.NewDBAPI(self.lstore)
	to := network.NewKademlia(
		common.FromHex(config.BzzKey),
		network.NewKadParams(),
	)
	delivery := stream.NewDelivery(to, db)

	self.streamer = stream.NewRegistry(addr, delivery, db, stateStore, &stream.RegistryOptions{
		SkipCheck:       config.DeliverySkipCheck,
		DoSync:          config.SyncEnabled,
		DoRetrieve:      true,
		SyncUpdateDelay: config.SyncUpdateDelay,
	})

	// set up NetStore, the cloud storage local access layer
	netStore := storage.NewNetStore(self.lstore, self.streamer.Retrieve)
	// Swarm Hash Merklised Chunking for Arbitrary-length Document/File storage
	self.fileStore = storage.NewFileStore(netStore, self.config.FileStoreParams)

	var resourceHandler *mru.Handler
	rhparams := &mru.HandlerParams{}

	resourceHandler = mru.NewHandler(rhparams)
	resourceHandler.SetStore(netStore)

	self.lstore.Validators = []storage.ChunkValidator{
		storage.NewContentAddressValidator(storage.MakeHashFunc(storage.DefaultHash)),
		resourceHandler,
	}

	log.Debug("Setup local storage")

	self.bzz = network.NewBzz(bzzconfig, to, stateStore, stream.Spec, self.streamer.Run)

	// Pss = postal service over swarm (devp2p over bzz)
	self.ps, err = pss.NewPss(to, config.Pss)
	if err != nil {
		return nil, err
	}
	if pss.IsActiveHandshake {
		pss.SetHandshakeController(self.ps, pss.NewHandshakeParams())
	}

	self.api = api.NewAPI(self.fileStore, self.dns, resourceHandler, self.privateKey)

	self.sfs = fuse.NewSwarmFS(self.api)
	log.Debug("Initialized FUSE filesystem")

	return self, nil
}

// parseEnsAPIAddress parses string according to format
// [tld:][contract-addr@]url and returns ENSClientConfig structure
// with endpoint, contract address and TLD.
func parseEnsAPIAddress(s string) (tld, endpoint string, addr common.Address) {
	isAllLetterString := func(s string) bool {
		for _, r := range s {
			if !unicode.IsLetter(r) {
				return false
			}
		}
		return true
	}
	endpoint = s
	if i := strings.Index(endpoint, ":"); i > 0 {
		if isAllLetterString(endpoint[:i]) && len(endpoint) > i+2 && endpoint[i+1:i+3] != "//" {
			tld = endpoint[:i]
			endpoint = endpoint[i+1:]
		}
	}
	if i := strings.Index(endpoint, "@"); i > 0 {
		addr = common.HexToAddress(endpoint[:i])
		endpoint = endpoint[i+1:]
	}
	return
}

// ensClient provides functionality for api.ResolveValidator
type ensClient struct {
	*ens.ENS
	*ethclient.Client
}

// newEnsClient creates a new ENS client for that is a consumer of
// a ENS API on a specific endpoint. It is used as a helper function
// for creating multiple resolvers in NewSwarm function.
func newEnsClient(endpoint string, addr common.Address, config *api.Config, privkey *ecdsa.PrivateKey) (*ensClient, error) {
	log.Info("connecting to ENS API", "url", endpoint)
	client, err := rpc.Dial(endpoint)
	if err != nil {
		return nil, fmt.Errorf("error connecting to ENS API %s: %s", endpoint, err)
	}
	ethClient := ethclient.NewClient(client)

	ensRoot := config.EnsRoot
	if addr != (common.Address{}) {
		ensRoot = addr
	} else {
		a, err := detectEnsAddr(client)
		if err == nil {
			ensRoot = a
		} else {
			log.Warn(fmt.Sprintf("could not determine ENS contract address, using default %s", ensRoot), "err", err)
		}
	}
	transactOpts := bind.NewKeyedTransactor(privkey)
	dns, err := ens.NewENS(transactOpts, ensRoot, ethClient)
	if err != nil {
		return nil, err
	}
	log.Debug(fmt.Sprintf("-> Swarm Domain Name Registrar %v @ address %v", endpoint, ensRoot.Hex()))
	return &ensClient{
		ENS:    dns,
		Client: ethClient,
	}, err
}

// detectEnsAddr determines the ENS contract address by getting both the
// version and genesis hash using the client and matching them to either
// mainnet or testnet addresses
func detectEnsAddr(client *rpc.Client) (common.Address, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var version string
	if err := client.CallContext(ctx, &version, "net_version"); err != nil {
		return common.Address{}, err
	}

	block, err := ethclient.NewClient(client).BlockByNumber(ctx, big.NewInt(0))
	if err != nil {
		return common.Address{}, err
	}

	switch {

	case version == "1" && block.Hash() == params.MainnetGenesisHash:
		log.Info("using Mainnet ENS contract address", "addr", ens.MainNetAddress)
		return ens.MainNetAddress, nil

	case version == "3" && block.Hash() == params.TestnetGenesisHash:
		log.Info("using Testnet ENS contract address", "addr", ens.TestNetAddress)
		return ens.TestNetAddress, nil

	default:
		return common.Address{}, fmt.Errorf("unknown version and genesis hash: %s %s", version, block.Hash())
	}
}

/*
Start is called when the stack is started
* starts the network kademlia hive peer management
* (starts netStore level 0 api)
* starts DPA level 1 api (chunking -> store/retrieve requests)
* (starts level 2 api)
* starts http proxy server
* registers url scheme handlers for bzz, etc
* TODO: start subservices like sword, swear, swarmdns
*/
// implements the node.Service interface
func (self *Swarm) Start(srv *p2p.Server) error {
	startTime = time.Now()

	self.tracerClose = tracing.Closer

	// update uaddr to correct enode
	newaddr := self.bzz.UpdateLocalAddr([]byte(srv.Self().String()))
	log.Info("Updated bzz local addr", "oaddr", fmt.Sprintf("%x", newaddr.OAddr), "uaddr", fmt.Sprintf("%s", newaddr.UAddr))
	// set chequebook
	if self.config.SwapEnabled {
		ctx := context.Background() // The initial setup has no deadline.
		err := self.SetChequebook(ctx)
		if err != nil {
			return fmt.Errorf("Unable to set chequebook for SWAP: %v", err)
		}
		log.Debug(fmt.Sprintf("-> cheque book for SWAP: %v", self.config.Swap.Chequebook()))
	} else {
		log.Debug(fmt.Sprintf("SWAP disabled: no cheque book set"))
	}

	log.Info("Starting bzz service")

	err := self.bzz.Start(srv)
	if err != nil {
		log.Error("bzz failed", "err", err)
		return err
	}
	log.Info("Swarm network started", "bzzaddr", fmt.Sprintf("%x", self.bzz.Hive.Overlay.BaseAddr()))

	if self.ps != nil {
		self.ps.Start(srv)
	}

	// start swarm http proxy server
	if self.config.Port != "" {
		addr := net.JoinHostPort(self.config.ListenAddr, self.config.Port)
		server := httpapi.NewServer(self.api, self.config.Cors)

		if self.config.Cors != "" {
			log.Debug("Swarm HTTP proxy CORS headers", "allowedOrigins", self.config.Cors)
		}

		log.Debug("Starting Swarm HTTP proxy", "port", self.config.Port)
		go func() {
			err := server.ListenAndServe(addr)
			if err != nil {
				log.Error("Could not start Swarm HTTP proxy", "err", err.Error())
			}
		}()
	}

	self.periodicallyUpdateGauges()

	startCounter.Inc(1)
	self.streamer.Start(srv)
	return nil
}

func (self *Swarm) periodicallyUpdateGauges() {
	ticker := time.NewTicker(updateGaugesPeriod)

	go func() {
		for range ticker.C {
			self.updateGauges()
		}
	}()
}

func (self *Swarm) updateGauges() {
	uptimeGauge.Update(time.Since(startTime).Nanoseconds())
	requestsCacheGauge.Update(int64(self.lstore.RequestsCacheLen()))
}

// implements the node.Service interface
// stops all component services.
func (self *Swarm) Stop() error {
	if self.tracerClose != nil {
		err := self.tracerClose.Close()
		if err != nil {
			return err
		}
	}

	if self.ps != nil {
		self.ps.Stop()
	}
	if ch := self.config.Swap.Chequebook(); ch != nil {
		ch.Stop()
		ch.Save()
	}

	if self.lstore != nil {
		self.lstore.DbStore.Close()
	}
	self.sfs.Stop()
	stopCounter.Inc(1)
	self.streamer.Stop()
	return self.bzz.Stop()
}

// implements the node.Service interface
func (self *Swarm) Protocols() (protos []p2p.Protocol) {
	protos = append(protos, self.bzz.Protocols()...)

	if self.ps != nil {
		protos = append(protos, self.ps.Protocols()...)
	}
	return
}

func (self *Swarm) RegisterPssProtocol(spec *protocols.Spec, targetprotocol *p2p.Protocol, options *pss.ProtocolParams) (*pss.Protocol, error) {
	if !pss.IsActiveProtocol {
		return nil, fmt.Errorf("Pss protocols not available (built with !nopssprotocol tag)")
	}
	topic := pss.ProtocolTopic(spec)
	return pss.RegisterProtocol(self.ps, &topic, spec, targetprotocol, options)
}

// implements node.Service
// APIs returns the RPC API descriptors the Swarm implementation offers
func (self *Swarm) APIs() []rpc.API {

	apis := []rpc.API{
		// public APIs
		{
			Namespace: "bzz",
			Version:   "3.0",
			Service:   &Info{self.config, chequebook.ContractParams},
			Public:    true,
		},
		// admin APIs
		{
			Namespace: "bzz",
			Version:   "3.0",
			Service:   api.NewControl(self.api, self.bzz.Hive),
			Public:    false,
		},
		{
			Namespace: "chequebook",
			Version:   chequebook.Version,
			Service:   chequebook.NewApi(self.config.Swap.Chequebook),
			Public:    false,
		},
		{
			Namespace: "swarmfs",
			Version:   fuse.Swarmfs_Version,
			Service:   self.sfs,
			Public:    false,
		},
		// storage APIs
		// DEPRECATED: Use the HTTP API instead
		{
			Namespace: "bzz",
			Version:   "0.1",
			Service:   api.NewStorage(self.api),
			Public:    true,
		},
		{
			Namespace: "bzz",
			Version:   "0.1",
			Service:   api.NewFileSystem(self.api),
			Public:    false,
		},
		// {Namespace, Version, api.NewAdmin(self), false},
	}

	apis = append(apis, self.bzz.APIs()...)

	if self.ps != nil {
		apis = append(apis, self.ps.APIs()...)
	}

	return apis
}

func (self *Swarm) Api() *api.API {
	return self.api
}

// SetChequebook ensures that the local checquebook is set up on chain.
func (self *Swarm) SetChequebook(ctx context.Context) error {
	err := self.config.Swap.SetChequebook(ctx, self.backend, self.config.Path)
	if err != nil {
		return err
	}
	log.Info(fmt.Sprintf("new chequebook set (%v): saving config file, resetting all connections in the hive", self.config.Swap.Contract.Hex()))
	return nil
}

// serialisable info about swarm
type Info struct {
	*api.Config
	*chequebook.Params
}

func (self *Info) Info() *Info {
	return self
}
