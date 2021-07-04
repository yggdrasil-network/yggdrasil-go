package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/text/encoding/unicode"

	"github.com/gologme/log"
	gsyslog "github.com/hashicorp/go-syslog"
	"github.com/hjson/hjson-go"
	"github.com/kardianos/minwinsvc"
	"github.com/mitchellh/mapstructure"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
	"github.com/yggdrasil-network/yggdrasil-go/src/tuntap"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

type node struct {
	core      core.Core
	config    *config.NodeConfig
	tuntap    *tuntap.TunAdapter
	multicast *multicast.Multicast
	admin     *admin.AdminSocket
}

func readConfig(log *log.Logger, useconf bool, useconffile string, normaliseconf bool) *config.NodeConfig {
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
	// Generate a new configuration - this gives us a set of sane defaults -
	// then parse the configuration we loaded above on top of it. The effect
	// of this is that any configuration item that is missing from the provided
	// configuration will use a sane default.
	cfg := defaults.GenerateConfig()
	var dat map[string]interface{}
	if err := hjson.Unmarshal(conf, &dat); err != nil {
		panic(err)
	}
	// Check if we have old field names
	if _, ok := dat["TunnelRouting"]; ok {
		log.Warnln("WARNING: Tunnel routing is no longer supported")
	}
	if old, ok := dat["SigningPrivateKey"]; ok {
		log.Warnln("WARNING: The \"SigningPrivateKey\" configuration option has been renamed to \"PrivateKey\"")
		if _, ok := dat["PrivateKey"]; !ok {
			if privstr, err := hex.DecodeString(old.(string)); err == nil {
				priv := ed25519.PrivateKey(privstr)
				pub := priv.Public().(ed25519.PublicKey)
				dat["PrivateKey"] = hex.EncodeToString(priv[:])
				dat["PublicKey"] = hex.EncodeToString(pub[:])
			} else {
				log.Warnln("WARNING: The \"SigningPrivateKey\" configuration option contains an invalid value and will be ignored")
			}
		}
	}
	if oldmc, ok := dat["MulticastInterfaces"]; ok {
		if oldmcvals, ok := oldmc.([]interface{}); ok {
			var newmc []config.MulticastInterfaceConfig
			for _, oldmcval := range oldmcvals {
				if str, ok := oldmcval.(string); ok {
					newmc = append(newmc, config.MulticastInterfaceConfig{
						Regex:  str,
						Beacon: true,
						Listen: true,
					})
				}
			}
			if newmc != nil {
				if oldport, ok := dat["LinkLocalTCPPort"]; ok {
					// numbers parse to float64 by default
					if port, ok := oldport.(float64); ok {
						for idx := range newmc {
							newmc[idx].Port = uint16(port)
						}
					}
				}
				dat["MulticastInterfaces"] = newmc
			}
		}
	}
	// Sanitise the config
	confJson, err := json.Marshal(dat)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(confJson, &cfg); err != nil {
		panic(err)
	}
	// Overlay our newly mapped configuration onto the autoconf node config that
	// we generated above.
	if err = mapstructure.Decode(dat, &cfg); err != nil {
		panic(err)
	}
	return cfg
}

// Generates a new configuration and returns it in HJSON format. This is used
// with -genconf.
func doGenconf(isjson bool) string {
	cfg := defaults.GenerateConfig()
	var bs []byte
	var err error
	if isjson {
		bs, err = json.MarshalIndent(cfg, "", "  ")
	} else {
		bs, err = hjson.Marshal(cfg)
	}
	if err != nil {
		panic(err)
	}
	return string(bs)
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
	useconffile   string
	normaliseconf bool
	confjson      bool
	autoconf      bool
	ver           bool
	logto         string
	getaddr       bool
	getsnet       bool
	loglevel      string
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
	}
}

// The main function is responsible for configuring and starting Yggdrasil.
func run(args yggArgs, ctx context.Context, done chan struct{}) {
	defer close(done)
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
		// port numbers, and will use an automatically selected TUN/TAP interface.
		cfg = defaults.GenerateConfig()
	case args.useconffile != "" || args.useconf:
		// Read the configuration from either stdin or from the filesystem
		cfg = readConfig(logger, args.useconf, args.useconffile, args.normaliseconf)
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
		fmt.Println(doGenconf(args.confjson))
		return
	default:
		// No flags were provided, therefore print the list of flags to stdout.
		flag.PrintDefaults()
	}
	// Have we got a working configuration? If we don't then it probably means
	// that neither -autoconf, -useconf or -useconffile were set above. Stop
	// if we don't.
	if cfg == nil {
		return
	}
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
			addr := address.AddrForKey(key)
			ip := net.IP(addr[:])
			fmt.Println(ip.String())
		}
		return
	case args.getsnet:
		if key := getNodeKey(); key != nil {
			snet := address.SubnetForKey(key)
			ipnet := net.IPNet{
				IP:   append(snet[:], 0, 0, 0, 0, 0, 0, 0, 0),
				Mask: net.CIDRMask(len(snet)*8, 128),
			}
			fmt.Println(ipnet.String())
		}
		return
	default:
	}

	// Setup the Yggdrasil node itself. The node{} type includes a Core, so we
	// don't need to create this manually.
	n := node{config: cfg}
	// Now start Yggdrasil - this starts the DHT, router, switch and other core
	// components needed for Yggdrasil to operate
	if err = n.core.Start(cfg, logger); err != nil {
		logger.Errorln("An error occurred during startup")
		panic(err)
	}
	// Register the session firewall gatekeeper function
	// Allocate our modules
	n.admin = &admin.AdminSocket{}
	n.multicast = &multicast.Multicast{}
	n.tuntap = &tuntap.TunAdapter{}
	// Start the admin socket
	if err := n.admin.Init(&n.core, cfg, logger, nil); err != nil {
		logger.Errorln("An error occurred initialising admin socket:", err)
	} else if err := n.admin.Start(); err != nil {
		logger.Errorln("An error occurred starting admin socket:", err)
	}
	n.admin.SetupAdminHandlers(n.admin)
	// Start the multicast interface
	if err := n.multicast.Init(&n.core, cfg, logger, nil); err != nil {
		logger.Errorln("An error occurred initialising multicast:", err)
	} else if err := n.multicast.Start(); err != nil {
		logger.Errorln("An error occurred starting multicast:", err)
	}
	n.multicast.SetupAdminHandlers(n.admin)
	// Start the TUN/TAP interface
	if err := n.tuntap.Init(&n.core, cfg, logger, nil); err != nil {
		logger.Errorln("An error occurred initialising TUN/TAP:", err)
	} else if err := n.tuntap.Start(); err != nil {
		logger.Errorln("An error occurred starting TUN/TAP:", err)
	}
	n.tuntap.SetupAdminHandlers(n.admin)
	// Make some nice output that tells us what our IPv6 address and subnet are.
	// This is just logged to stdout for the user.
	address := n.core.Address()
	subnet := n.core.Subnet()
	public := n.core.GetSelf().Key
	logger.Infof("Your public key is %s", hex.EncodeToString(public[:]))
	logger.Infof("Your IPv6 address is %s", address.String())
	logger.Infof("Your IPv6 subnet is %s", subnet.String())
	// Catch interrupts from the operating system to exit gracefully.
	<-ctx.Done()
	// Capture the service being stopped on Windows.
	minwinsvc.SetOnExit(n.shutdown)
	n.shutdown()
}

func (n *node) shutdown() {
	_ = n.admin.Stop()
	_ = n.multicast.Stop()
	_ = n.tuntap.Stop()
	n.core.Stop()
}

func main() {
	args := getArgs()
	hup := make(chan os.Signal, 1)
	//signal.Notify(hup, os.Interrupt, syscall.SIGHUP)
	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
	for {
		done := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		go run(args, ctx, done)
		select {
		case <-hup:
			cancel()
			<-done
		case <-term:
			cancel()
			<-done
			return
		case <-done:
			return
		}
	}
}
