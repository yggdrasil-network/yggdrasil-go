package yggdrasil

// import "io/ioutil"
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

func (c *Core) init(bpub *boxPubKey,
	bpriv *boxPrivKey,
	spub *sigPubKey,
	spriv *sigPrivKey) {
	// TODO separate init and start functions
	//  Init sets up structs
	//  Start launches goroutines that depend on structs being set up
	// This is pretty much required to completely avoid race conditions
	util_initByteStore()
	// c.log = log.New(ioutil.Discard, "", 0)
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

func (c *Core) Start(nc *config.NodeConfig, log *log.Logger) error {
	c.log = log
	c.log.Println("Starting up...")

	udp := udpInterface{}
	if err := udp.init(c, nc.Listen); err != nil {
		c.log.Println("Failed to start UDP interface")
		return err
	}
	c.udp = &udp

	tcp := tcpInterface{}
	if err := tcp.init(c, nc.Listen); err != nil {
		c.log.Println("Failed to start TCP interface")
		return err
	}
	c.tcp = &tcp

	var boxPub boxPubKey
	var boxPriv boxPrivKey
	var sigPub sigPubKey
	var sigPriv sigPrivKey
	copy(boxPub[:], nc.EncryptionPublicKey)
	copy(boxPriv[:], nc.EncryptionPrivateKey)
	copy(sigPub[:], nc.SigningPublicKey)
	copy(sigPriv[:], nc.SigningPrivateKey)

	c.init(&boxPub, &boxPriv, &sigPub, &sigPriv)
	c.admin.init(c, nc.AdminListen)

	if err := c.router.start(); err != nil {
		c.log.Println("Failed to start router")
		return err
	}

	if err := c.switchTable.start(); err != nil {
		c.log.Println("Failed to start switch table ticker")
		return err
	}

	if err := c.tun.setup(nc.IfName, nc.IfTAPMode, net.IP(c.router.addr[:]).String(), nc.IfMTU); err != nil {
		c.log.Println("Failed to start TUN/TAP")
		return err
	}

	if err := c.admin.start(); err != nil {
		c.log.Println("Failed to start admin socket")
		return err
	}

	if err := c.multicast.start(); err != nil {
		c.log.Println("Failed to start multicast interface")
		return err
	}

	c.log.Println("Startup complete")
	return nil
}

func (c *Core) Stop() {
	c.log.Println("Stopping...")
	c.tun.close()
	c.log.Println("Goodbye!")
}

func (c *Core) NewEncryptionKeys() (*boxPubKey, *boxPrivKey) {
	return newBoxKeys()
}

func (c *Core) NewSigningKeys() (*sigPubKey, *sigPrivKey) {
	return newSigKeys()
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

func (c *Core) SetLogger(log *log.Logger) {
	c.log = log
}

func (c *Core) AddPeer(addr string) error {
	return c.admin.addPeer(addr)
}

func (c *Core) AddMulticastInterfaceExpr(expr *regexp.Regexp) {
	c.ifceExpr = append(c.ifceExpr, expr)
}

func (c *Core) AddAllowedEncryptionPublicKey(boxStr string) error {
	return c.admin.addAllowedEncryptionPublicKey(boxStr)
}

func (c *Core) GetTUNDefaultIfName() string {
	return getDefaults().defaultIfName
}

func (c *Core) GetTUNDefaultIfMTU() int {
	return getDefaults().defaultIfMTU
}

func (c *Core) GetTUNMaximumIfMTU() int {
	return getDefaults().maximumIfMTU
}

func (c *Core) GetTUNDefaultIfTAPMode() bool {
	return getDefaults().defaultIfTAPMode
}

func (c *Core) GetTUNIfName() string {
	return c.tun.iface.Name()
}

func (c *Core) GetTUNIfMTU() int {
	return c.tun.mtu
}
