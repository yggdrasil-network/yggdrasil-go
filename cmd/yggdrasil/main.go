package main

import (
	"bytes"
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
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/module"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
	"github.com/yggdrasil-network/yggdrasil-go/src/tuntap"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

type node struct {
	core      yggdrasil.Core
	state     *config.NodeState
	tuntap    module.Module // tuntap.TunAdapter
	multicast module.Module // multicast.Multicast
	admin     module.Module // admin.AdminSocket
}

func readConfig(useconf *bool, useconffile *string, normaliseconf *bool) *config.NodeConfig {
	// Use a configuration file. If -useconf, the configuration will be read
	// from stdin. If -useconffile, the configuration will be read from the
	// filesystem.
	var conf []byte
	var err error
	if *useconffile != "" {
		// Read the file from the filesystem
		conf, err = ioutil.ReadFile(*useconffile)
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
	cfg := config.GenerateConfig()
	var dat map[string]interface{}
	if err := hjson.Unmarshal(conf, &dat); err != nil {
		panic(err)
	}
	// Check for fields that have changed type recently, e.g. the Listen config
	// option is now a []string rather than a string
	if listen, ok := dat["Listen"].(string); ok {
		dat["Listen"] = []string{listen}
	}
	if tunnelrouting, ok := dat["TunnelRouting"].(map[string]interface{}); ok {
		if c, ok := tunnelrouting["IPv4Sources"]; ok {
			delete(tunnelrouting, "IPv4Sources")
			tunnelrouting["IPv4LocalSubnets"] = c
		}
		if c, ok := tunnelrouting["IPv6Sources"]; ok {
			delete(tunnelrouting, "IPv6Sources")
			tunnelrouting["IPv6LocalSubnets"] = c
		}
		if c, ok := tunnelrouting["IPv4Destinations"]; ok {
			delete(tunnelrouting, "IPv4Destinations")
			tunnelrouting["IPv4RemoteSubnets"] = c
		}
		if c, ok := tunnelrouting["IPv6Destinations"]; ok {
			delete(tunnelrouting, "IPv6Destinations")
			tunnelrouting["IPv6RemoteSubnets"] = c
		}
	}
	// Sanitise the config
	confJson, err := json.Marshal(dat)
	if err != nil {
		panic(err)
	}
	json.Unmarshal(confJson, &cfg)
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
	cfg := config.GenerateConfig()
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

// The main function is responsible for configuring and starting Yggdrasil.
func main() {
	// Configure the command line parameters.
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

	var cfg *config.NodeConfig
	var err error
	switch {
	case *ver:
		fmt.Println("Build name:", version.BuildName())
		fmt.Println("Build version:", version.BuildVersion())
		return
	case *autoconf:
		// Use an autoconf-generated config, this will give us random keys and
		// port numbers, and will use an automatically selected TUN/TAP interface.
		cfg = config.GenerateConfig()
	case *useconffile != "" || *useconf:
		// Read the configuration from either stdin or from the filesystem
		cfg = readConfig(useconf, useconffile, normaliseconf)
		// If the -normaliseconf option was specified then remarshal the above
		// configuration and print it back to stdout. This lets the user update
		// their configuration file with newly mapped names (like above) or to
		// convert from plain JSON to commented HJSON.
		if *normaliseconf {
			var bs []byte
			if *confjson {
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
	case *genconf:
		// Generate a new configuration and print it to stdout.
		fmt.Println(doGenconf(*confjson))
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
	getNodeID := func() *crypto.NodeID {
		if pubkey, err := hex.DecodeString(cfg.EncryptionPublicKey); err == nil {
			var box crypto.BoxPubKey
			copy(box[:], pubkey)
			return crypto.GetNodeID(&box)
		}
		return nil
	}
	switch {
	case *getaddr:
		if nodeid := getNodeID(); nodeid != nil {
			addr := *address.AddrForNodeID(nodeid)
			ip := net.IP(addr[:])
			fmt.Println(ip.String())
		}
		return
	case *getsnet:
		if nodeid := getNodeID(); nodeid != nil {
			snet := *address.SubnetForNodeID(nodeid)
			ipnet := net.IPNet{
				IP:   append(snet[:], 0, 0, 0, 0, 0, 0, 0, 0),
				Mask: net.CIDRMask(len(snet)*8, 128),
			}
			fmt.Println(ipnet.String())
		}
		return
	default:
	}
	// Create a new logger that logs output to stdout.
	var logger *log.Logger
	switch *logto {
	case "stdout":
		logger = log.New(os.Stdout, "", log.Flags())
	case "syslog":
		if syslogger, err := gsyslog.NewLogger(gsyslog.LOG_NOTICE, "DAEMON", version.BuildName()); err == nil {
			logger = log.New(syslogger, "", log.Flags())
		}
	default:
		if logfd, err := os.OpenFile(*logto, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			logger = log.New(logfd, "", log.Flags())
		}
	}
	if logger == nil {
		logger = log.New(os.Stdout, "", log.Flags())
		logger.Warnln("Logging defaulting to stdout")
	}

	setLogLevel(*loglevel, logger)

	// Setup the Yggdrasil node itself. The node{} type includes a Core, so we
	// don't need to create this manually.
	n := node{}
	// Now start Yggdrasil - this starts the DHT, router, switch and other core
	// components needed for Yggdrasil to operate
	n.state, err = n.core.Start(cfg, logger)
	if err != nil {
		logger.Errorln("An error occurred during startup")
		panic(err)
	}
	// Register the session firewall gatekeeper function
	n.core.SetSessionGatekeeper(n.sessionFirewall)
	// Allocate our modules
	n.admin = &admin.AdminSocket{}
	n.multicast = &multicast.Multicast{}
	n.tuntap = &tuntap.TunAdapter{}
	// Start the admin socket
	n.admin.Init(&n.core, n.state, logger, nil)
	if err := n.admin.Start(); err != nil {
		logger.Errorln("An error occurred starting admin socket:", err)
	}
	n.admin.SetupAdminHandlers(n.admin.(*admin.AdminSocket))
	// Start the multicast interface
	n.multicast.Init(&n.core, n.state, logger, nil)
	if err := n.multicast.Start(); err != nil {
		logger.Errorln("An error occurred starting multicast:", err)
	}
	n.multicast.SetupAdminHandlers(n.admin.(*admin.AdminSocket))
	// Start the TUN/TAP interface
	if listener, err := n.core.ConnListen(); err == nil {
		if dialer, err := n.core.ConnDialer(); err == nil {
			n.tuntap.Init(&n.core, n.state, logger, tuntap.TunOptions{Listener: listener, Dialer: dialer})
			if err := n.tuntap.Start(); err != nil {
				logger.Errorln("An error occurred starting TUN/TAP:", err)
			}
			n.tuntap.SetupAdminHandlers(n.admin.(*admin.AdminSocket))
		} else {
			logger.Errorln("Unable to get Dialer:", err)
		}
	} else {
		logger.Errorln("Unable to get Listener:", err)
	}
	// Make some nice output that tells us what our IPv6 address and subnet are.
	// This is just logged to stdout for the user.
	address := n.core.Address()
	subnet := n.core.Subnet()
	logger.Infof("Your IPv6 address is %s", address.String())
	logger.Infof("Your IPv6 subnet is %s", subnet.String())
	// Catch interrupts from the operating system to exit gracefully.
	c := make(chan os.Signal, 1)
	r := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	signal.Notify(r, os.Interrupt, syscall.SIGHUP)
	// Capture the service being stopped on Windows.
	minwinsvc.SetOnExit(n.shutdown)
	defer n.shutdown()
	// Wait for the terminate/interrupt signal. Once a signal is received, the
	// deferred Stop function above will run which will shut down TUN/TAP.
	for {
		select {
		case <-c:
			goto exit
		case <-r:
			if *useconffile != "" {
				cfg = readConfig(useconf, useconffile, normaliseconf)
				logger.Infoln("Reloading configuration from", *useconffile)
				n.core.UpdateConfig(cfg)
				n.tuntap.UpdateConfig(cfg)
				n.multicast.UpdateConfig(cfg)
			} else {
				logger.Errorln("Reloading config at runtime is only possible with -useconffile")
			}
		}
	}
exit:
}

func (n *node) shutdown() {
	n.admin.Stop()
	n.multicast.Stop()
	n.tuntap.Stop()
	n.core.Stop()
}

func (n *node) sessionFirewall(pubkey *crypto.BoxPubKey, initiator bool) bool {
	n.state.Mutex.RLock()
	defer n.state.Mutex.RUnlock()

	// Allow by default if the session firewall is disabled
	if !n.state.Current.SessionFirewall.Enable {
		return true
	}

	// Prepare for checking whitelist/blacklist
	var box crypto.BoxPubKey
	// Reject blacklisted nodes
	for _, b := range n.state.Current.SessionFirewall.BlacklistEncryptionPublicKeys {
		key, err := hex.DecodeString(b)
		if err == nil {
			copy(box[:crypto.BoxPubKeyLen], key)
			if box == *pubkey {
				return false
			}
		}
	}

	// Allow whitelisted nodes
	for _, b := range n.state.Current.SessionFirewall.WhitelistEncryptionPublicKeys {
		key, err := hex.DecodeString(b)
		if err == nil {
			copy(box[:crypto.BoxPubKeyLen], key)
			if box == *pubkey {
				return true
			}
		}
	}

	// Allow outbound sessions if appropriate
	if n.state.Current.SessionFirewall.AlwaysAllowOutbound {
		if initiator {
			return true
		}
	}

	// Look and see if the pubkey is that of a direct peer
	var isDirectPeer bool
	for _, peer := range n.core.GetPeers() {
		if peer.PublicKey == *pubkey {
			isDirectPeer = true
			break
		}
	}

	// Allow direct peers if appropriate
	if n.state.Current.SessionFirewall.AllowFromDirect && isDirectPeer {
		return true
	}

	// Allow remote nodes if appropriate
	if n.state.Current.SessionFirewall.AllowFromRemote && !isDirectPeer {
		return true
	}

	// Finally, default-deny if not matching any of the above rules
	return false
}
