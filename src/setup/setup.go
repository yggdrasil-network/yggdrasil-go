package setup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strings"

	"github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/ipv6rwc"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
	"github.com/yggdrasil-network/yggdrasil-go/src/tun"
	"golang.org/x/text/encoding/unicode"
)

type Node struct {
	core      *core.Core
	ctx       context.Context
	cancel    context.CancelFunc
	logger    *log.Logger
	config    *config.NodeConfig
	tun       *tun.TunAdapter
	multicast *multicast.Multicast
	admin     *admin.AdminSocket
}

func NewNode(cfg *config.NodeConfig, logger *log.Logger) *Node {
	ctx, cancel := context.WithCancel(context.Background())
	return &Node{
		ctx:       ctx,
		cancel:    cancel,
		logger:    logger,
		config:    cfg,
		tun:       &tun.TunAdapter{},
		multicast: &multicast.Multicast{},
		admin:     &admin.AdminSocket{},
	}
}

func (n *Node) Close() {
	n.cancel()
	_ = n.admin.Stop()
	_ = n.multicast.Stop()
	_ = n.tun.Stop()
	_ = n.core.Close()
}

func (n *Node) Done() <-chan struct{} {
	return n.ctx.Done()
}

func (n *Node) Admin() *admin.AdminSocket {
	return n.admin
}

// The main function is responsible for configuring and starting Yggdrasil.
func (n *Node) Run(args Arguments) error {
	var err error = nil
	// Have we got a working configuration? If we don't then it probably means
	// that neither -autoconf, -useconf or -useconffile were set above. Stop
	// if we don't.
	if n.config == nil {
		return fmt.Errorf("no configuration supplied")
	}
	// Have we been asked for the node address yet? If so, print it and then stop.
	getNodeKey := func() ed25519.PublicKey {
		return ed25519.PrivateKey(n.config.PrivateKey).Public().(ed25519.PublicKey)
	}
	switch {
	case args.GetAddr:
		if key := getNodeKey(); key != nil {
			addr := address.AddrForKey(key)
			ip := net.IP(addr[:])
			fmt.Println(ip.String())
		}
		return nil
	case args.GetSubnet:
		if key := getNodeKey(); key != nil {
			snet := address.SubnetForKey(key)
			ipnet := net.IPNet{
				IP:   append(snet[:], 0, 0, 0, 0, 0, 0, 0, 0),
				Mask: net.CIDRMask(len(snet)*8, 128),
			}
			fmt.Println(ipnet.String())
		}
		return nil
	case args.GetPKey:
		if key := getNodeKey(); key != nil {
			fmt.Println(hex.EncodeToString(key))
		}
		return nil
	case args.ExportKey:
		pem, err := n.config.MarshalPEMPrivateKey()
		if err != nil {
			return err
		}
		fmt.Println(string(pem))
		return nil
	default:
	}

	// Setup the Yggdrasil node itself.
	{
		options := []core.SetupOption{
			core.NodeInfo(n.config.NodeInfo),
			core.NodeInfoPrivacy(n.config.NodeInfoPrivacy),
		}
		for _, addr := range n.config.Listen {
			options = append(options, core.ListenAddress(addr))
		}
		for _, peer := range n.config.Peers {
			options = append(options, core.Peer{URI: peer})
		}
		for intf, peers := range n.config.InterfacePeers {
			for _, peer := range peers {
				options = append(options, core.Peer{URI: peer, SourceInterface: intf})
			}
		}
		for _, allowed := range n.config.AllowedPublicKeys {
			k, err := hex.DecodeString(allowed)
			if err != nil {
				return err
			}
			options = append(options, core.AllowedPublicKey(k[:]))
		}
		if n.core, err = core.New(n.config.Certificate, n.logger, options...); err != nil {
			return err
		}
		address, subnet := n.core.Address(), n.core.Subnet()
		n.logger.Infof("Your public key is %s", hex.EncodeToString(n.core.PublicKey()))
		n.logger.Infof("Your IPv6 address is %s", address.String())
		n.logger.Infof("Your IPv6 subnet is %s", subnet.String())
	}

	// Setup the admin socket.
	{
		options := []admin.SetupOption{
			admin.ListenAddress(n.config.AdminListen),
		}
		if n.config.LogLookups {
			options = append(options, admin.LogLookups{})
		}
		if n.admin, err = admin.New(n.core, n.logger, options...); err != nil {
			return err
		}
		if n.admin != nil {
			n.admin.SetupAdminHandlers()
		}
	}

	// Setup the multicast module.
	{
		options := []multicast.SetupOption{}
		for _, intf := range n.config.MulticastInterfaces {
			options = append(options, multicast.MulticastInterface{
				Regex:    regexp.MustCompile(intf.Regex),
				Beacon:   intf.Beacon,
				Listen:   intf.Listen,
				Port:     intf.Port,
				Priority: uint8(intf.Priority),
				Password: intf.Password,
			})
		}
		if n.multicast, err = multicast.New(n.core, n.logger, options...); err != nil {
			return err
		}
		if n.admin != nil && n.multicast != nil {
			n.multicast.SetupAdminHandlers(n.admin)
		}
	}

	return nil
}

func (n *Node) SetLogLevel(loglevel string) {
	levels := [...]string{"error", "warn", "info", "debug", "trace"}
	loglevel = strings.ToLower(loglevel)

	contains := func() bool {
		for _, l := range levels {
			if l == loglevel {
				return true
			}
		}
		return false
	}

	if !contains() { // set default log level
		n.logger.Infoln("Loglevel parse failed. Set default level(info)")
		loglevel = "info"
	}

	for _, l := range levels {
		n.logger.EnableLevel(l)
		if l == loglevel {
			break
		}
	}
}

func (n *Node) SetupTun() error {
	var err error = nil

	options := []tun.SetupOption{
		tun.InterfaceName(n.config.IfName),
		tun.InterfaceMTU(n.config.IfMTU),
	}

	if n.tun, err = tun.New(ipv6rwc.NewReadWriteCloser(n.core), n.logger, options...); err != nil {
		return err
	}

	if n.admin != nil && n.tun != nil {
		n.tun.SetupAdminHandlers(n.admin)
	}

	return nil
}

func ReadConfig(log *log.Logger, useconf bool, useconffile string, normaliseconf bool) *config.NodeConfig {
	// Use a configuration file. If -useconf, the configuration will be read
	// from stdin. If -useconffile, the configuration will be read from the
	// filesystem.
	var conf []byte
	var err error
	if useconffile != "" {
		// Read the file from the filesystem
		conf, err = ioutil.ReadFile(useconffile)
	} else {
		// Read the file from stdin.
		conf, err = ioutil.ReadAll(os.Stdin)
	}
	if err != nil {
		panic(err)
	}
	// If there's a byte order mark - which Windows 10 is now incredibly fond of
	// throwing everywhere when it's converting things into UTF-16 for the hell
	// of it - remove it and decode back down into UTF-8. This is necessary
	// because hjson doesn't know what to do with UTF-16 and will panic
	if bytes.Equal(conf[0:2], []byte{0xFF, 0xFE}) ||
		bytes.Equal(conf[0:2], []byte{0xFE, 0xFF}) {
		utf := unicode.UTF16(unicode.BigEndian, unicode.UseBOM)
		decoder := utf.NewDecoder()
		conf, err = decoder.Bytes(conf)
		if err != nil {
			panic(err)
		}
	}
	return config.ReadConfig(conf)
}

type Arguments struct {
	GenConf       bool
	UseConf       bool
	NormaliseConf bool
	ExportKey     bool
	ConfJSON      bool
	AutoConf      bool
	Version       bool
	GetAddr       bool
	GetSubnet     bool
	GetPKey       bool
	UseConfFile   string
	LogTo         string
	LogLevel      string
}

func ParseArguments() Arguments {
	genconf := flag.Bool("genconf", false, "print a new config to stdout")
	useconf := flag.Bool("useconf", false, "read HJSON/JSON config from stdin")
	useconffile := flag.String("useconffile", "", "read HJSON/JSON config from specified file path")
	normaliseconf := flag.Bool("normaliseconf", false, "use in combination with either -useconf or -useconffile, outputs your configuration normalised")
	exportkey := flag.Bool("exportkey", false, "use in combination with either -useconf or -useconffile, outputs your private key in PEM format")
	confjson := flag.Bool("json", false, "print configuration from -genconf or -normaliseconf as JSON instead of HJSON")
	autoconf := flag.Bool("autoconf", false, "automatic mode (dynamic IP, peer with IPv6 neighbors)")
	ver := flag.Bool("version", false, "prints the version of this build")
	logto := flag.String("logto", "stdout", "file path to log to, \"syslog\" or \"stdout\"")
	getaddr := flag.Bool("address", false, "returns the IPv6 address as derived from the supplied configuration")
	getsnet := flag.Bool("subnet", false, "returns the IPv6 subnet as derived from the supplied configuration")
	getpkey := flag.Bool("publickey", false, "use in combination with either -useconf or -useconffile, outputs your public key")
	loglevel := flag.String("loglevel", "info", "loglevel to enable")
	flag.Parse()
	return Arguments{
		GenConf:       *genconf,
		UseConf:       *useconf,
		UseConfFile:   *useconffile,
		NormaliseConf: *normaliseconf,
		ExportKey:     *exportkey,
		ConfJSON:      *confjson,
		AutoConf:      *autoconf,
		Version:       *ver,
		LogTo:         *logto,
		GetAddr:       *getaddr,
		GetSubnet:     *getsnet,
		GetPKey:       *getpkey,
		LogLevel:      *loglevel,
	}
}
