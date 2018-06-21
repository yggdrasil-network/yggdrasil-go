package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"golang.org/x/text/encoding/unicode"

	"github.com/kardianos/minwinsvc"
	"github.com/mitchellh/mapstructure"
	"github.com/neilalexander/hjson-go"

	"yggdrasil"
	"yggdrasil/config"
)

type nodeConfig = config.NodeConfig
type Core = yggdrasil.Core

type node struct {
	core Core
}

// Generates default configuration. This is used when outputting the -genconf
// parameter and also when using -autoconf. The isAutoconf flag is used to
// determine whether the operating system should select a free port by itself
// (which guarantees that there will not be a conflict with any other services)
// or whether to generate a random port number. The only side effect of setting
// isAutoconf is that the TCP and UDP ports will likely end up with different
// port numbers.
func generateConfig(isAutoconf bool) *nodeConfig {
	// Create a new core.
	core := Core{}
	// Generate encryption keys.
	bpub, bpriv := core.NewEncryptionKeys()
	spub, spriv := core.NewSigningKeys()
	// Create a node configuration and populate it.
	cfg := nodeConfig{}
	if isAutoconf {
		cfg.Listen = "[::]:0"
	} else {
		r1 := rand.New(rand.NewSource(time.Now().UnixNano()))
		cfg.Listen = fmt.Sprintf("[::]:%d", r1.Intn(65534-32768)+32768)
	}
	cfg.AdminListen = "localhost:9001"
	cfg.EncryptionPublicKey = hex.EncodeToString(bpub[:])
	cfg.EncryptionPrivateKey = hex.EncodeToString(bpriv[:])
	cfg.SigningPublicKey = hex.EncodeToString(spub[:])
	cfg.SigningPrivateKey = hex.EncodeToString(spriv[:])
	cfg.Peers = []string{}
	cfg.AllowedEncryptionPublicKeys = []string{}
	cfg.MulticastInterfaces = []string{".*"}
	cfg.IfName = core.GetTUNDefaultIfName()
	cfg.IfMTU = core.GetTUNDefaultIfMTU()
	cfg.IfTAPMode = core.GetTUNDefaultIfTAPMode()

	return &cfg
}

// Generates a new configuration and returns it in HJSON format. This is used
// with -genconf.
func doGenconf() string {
	cfg := generateConfig(false)
	bs, err := hjson.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	return string(bs)
}

// The main function is responsible for configuring and starting Yggdrasil.
func main() {
	// Configure the command line parameters.
	genconf := flag.Bool("genconf", false, "print a new config to stdout")
	useconf := flag.Bool("useconf", false, "read config from stdin")
	useconffile := flag.String("useconffile", "", "read config from specified file path")
	normaliseconf := flag.Bool("normaliseconf", false, "use in combination with either -useconf or -useconffile, outputs your configuration normalised")
	autoconf := flag.Bool("autoconf", false, "automatic mode (dynamic IP, peer with IPv6 neighbors)")
	flag.Parse()

	var cfg *nodeConfig
	switch {
	case *autoconf:
		// Use an autoconf-generated config, this will give us random keys and
		// port numbers, and will use an automatically selected TUN/TAP interface.
		cfg = generateConfig(true)
	case *useconffile != "" || *useconf:
		// Use a configuration file. If -useconf, the configuration will be read
		// from stdin. If -useconffile, the configuration will be read from the
		// filesystem.
		var config []byte
		var err error
		if *useconffile != "" {
			// Read the file from the filesystem
			config, err = ioutil.ReadFile(*useconffile)
		} else {
			// Read the file from stdin.
			config, err = ioutil.ReadAll(os.Stdin)
		}
		if err != nil {
			panic(err)
		}
		// If there's a byte order mark - which Windows 10 is now incredibly fond of
		// throwing everywhere when it's converting things into UTF-16 for the hell
		// of it - remove it and decode back down into UTF-8. This is necessary
		// because hjson doesn't know what to do with UTF-16 and will panic
		if bytes.Compare(config[0:2], []byte{0xFF, 0xFE}) == 0 ||
			bytes.Compare(config[0:2], []byte{0xFE, 0xFF}) == 0 {
			utf := unicode.UTF16(unicode.BigEndian, unicode.UseBOM)
			decoder := utf.NewDecoder()
			config, err = decoder.Bytes(config)
			if err != nil {
				panic(err)
			}
		}
		// Generate a new configuration - this gives us a set of sane defaults -
		// then parse the configuration we loaded above on top of it. The effect
		// of this is that any configuration item that is missing from the provided
		// configuration will use a sane default.
		cfg = generateConfig(false)
		var dat map[string]interface{}
		if err := hjson.Unmarshal(config, &dat); err != nil {
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
						log.Println("Warning: Deprecated config option", from, "- please remove")
					}
				} else {
					if !*normaliseconf {
						log.Println("Warning: Deprecated config option", from, "- please rename to", to)
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
		// Overlay our newly mapped configuration onto the autoconf node config that
		// we generated above.
		if err = mapstructure.Decode(dat, &cfg); err != nil {
			panic(err)
		}
		// If the -normaliseconf option was specified then remarshal the above
		// configuration and print it back to stdout. This lets the user update
		// their configuration file with newly mapped names (like above) or to
		// convert from plain JSON to commented HJSON.
		if *normaliseconf {
			bs, err := hjson.Marshal(cfg)
			if err != nil {
				panic(err)
			}
			fmt.Println(string(bs))
			return
		}
	case *genconf:
		// Generate a new configuration and print it to stdout.
		fmt.Println(doGenconf())
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
	// Setup the Yggdrasil node itself. The node{} type includes a Core, so we
	// don't need to create this manually.
	n := node{}
	// Check to see if any multicast interface expressions were provided in the
	// config. If they were then set them now.
	for _, ll := range cfg.MulticastInterfaces {
		ifceExpr, err := regexp.Compile(ll)
		if err != nil {
			panic(err)
		}
		n.core.AddMulticastInterfaceExpr(ifceExpr)
	}
	// Now that we have a working configuration, we can now actually start
	// Yggdrasil. This will start the router, switch, DHT node, TCP and UDP
	// sockets, TUN/TAP adapter and multicast discovery port.
	if err := n.core.Start(cfg, logger); err != nil {
		logger.Println("An error occurred during startup")
		panic(err)
	}
	// Check to see if any allowed encryption keys were provided in the config.
	// If they were then set them now.
	for _, pBoxStr := range cfg.AllowedEncryptionPublicKeys {
		n.core.AddAllowedEncryptionPublicKey(pBoxStr)
	}
	// If any static peers were provided in the configuration above then we should
	// configure them. The loop ensures that disconnected peers will eventually
	// be reconnected with.
	go func() {
		if len(cfg.Peers) == 0 {
			return
		}
		for {
			for _, p := range cfg.Peers {
				n.core.AddPeer(p)
				time.Sleep(time.Second)
			}
			time.Sleep(time.Minute)
		}
	}()
	// The Stop function ensures that the TUN/TAP adapter is correctly shut down
	// before the program exits.
	defer func() {
		n.core.Stop()
	}()
	// Make some nice output that tells us what our IPv6 address and subnet are.
	// This is just logged to stdout for the user.
	address := n.core.GetAddress()
	subnet := n.core.GetSubnet()
	logger.Printf("Your IPv6 address is %s", address.String())
	logger.Printf("Your IPv6 subnet is %s", subnet.String())
	// Catch interrupts from the operating system to exit gracefully.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	// Create a function to capture the service being stopped on Windows.
	winTerminate := func() {
		c <- os.Interrupt
	}
	minwinsvc.SetOnExit(winTerminate)
	// Wait for the terminate/interrupt signal. Once a signal is received, the
	// deferred Stop function above will run which will shut down TUN/TAP.
	<-c
}
