// pss send-to-self hello world
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contracts/chequebook"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	//"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/swarm"
	bzzapi "github.com/ethereum/go-ethereum/swarm/api"
	"github.com/ethereum/go-ethereum/swarm/pss"
	demo "github.com/nolash/go-ethereum-p2p-demo/common"
)

func init() {
	log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
}

func newService(bzzdir string, bzzport int, bzznetworkid uint64) func(ctx *node.ServiceContext) (node.Service, error) {
	return func(ctx *node.ServiceContext) (node.Service, error) {

		// generate a new private key
		privkey, err := crypto.GenerateKey()
		if err != nil {
			demo.Log.Crit("private key generate servicenode 'left' fail: %v")
		}

		// create necessary swarm params
		var ensApi chequebook.Backend = nil
		swapEnabled := false
		syncEnabled := false
		pssEnabled := true
		cors := "*"
		checkbookaddr := crypto.PubkeyToAddress(privkey.PublicKey)
		bzzconfig, err := bzzapi.NewConfig(bzzdir, checkbookaddr, privkey, bzznetworkid)
		if err != nil {
			demo.Log.Crit("unable to configure swarm", "err", err)
		}
		bzzconfig.Port = fmt.Sprintf("%d", bzzport)

		return swarm.NewSwarm(ctx, ensApi, bzzconfig, swapEnabled, syncEnabled, cors, pssEnabled)
	}
}

func main() {

	// create three nodes
	l_stack, err := demo.NewServiceNode(demo.P2PDefaultPort, 0, 0)
	if err != nil {
		demo.Log.Crit(err.Error())
	}
	r_stack, err := demo.NewServiceNode(demo.P2PDefaultPort+1, 0, 0)
	if err != nil {
		demo.Log.Crit(err.Error())
	}
	c_stack, err := demo.NewServiceNode(demo.P2PDefaultPort+2, 0, 0)
	if err != nil {
		demo.Log.Crit(err.Error())
	}

	// register the pss activated bzz services
	l_svc := newService(l_stack.InstanceDir(), demo.BzzDefaultPort, demo.BzzDefaultNetworkId)
	err = l_stack.Register(l_svc)
	if err != nil {
		demo.Log.Crit("servicenode 'left' pss register fail", "err", err)
	}
	r_svc := newService(r_stack.InstanceDir(), demo.BzzDefaultPort+1, demo.BzzDefaultNetworkId)
	err = r_stack.Register(r_svc)
	if err != nil {
		demo.Log.Crit("servicenode 'right' pss register fail", "err", err)
	}
	c_svc := newService(c_stack.InstanceDir(), demo.BzzDefaultPort+2, demo.BzzDefaultNetworkId)
	err = c_stack.Register(c_svc)
	if err != nil {
		demo.Log.Crit("servicenode 'right' pss register fail", "err", err)
	}

	// start the nodes
	err = l_stack.Start()
	if err != nil {
		demo.Log.Crit("servicenode start failed", "err", err)
	}
	err = r_stack.Start()
	if err != nil {
		demo.Log.Crit("servicenode start failed", "err", err)
	}
	err = c_stack.Start()
	if err != nil {
		demo.Log.Crit("servicenode start failed", "err", err)
	}

	// connect the nodes to the middle
	c_stack.Server().AddPeer(l_stack.Server().Self())
	c_stack.Server().AddPeer(r_stack.Server().Self())

	// get the rpc clients
	l_rpcclient, err := l_stack.Attach()
	r_rpcclient, err := r_stack.Attach()

	// wait until the state of the swarm overlay network is ready
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = demo.WaitHealthy(ctx, 2, l_rpcclient, r_rpcclient)
	if err != nil {
		demo.Log.Crit("health check fail", "err", err)
	}
	time.Sleep(time.Second) // because the healthy does not work

	// get a valid topic byte
	var topic pss.Topic
	err = l_rpcclient.Call(&topic, "pss_stringToTopic", "foo")
	if err != nil {
		demo.Log.Crit("pss string to topic fail", "err", err)
	}

	// subscribe to incoming messages on both servicenodes
	// this will register message handlers, needed to receive reciprocal comms
	l_msgC := make(chan pss.APIMsg)
	l_sub_pss, err := l_rpcclient.Subscribe(context.Background(), "pss", l_msgC, "receive", topic)
	if err != nil {
		demo.Log.Crit("pss subscribe error", "err", err)
	}
	r_msgC := make(chan pss.APIMsg)
	r_sub_pss, err := r_rpcclient.Subscribe(context.Background(), "pss", r_msgC, "receive", topic)
	if err != nil {
		demo.Log.Crit("pss subscribe error", "err", err)
	}

	// get the public keys
	var l_pubkey []byte
	err = l_rpcclient.Call(&l_pubkey, "pss_getPublicKey")
	if err != nil {
		demo.Log.Crit("pss get pubkey fail", "err", err)
	}
	var r_pubkey []byte
	err = r_rpcclient.Call(&r_pubkey, "pss_getPublicKey")
	if err != nil {
		demo.Log.Crit("pss get pubkey fail", "err", err)
	}

	// get the overlay addresses
	var l_bzzaddr []byte
	err = l_rpcclient.Call(&l_bzzaddr, "pss_baseAddr")
	if err != nil {
		demo.Log.Crit("pss get baseaddr fail", "err", err)
	}
	var r_bzzaddr []byte
	err = r_rpcclient.Call(&r_bzzaddr, "pss_baseAddr")
	if err != nil {
		demo.Log.Crit("pss get baseaddr fail", "err", err)
	}

	// make the nodes aware of each others' public keys
	err = l_rpcclient.Call(nil, "pss_setPeerPublicKey", r_pubkey, topic, r_bzzaddr)
	if err != nil {
		demo.Log.Crit("pss set pubkey fail", "err", err)
	}
	err = r_rpcclient.Call(nil, "pss_setPeerPublicKey", l_pubkey, topic, l_bzzaddr)
	if err != nil {
		demo.Log.Crit("pss set pubkey fail", "err", err)
	}

	// activate handshake on both sides
	err = l_rpcclient.Call(nil, "pss_addHandshake", topic)
	if err != nil {
		demo.Log.Crit("pss handshake activate fail", "err", err)
	}
	err = r_rpcclient.Call(nil, "pss_addHandshake", topic)
	if err != nil {
		demo.Log.Crit("pss handshake activate fail", "err", err)
	}

	// we need this for the send api call
	pubkeyid := common.ToHex(r_pubkey)

	// initiate handshake and retrieve symkeys
	var symkeyids []string
	err = l_rpcclient.Call(&symkeyids, "pss_handshake", pubkeyid, topic, true, true)
	if err != nil {
		demo.Log.Crit("handshake fail", "err", err)
	}

	// convert the pubkey to hex string
	// send message using asymmetric encryption
	err = l_rpcclient.Call(nil, "pss_sendSym", symkeyids[0], topic, []byte("bar"))
	if err != nil {
		demo.Log.Crit("pss send fail", "err", err)
	}

	// get the incoming message
	for ; ; inmsg := <-r_msgC {
		if !inmsg.Asymmetric {
			demo.Log.Info("pss received", "msg", fmt.Sprintf("%s", inmsg.Msg), "from", fmt.Sprintf("%x", inmsg.Key))
			break
		}
	}

	// bring down the servicenodes
	l_sub_pss.Unsubscribe()
	r_sub_pss.Unsubscribe()
	r_rpcclient.Close()
	l_rpcclient.Close()
	c_stack.Stop()
	r_stack.Stop()
	l_stack.Stop()
}
