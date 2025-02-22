package notify

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/rwdxchain/go-rwdxchaina/common/hexutil"
	"github.com/rwdxchain/go-rwdxchaina/crypto"
	"github.com/rwdxchain/go-rwdxchaina/log"
	"github.com/rwdxchain/go-rwdxchaina/node"
	"github.com/rwdxchain/go-rwdxchaina/p2p/discover"
	"github.com/rwdxchain/go-rwdxchaina/p2p/simulations"
	"github.com/rwdxchain/go-rwdxchaina/p2p/simulations/adapters"
	"github.com/rwdxchain/go-rwdxchaina/swarm/network"
	"github.com/rwdxchain/go-rwdxchaina/swarm/pss"
	"github.com/rwdxchain/go-rwdxchaina/swarm/state"
	whisper "github.com/rwdxchain/go-rwdxchaina/whisper/whisperv5"
)

var (
	loglevel = flag.Int("l", 3, "loglevel")
	psses    map[string]*pss.Pss
	w        *whisper.Whisper
	wapi     *whisper.PublicWhisperAPI
)

func init() {
	flag.Parse()
	hs := log.StreamHandler(os.Stderr, log.TerminalFormat(true))
	hf := log.LvlFilterHandler(log.Lvl(*loglevel), hs)
	h := log.CallerFileHandler(hf)
	log.Root().SetHandler(h)

	w = whisper.New(&whisper.DefaultConfig)
	wapi = whisper.NewPublicWhisperAPI(w)
	psses = make(map[string]*pss.Pss)
}

// Creates a client node and notifier node
// Client sends pss notifications requests
// notifier sends initial notification with symmetric key, and
// second notification symmetrically encrypted
func TestStart(t *testing.T) {
	adapter := adapters.NewSimAdapter(newServices(false))
	net := simulations.NewNetwork(adapter, &simulations.NetworkConfig{
		ID:             "0",
		DefaultService: "bzz",
	})
	leftNodeConf := adapters.RandomNodeConfig()
	leftNodeConf.Services = []string{"bzz", "pss"}
	leftNode, err := net.NewNodeWithConfig(leftNodeConf)
	if err != nil {
		t.Fatal(err)
	}
	err = net.Start(leftNode.ID())
	if err != nil {
		t.Fatal(err)
	}

	rightNodeConf := adapters.RandomNodeConfig()
	rightNodeConf.Services = []string{"bzz", "pss"}
	rightNode, err := net.NewNodeWithConfig(rightNodeConf)
	if err != nil {
		t.Fatal(err)
	}
	err = net.Start(rightNode.ID())
	if err != nil {
		t.Fatal(err)
	}

	err = net.Connect(rightNode.ID(), leftNode.ID())
	if err != nil {
		t.Fatal(err)
	}

	leftRpc, err := leftNode.Client()
	if err != nil {
		t.Fatal(err)
	}

	rightRpc, err := rightNode.Client()
	if err != nil {
		t.Fatal(err)
	}

	var leftAddr string
	err = leftRpc.Call(&leftAddr, "pss_baseAddr")
	if err != nil {
		t.Fatal(err)
	}

	var rightAddr string
	err = rightRpc.Call(&rightAddr, "pss_baseAddr")
	if err != nil {
		t.Fatal(err)
	}

	var leftPub string
	err = leftRpc.Call(&leftPub, "pss_getPublicKey")
	if err != nil {
		t.Fatal(err)
	}

	var rightPub string
	err = rightRpc.Call(&rightPub, "pss_getPublicKey")
	if err != nil {
		t.Fatal(err)
	}

	rsrcName := "foo.eth"
	rsrcTopic := pss.BytesToTopic([]byte(rsrcName))

	// wait for kademlia table to populate
	time.Sleep(time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	rmsgC := make(chan *pss.APIMsg)
	rightSub, err := rightRpc.Subscribe(ctx, "pss", rmsgC, "receive", controlTopic)
	if err != nil {
		t.Fatal(err)
	}
	defer rightSub.Unsubscribe()

	updateC := make(chan []byte)
	updateMsg := []byte{}
	ctrlClient := NewController(psses[rightPub])
	ctrlNotifier := NewController(psses[leftPub])
	ctrlNotifier.NewNotifier("foo.eth", 2, updateC)

	pubkeybytes, err := hexutil.Decode(leftPub)
	if err != nil {
		t.Fatal(err)
	}
	pubkey, err := crypto.UnmarshalPubkey(pubkeybytes)
	if err != nil {
		t.Fatal(err)
	}
	addrbytes, err := hexutil.Decode(leftAddr)
	if err != nil {
		t.Fatal(err)
	}
	ctrlClient.Subscribe(rsrcName, pubkey, addrbytes, func(s string, b []byte) error {
		if s != "foo.eth" || !bytes.Equal(updateMsg, b) {
			t.Fatalf("unexpected result in client handler: '%s':'%x'", s, b)
		}
		log.Info("client handler receive", "s", s, "b", b)
		return nil
	})

	var inMsg *pss.APIMsg
	select {
	case inMsg = <-rmsgC:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	dMsg, err := NewMsgFromPayload(inMsg.Msg)
	if err != nil {
		t.Fatal(err)
	}
	if dMsg.namestring != rsrcName {
		t.Fatalf("expected name '%s', got '%s'", rsrcName, dMsg.namestring)
	}
	if !bytes.Equal(dMsg.Payload[:len(updateMsg)], updateMsg) {
		t.Fatalf("expected payload first %d bytes '%x', got '%x'", len(updateMsg), updateMsg, dMsg.Payload[:len(updateMsg)])
	}
	if len(updateMsg)+symKeyLength != len(dMsg.Payload) {
		t.Fatalf("expected payload length %d, have %d", len(updateMsg)+symKeyLength, len(dMsg.Payload))
	}

	rightSubUpdate, err := rightRpc.Subscribe(ctx, "pss", rmsgC, "receive", rsrcTopic)
	if err != nil {
		t.Fatal(err)
	}
	defer rightSubUpdate.Unsubscribe()

	updateMsg = []byte("plugh")
	updateC <- updateMsg
	select {
	case inMsg = <-rmsgC:
	case <-ctx.Done():
		log.Error("timed out waiting for msg", "topic", fmt.Sprintf("%x", rsrcTopic))
		t.Fatal(ctx.Err())
	}
	dMsg, err = NewMsgFromPayload(inMsg.Msg)
	if err != nil {
		t.Fatal(err)
	}
	if dMsg.namestring != rsrcName {
		t.Fatalf("expected name %s, got %s", rsrcName, dMsg.namestring)
	}
	if !bytes.Equal(dMsg.Payload, updateMsg) {
		t.Fatalf("expected payload '%x', got '%x'", updateMsg, dMsg.Payload)
	}

}

func newServices(allowRaw bool) adapters.Services {
	stateStore := state.NewInmemoryStore()
	kademlias := make(map[discover.NodeID]*network.Kademlia)
	kademlia := func(id discover.NodeID) *network.Kademlia {
		if k, ok := kademlias[id]; ok {
			return k
		}
		addr := network.NewAddrFromNodeID(id)
		params := network.NewKadParams()
		params.MinProxBinSize = 2
		params.MaxBinSize = 3
		params.MinBinSize = 1
		params.MaxRetries = 1000
		params.RetryExponent = 2
		params.RetryInterval = 1000000
		kademlias[id] = network.NewKademlia(addr.Over(), params)
		return kademlias[id]
	}
	return adapters.Services{
		"pss": func(ctx *adapters.ServiceContext) (node.Service, error) {
			ctxlocal, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			keys, err := wapi.NewKeyPair(ctxlocal)
			privkey, err := w.GetPrivateKey(keys)
			pssp := pss.NewPssParams().WithPrivateKey(privkey)
			pssp.MsgTTL = time.Second * 30
			pssp.AllowRaw = allowRaw
			pskad := kademlia(ctx.Config.ID)
			ps, err := pss.NewPss(pskad, pssp)
			if err != nil {
				return nil, err
			}
			//psses[common.ToHex(crypto.FromECDSAPub(&privkey.PublicKey))] = ps
			psses[hexutil.Encode(crypto.FromECDSAPub(&privkey.PublicKey))] = ps
			return ps, nil
		},
		"bzz": func(ctx *adapters.ServiceContext) (node.Service, error) {
			addr := network.NewAddrFromNodeID(ctx.Config.ID)
			hp := network.NewHiveParams()
			hp.Discovery = false
			config := &network.BzzConfig{
				OverlayAddr:  addr.Over(),
				UnderlayAddr: addr.Under(),
				HiveParams:   hp,
			}
			return network.NewBzz(config, kademlia(ctx.Config.ID), stateStore, nil, nil), nil
		},
	}
}
