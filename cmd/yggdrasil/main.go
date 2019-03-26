package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/text/encoding/unicode"

	"github.com/gologme/log"
	"github.com/hjson/hjson-go"
	"github.com/kardianos/minwinsvc"
	"github.com/mitchellh/mapstructure"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

type nodeConfig = config.NodeConfig
type Core = yggdrasil.Core

type node struct {
	core Core
}

func readConfig(useconf *bool, useconffile *string, normaliseconf *bool) *nodeConfig {
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
	if bytes.Compare(conf[0:2], []byte{0xFF, 0xFE}) == 0 ||
		bytes.Compare(conf[0:2], []byte{0xFE, 0xFF}) == 0 {
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
	confJson, err := json.Marshal(dat)
	if err != nil {
		panic(err)
	}
	json.Unmarshal(confJson, &cfg)
	// For now we will do a little bit to help the user adjust their
	// configuration to match the new configuration format, as some of the key
	// names have changed recently.
	changes := map[string]string{
		"Multicast":      "",
		"ReadTimeout":    "",
		"LinkLocal":      "MulticastInterfaces",
		"BoxPub":         "EncryptionPublicKey",
		"BoxPriv":        "EncryptionPrivateKey",
		"SigPub":         "SigningPublicKey",
		"SigPriv":        "SigningPrivateKey",
		"AllowedBoxPubs": "AllowedEncryptionPublicKeys",
	}
	// Loop over the mappings aove and see if we have anything to fix.
	for from, to := range changes {
		if _, ok := dat[from]; ok {
			if to == "" {
				if !*normaliseconf {
					log.Println("Warning: Config option", from, "is deprecated")
				}
			} else {
				if !*normaliseconf {
					log.Println("Warning: Config option", from, "has been renamed - please change to", to)
				}
				// If the configuration file doesn't already contain a line with the
				// new name then set it to the old value. This makes sure that we
				// don't overwrite something that was put there intentionally.
				if _, ok := dat[to]; !ok {
					dat[to] = dat[from]
				}
			}
		}
	}
	// Check to see if the peers are in a parsable format, if not then default
	// them to the TCP scheme
	if peers, ok := dat["Peers"].([]interface{}); ok {
	peerloop:
		for index, peer := range peers {
			uri := peer.(string)
			for _, prefix := range []string{"tcp://", "socks://", "srv://", "txt://"} {
				if strings.HasPrefix(uri, prefix) {
					continue peerloop
				}
			}
			if strings.HasPrefix(uri, "tcp:") {
				uri = uri[4:]
			}
			(dat["Peers"].([]interface{}))[index] = "tcp://" + uri
		}
	}
	// Now do the same with the interface peers
	if interfacepeers, ok := dat["InterfacePeers"].(map[string]interface{}); ok {
	interfacepeerloop:
		for intf, peers := range interfacepeers {
			for index, peer := range peers.([]interface{}) {
				uri := peer.(string)
				for _, prefix := range []string{"tcp://", "socks://", "srv://", "txt://"} {
					if strings.HasPrefix(uri, prefix) {
						continue interfacepeerloop
					}
				}
				if strings.HasPrefix(uri, "tcp:") {
					uri = uri[4:]
				}
				((dat["InterfacePeers"].(map[string]interface{}))[intf]).([]interface{})[index] = "tcp://" + uri
			}
		}
	}
	// Do a quick check for old-format Listen statement so that mapstructure
	// doesn't fail and crash
	if listen, ok := dat["Listen"].(string); ok {
		if strings.HasPrefix(listen, "tcp://") {
			dat["Listen"] = []string{listen}
		} else {
			dat["Listen"] = []string{"tcp://" + listen}
		}
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

// The main function is responsible for configuring and starting Yggdrasil.
func main() {
	// Configure the command line parameters.
	genconf := flag.Bool("genconf", false, "print a new config to stdout")
	useconf := flag.Bool("useconf", false, "read HJSON/JSON config from stdin")
	useconffile := flag.String("useconffile", "", "read HJSON/JSON config from specified file path")
	normaliseconf := flag.Bool("normaliseconf", false, "use in combination with either -useconf or -useconffile, outputs your configuration normalised")
	confjson := flag.Bool("json", false, "print configuration from -genconf or -normaliseconf as JSON instead of HJSON")
	autoconf := flag.Bool("autoconf", false, "automatic mode (dynamic IP, peer with IPv6 neighbors)")
	version := flag.Bool("version", false, "prints the version of this build")
	logging := flag.String("logging", "info,warn,error", "comma-separated list of logging levels to enable")
	flag.Parse()

	var cfg *nodeConfig
	var err error
	switch {
	case *version:
		fmt.Println("Build name:", yggdrasil.GetBuildName())
		fmt.Println("Build version:", yggdrasil.GetBuildVersion())
		os.Exit(0)
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
	// Create a new logger that logs output to stdout.
	logger := log.New(os.Stdout, "", log.Flags())
	//logger.EnableLevel("error")
	//logger.EnableLevel("warn")
	//logger.EnableLevel("info")
	if levels := strings.Split(*logging, ","); len(levels) > 0 {
		for _, level := range levels {
			l := strings.TrimSpace(level)
			switch l {
			case "error", "warn", "info", "trace", "debug":
				logger.EnableLevel(l)
			default:
				continue
			}
		}
	}
	// Setup the Yggdrasil node itself. The node{} type includes a Core, so we
	// don't need to create this manually.
	n := node{}
	// Now that we have a working configuration, we can now actually start
	// Yggdrasil. This will start the router, switch, DHT node, TCP and UDP
	// sockets, TUN/TAP adapter and multicast discovery port.
	if err := n.core.Start(cfg, logger); err != nil {
		logger.Errorln("An error occurred during startup")
		panic(err)
	}
	// The Stop function ensures that the TUN/TAP adapter is correctly shut down
	// before the program exits.
	defer func() {
		n.core.Stop()
	}()
	// Make some nice output that tells us what our IPv6 address and subnet are.
	// This is just logged to stdout for the user.
	address := n.core.GetAddress()
	subnet := n.core.GetSubnet()
	logger.Infof("Your IPv6 address is %s", address.String())
	logger.Infof("Your IPv6 subnet is %s", subnet.String())
	// Catch interrupts from the operating system to exit gracefully.
	c := make(chan os.Signal, 1)
	r := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	signal.Notify(r, os.Interrupt, syscall.SIGHUP)
	// Create a function to capture the service being stopped on Windows.
	winTerminate := func() {
		c <- os.Interrupt
	}
	minwinsvc.SetOnExit(winTerminate)
	// Wait for the terminate/interrupt signal. Once a signal is received, the
	// deferred Stop function above will run which will shut down TUN/TAP.
	for {
		select {
		case _ = <-r:
			if *useconffile != "" {
				cfg = readConfig(useconf, useconffile, normaliseconf)
				n.core.UpdateConfig(cfg)
			} else {
				logger.Errorln("Reloading config at runtime is only possible with -useconffile")
			}
		case _ = <-c:
			goto exit
		}
	}
exit:
}
