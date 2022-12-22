package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/gologme/log"
	gsyslog "github.com/hashicorp/go-syslog"
	"github.com/hjson/hjson-go"
	"github.com/kardianos/minwinsvc"

	//"github.com/RiV-chain/RiV-mesh/src/address"
	"github.com/RiV-chain/RiV-mesh/src/admin"
	"github.com/RiV-chain/RiV-mesh/src/config"
	"github.com/RiV-chain/RiV-mesh/src/defaults"

	"github.com/RiV-chain/RiV-mesh/src/core"
	//"github.com/RiV-chain/RiV-mesh/src/ipv6rwc"
	"github.com/RiV-chain/RiV-mesh/src/multicast"
	"github.com/RiV-chain/RiV-mesh/src/restapi"
	"github.com/RiV-chain/RiV-mesh/src/tun"
	"github.com/RiV-chain/RiV-mesh/src/version"
)

type node struct {
	core        *core.Core
	tun         *tun.TunAdapter
	multicast   *multicast.Multicast
	admin       *admin.AdminSocket
	rest_server *restapi.RestServer
}

func setLogLevel(loglevel string, logger *log.Logger) {
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
		logger.Infoln("Loglevel parse failed. Set default level(info)")
		loglevel = "info"
	}

	for _, l := range levels {
		logger.EnableLevel(l)
		if l == loglevel {
			break
		}
	}
}

type yggArgs struct {
	genconf       bool
	useconf       bool
	normaliseconf bool
	confjson      bool
	autoconf      bool
	ver           bool
	getaddr       bool
	getsnet       bool
	useconffile   string
	logto         string
	loglevel      string
	httpaddress   string
	wwwroot       string
}

func getArgs() yggArgs {
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
	httpaddress := flag.String("httpaddress", "", "httpaddress to enable")
	wwwroot := flag.String("wwwroot", "", "wwwroot to enable")

	flag.Parse()
	return yggArgs{
		genconf:       *genconf,
		useconf:       *useconf,
		useconffile:   *useconffile,
		normaliseconf: *normaliseconf,
		confjson:      *confjson,
		autoconf:      *autoconf,
		ver:           *ver,
		logto:         *logto,
		getaddr:       *getaddr,
		getsnet:       *getsnet,
		loglevel:      *loglevel,
		httpaddress:   *httpaddress,
		wwwroot:       *wwwroot,
	}
}

func run(args yggArgs, ctx context.Context) {
	// Create a new logger that logs output to stdout.
	var logger *log.Logger
	switch args.logto {
	case "stdout":
		logger = log.New(os.Stdout, "", log.Flags())
	case "syslog":
		if syslogger, err := gsyslog.NewLogger(gsyslog.LOG_NOTICE, "DAEMON", version.BuildName()); err == nil {
			logger = log.New(syslogger, "", log.Flags())
		}
	default:
		if logfd, err := os.OpenFile(args.logto, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			logger = log.New(logfd, "", log.Flags())
		}
	}
	if logger == nil {
		logger = log.New(os.Stdout, "", log.Flags())
		logger.Warnln("Logging defaulting to stdout")
	}

	if args.normaliseconf {
		setLogLevel("error", logger)
	} else {
		setLogLevel(args.loglevel, logger)
	}

	var cfg *config.NodeConfig
	var err error
	switch {
	case args.ver:
		fmt.Println("Build name:", version.BuildName())
		fmt.Println("Build version:", version.BuildVersion())
		return
	case args.autoconf:
		// Use an autoconf-generated config, this will give us random keys and
		// port numbers, and will use an automatically selected TUN interface.
		cfg = defaults.GenerateConfig()
	case args.useconffile != "" || args.useconf:
		// Read the configuration from either stdin or from the filesystem
		cfg, err = defaults.ReadConfig(args.useconffile)
		if err != nil {
			panic("Configuration file load error: " + err.Error())
		}
		// If the -normaliseconf option was specified then remarshal the above
		// configuration and print it back to stdout. This lets the user update
		// their configuration file with newly mapped names (like above) or to
		// convert from plain JSON to commented HJSON.
		if args.normaliseconf {
			var bs []byte
			if args.confjson {
				bs, err = json.MarshalIndent(cfg, "", "  ")
			} else {
				bs, err = hjson.Marshal(cfg)
			}
			if err != nil {
				panic(err)
			}
			fmt.Println(string(bs))
			return
		}
	case args.genconf:
		// Generate a new configuration and print it to stdout.
		fmt.Println(defaults.Genconf(args.confjson))
		return
	default:
		// No flags were provided, therefore print the list of flags to stdout.
		fmt.Println("Usage:")
		flag.PrintDefaults()

		if args.getaddr || args.getsnet {
			fmt.Println("\nError: You need to specify some config data using -useconf or -useconffile.")
		}
	}
	// Have we got a working configuration? If we don't then it probably means
	// that neither -autoconf, -useconf or -useconffile were set above. Stop
	// if we don't.
	if cfg == nil {
		return
	}
	n := &node{}
	// Have we been asked for the node address yet? If so, print it and then stop.
	getNodeKey := func() ed25519.PublicKey {
		if pubkey, err := hex.DecodeString(cfg.PrivateKey); err == nil {
			return ed25519.PrivateKey(pubkey).Public().(ed25519.PublicKey)
		}
		return nil
	}
	switch {
	case args.getaddr:
		if key := getNodeKey(); key != nil {
			addr := n.core.AddrForKey(key)
			ip := net.IP(addr[:])
			fmt.Println(ip.String())
		}
		return
	case args.getsnet:
		if key := getNodeKey(); key != nil {
			snet := n.core.SubnetForKey(key)
			ipnet := net.IPNet{
				IP:   append(snet[:], 0, 0, 0, 0, 0, 0, 0, 0),
				Mask: net.CIDRMask(len(snet)*8, 128),
			}
			fmt.Println(ipnet.String())
		}
		return
	}
	//override httpaddress and wwwroot parameters in cfg
	if len(cfg.HttpAddress) == 0 {
		cfg.HttpAddress = args.httpaddress
	}
	if len(cfg.WwwRoot) == 0 {
		cfg.WwwRoot = args.wwwroot
	}

	// Setup the RiV-mesh node itself.
	{
		sk, err := hex.DecodeString(cfg.PrivateKey)
		if err != nil {
			panic(err)
		}
		options := []core.SetupOption{
			core.NodeInfo(cfg.NodeInfo),
			core.NodeInfoPrivacy(cfg.NodeInfoPrivacy),
			core.NetworkDomain(cfg.NetworkDomain),
		}
		for _, addr := range cfg.Listen {
			options = append(options, core.ListenAddress(addr))
		}
		for _, peer := range cfg.Peers {
			options = append(options, core.Peer{URI: peer})
		}
		for intf, peers := range cfg.InterfacePeers {
			for _, peer := range peers {
				options = append(options, core.Peer{URI: peer, SourceInterface: intf})
			}
		}
		for _, allowed := range cfg.AllowedPublicKeys {
			k, err := hex.DecodeString(allowed)
			if err != nil {
				panic(err)
			}
			options = append(options, core.AllowedPublicKey(k[:]))
		}
		if n.core, err = core.New(sk[:], logger, options...); err != nil {
			panic(err)
		}
	}

	// Setup the REST socket.
	{
		if n.rest_server, err = restapi.NewRestServer(restapi.RestServerCfg{
			Core:          n.core,
			Log:           logger,
			ListenAddress: cfg.HttpAddress,
			WwwRoot:       cfg.WwwRoot,
			ConfigFn:      args.useconffile,
		}); err != nil {
			logger.Errorln(err)
		} else {
			err = n.rest_server.Serve()
		}
	}

	// Setup the admin socket.
	{
		options := []admin.SetupOption{
			admin.ListenAddress(cfg.AdminListen),
		}
		if n.admin, err = admin.New(n.core, logger, options...); err != nil {
			panic(err)
		}
		if n.admin != nil {
			n.admin.SetupAdminHandlers()
		}
	}

	// Setup the multicast module.
	{
		options := []multicast.SetupOption{}
		for _, intf := range cfg.MulticastInterfaces {
			options = append(options, multicast.MulticastInterface{
				Regex:    regexp.MustCompile(intf.Regex),
				Beacon:   intf.Beacon,
				Listen:   intf.Listen,
				Port:     intf.Port,
				Priority: uint8(intf.Priority),
			})
		}
		if n.multicast, err = multicast.New(n.core, logger, options...); err != nil {
			panic(err)
		}
		if n.admin != nil && n.multicast != nil {
			n.multicast.SetupAdminHandlers(n.admin)
		}
	}

	// Setup the TUN module.
	{
		options := []tun.SetupOption{
			tun.InterfaceName(cfg.IfName),
			tun.InterfaceMTU(cfg.IfMTU),
		}
		if n.tun, err = tun.New(n.core, logger, options...); err != nil {
			panic(err)
		}
		if n.admin != nil && n.tun != nil {
			n.tun.SetupAdminHandlers(n.admin)
		}
	}

	// Make some nice output that tells us what our IPv6 address and subnet are.
	// This is just logged to stdout for the user.
	address := n.core.Address()
	subnet := n.core.Subnet()
	public := n.core.GetSelf().Key
	logger.Infof("Your public key is %s", hex.EncodeToString(public[:]))
	logger.Infof("Your IPv6 address is %s", address.String())
	logger.Infof("Your IPv6 subnet is %s", subnet.String())
	// Block until we are told to shut down.
	<-ctx.Done()

	// Shut down the node.
	_ = n.admin.Stop()
	_ = n.multicast.Stop()
	_ = n.tun.Stop()
	n.core.Stop()
}

func main() {
	args := getArgs()

	// Catch interrupts from the operating system to exit gracefully.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// Capture the service being stopped on Windows.
	minwinsvc.SetOnExit(cancel)

	// Start the node, block and then wait for it to shut down.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		run(args, ctx)
	}()
	wg.Wait()
}
