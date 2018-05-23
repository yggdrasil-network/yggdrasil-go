package yggdrasil

import "io/ioutil"
import "log"
import "regexp"
import "net"
import "yggdrasil/config"

type Core struct {
	// This is the main data structure that holds everything else for a node
	boxPub      boxPubKey
	boxPriv     boxPrivKey
	sigPub      sigPubKey
	sigPriv     sigPrivKey
	switchTable switchTable
	peers       peers
	sigs        sigManager
	sessions    sessions
	router      router
	dht         dht
	tun         tunDevice
	admin       admin
	searches    searches
	multicast   multicast
	tcp         *tcpInterface
	udp         *udpInterface
	log         *log.Logger
	ifceExpr    []*regexp.Regexp // the zone of link-local IPv6 peers must match this
}

func (c *Core) Init() {
	// Only called by the simulator, to set up nodes with random keys
	bpub, bpriv := newBoxKeys()
	spub, spriv := newSigKeys()
	c.init(bpub, bpriv, spub, spriv)
}

func (c *Core) Run(nc *config.NodeConfig) {
	var boxPub boxPubKey
   	var boxPriv boxPrivKey
   	var sigPub sigPubKey
   	var sigPriv sigPrivKey
   	copy(boxPub[:], nc.EncryptionPublicKey)
   	copy(boxPriv[:], nc.EncryptionPrivateKey)
   	copy(sigPub[:], nc.SigningPublicKey)
   	copy(sigPriv[:], nc.SigningPrivateKey)

   	c.init(&boxPub, &boxPriv, &sigPub, &sigPriv)

	c.udp.init(c, nc.Listen)
	c.tcp.init(c, nc.Listen)
	c.admin.init(c, nc.AdminListen)

	c.multicast.start()
	c.router.start()
	c.switchTable.start()
	c.tun.setup(nc.IfName, nc.IfTAPMode, net.IP(c.router.addr[:]).String(), nc.IfMTU)
	c.admin.start()
}

func (c *Core) init(bpub *boxPubKey,
	bpriv *boxPrivKey,
	spub *sigPubKey,
	spriv *sigPrivKey) {
	// TODO separate init and start functions
	//  Init sets up structs
	//  Start launches goroutines that depend on structs being set up
	// This is pretty much required to completely avoid race conditions
	util_initByteStore()
	c.log = log.New(ioutil.Discard, "", 0)
	c.boxPub, c.boxPriv = *bpub, *bpriv
	c.sigPub, c.sigPriv = *spub, *spriv
	c.admin.core = c
	c.sigs.init()
	c.searches.init(c)
	c.dht.init(c)
	c.sessions.init(c)
	c.multicast.init(c)
	c.peers.init(c)
	c.router.init(c)
	c.switchTable.init(c, c.sigPub) // TODO move before peers? before router?
	c.tun.init(c)
}

func (c *Core) GetNodeID() *NodeID {
	return getNodeID(&c.boxPub)
}

func (c *Core) GetTreeID() *TreeID {
	return getTreeID(&c.sigPub)
}

func (c *Core) GetAddress() *address {
	return address_addrForNodeID(c.GetNodeID())
}

func (c *Core) GetSubnet() *subnet {
	return address_subnetForNodeID(c.GetNodeID())
}
