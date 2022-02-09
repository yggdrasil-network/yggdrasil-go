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
	"strings"

	"github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
	"golang.org/x/text/encoding/unicode"
)

type Node struct {
	core.Core
	ctx       context.Context
	cancel    context.CancelFunc
	logger    *log.Logger
	config    *config.NodeConfig
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
		multicast: &multicast.Multicast{},
		admin:     &admin.AdminSocket{},
	}
}

func (n *Node) Close() {
	n.cancel()
	_ = n.multicast.Stop()
	_ = n.admin.Stop()
	_ = n.Core.Close()
}

func (n *Node) Done() <-chan struct{} {
	return n.ctx.Done()
}

func (n *Node) Admin() *admin.AdminSocket {
	return n.admin
}

// The main function is responsible for configuring and starting Yggdrasil.
func (n *Node) Run(args Arguments) error {
	// Have we got a working configuration? If we don't then it probably means
	// that neither -autoconf, -useconf or -useconffile were set above. Stop
	// if we don't.
	if n.config == nil {
		return fmt.Errorf("no configuration supplied")
	}
	// Have we been asked for the node address yet? If so, print it and then stop.
	getNodeKey := func() ed25519.PublicKey {
		if pubkey, err := hex.DecodeString(n.config.PrivateKey); err == nil {
			return ed25519.PrivateKey(pubkey).Public().(ed25519.PublicKey)
		}
		return nil
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
	default:
	}

	// Now start Yggdrasil - this starts the DHT, router, switch and other core
	// components needed for Yggdrasil to operate
	if err := n.Core.Start(n.config, n.logger); err != nil {
		return fmt.Errorf("n.core.Start: %w", err)
	}
	// Register the session firewall gatekeeper function
	// Allocate our modules

	// Start the admin socket
	n.admin = &admin.AdminSocket{}
	if err := n.admin.Init(&n.Core, n.config, n.logger, nil); err != nil {
		return fmt.Errorf("n.admin.Init: %w", err)
	} else if err := n.admin.Start(); err != nil {
		return fmt.Errorf("n.admin.Start: %w", err)
	}
	n.admin.SetupAdminHandlers(n.admin)

	// Start the multicast interface
	if err := n.multicast.Init(&n.Core, n.config, n.logger, nil); err != nil {
		return fmt.Errorf("n.multicast.Init: %w", err)
	} else if err := n.multicast.Start(); err != nil {
		return fmt.Errorf("n.admin.Start: %w", err)
	}
	n.multicast.SetupAdminHandlers(n.admin)

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
	ConfJSON      bool
	AutoConf      bool
	Version       bool
	GetAddr       bool
	GetSubnet     bool
	UseConfFile   string
	LogTo         string
	LogLevel      string
}

func ParseArguments() Arguments {
	genconf := flag.Bool("genconf", false, "print a new config to stdout")
	useconf := flag.Bool("useconf", false, "read HJSON/JSON config from stdin")
	useconffile := flag.String("useconffile", "", "read HJSON/JSON config from specified file path")
	normaliseconf := flag.Bool("normaliseconf", false, "use in combination with either -useconf or -useconffile, outputs your configuration normalised")
	confjson := flag.Bool("json", false, "print configuration from -genconf or -normaliseconf as JSON instead of HJSON")
	autoconf := flag.Bool("autoconf", false, "automatic mode (dynamic IP, peer with IPv6 neighbors)")
	ver := flag.Bool("version", false, "prints the version of this build")
	logto := flag.String("logto", "stdout", "file path to log to, \"syslog\" or \"stdout\"")
	getaddr := flag.Bool("address", false, "returns the IPv6 address as derived from the supplied configuration")
	getsnet := flag.Bool("subnet", false, "returns the IPv6 subnet as derived from the supplied configuration")
	loglevel := flag.String("loglevel", "info", "loglevel to enable")
	flag.Parse()
	return Arguments{
		GenConf:       *genconf,
		UseConf:       *useconf,
		UseConfFile:   *useconffile,
		NormaliseConf: *normaliseconf,
		ConfJSON:      *confjson,
		AutoConf:      *autoconf,
		Version:       *ver,
		LogTo:         *logto,
		GetAddr:       *getaddr,
		GetSubnet:     *getsnet,
		LogLevel:      *loglevel,
	}
}
