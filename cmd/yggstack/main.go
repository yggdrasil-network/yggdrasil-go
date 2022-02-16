package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/gologme/log"
	gsyslog "github.com/hashicorp/go-syslog"
	"github.com/hjson/hjson-go"
	"github.com/things-go/go-socks5"

	"github.com/yggdrasil-network/yggdrasil-go/cmd/yggstack/types"
	"github.com/yggdrasil-network/yggdrasil-go/contrib/netstack"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/setup"

	"github.com/yggdrasil-network/yggdrasil-go/src/version"

	_ "net/http/pprof"
)

// The main function is responsible for configuring and starting Yggdrasil.
func main() {
	var expose types.TCPMappings
	socks := flag.String("socks", "", "address to listen on for SOCKS, i.e. :1080")
	nameserver := flag.String("nameserver", "", "the Yggdrasil IPv6 address to use as a DNS server for SOCKS")
	flag.Var(&expose, "exposetcp", "TCP ports to expose to the network, e.g. 22, 2022:22, 22:192.168.1.1:2022")
	args := setup.ParseArguments()

	// Create a new logger that logs output to stdout.
	var logger *log.Logger
	switch args.LogTo {
	case "stdout":
		logger = log.New(os.Stdout, "", log.Flags())
	case "syslog":
		if syslogger, err := gsyslog.NewLogger(gsyslog.LOG_NOTICE, "DAEMON", version.BuildName()); err == nil {
			logger = log.New(syslogger, "", log.Flags())
		}
	default:
		if logfd, err := os.OpenFile(args.LogTo, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			logger = log.New(logfd, "", log.Flags())
		}
	}
	if logger == nil {
		logger = log.New(os.Stdout, "", log.Flags())
		logger.Warnln("Logging defaulting to stdout")
	}

	var cfg *config.NodeConfig
	var err error
	switch {
	case args.Version:
		fmt.Println("Build name:", version.BuildName())
		fmt.Println("Build version:", version.BuildVersion())
		return
	case args.AutoConf:
		// Use an autoconf-generated config, this will give us random keys and
		// port numbers, and will use an automatically selected TUN/TAP interface.
		cfg = config.GenerateConfig()
	case args.UseConfFile != "" || args.UseConf:
		// Read the configuration from either stdin or from the filesystem
		cfg = setup.ReadConfig(logger, args.UseConf, args.UseConfFile, args.NormaliseConf)
		// If the -normaliseconf option was specified then remarshal the above
		// configuration and print it back to stdout. This lets the user update
		// their configuration file with newly mapped names (like above) or to
		// convert from plain JSON to commented HJSON.
		if args.NormaliseConf {
			var bs []byte
			if args.ConfJSON {
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
	case args.GenConf:
		// Generate a new configuration and print it to stdout.
		fmt.Printf("%s\n", config.GenerateConfigJSON(args.ConfJSON))
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

	// Create a new standalone node
	n := setup.NewNode(cfg, logger)
	n.SetLogLevel(args.LogLevel)

	// Have we been asked for the node address yet? If so, print it and then stop.
	getNodeKey := func() ed25519.PublicKey {
		if pubkey, err := hex.DecodeString(cfg.PrivateKey); err == nil {
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
		return
	case args.GetSubnet:
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

	// Now start Yggdrasil - this starts the DHT, router, switch and other core
	// components needed for Yggdrasil to operate
	if err = n.Run(args); err != nil {
		logger.Fatalln(err)
	}

	// Make some nice output that tells us what our IPv6 address and subnet are.
	// This is just logged to stdout for the user.
	address := n.Address()
	subnet := n.Subnet()
	public := n.GetSelf().Key
	publicstr := hex.EncodeToString(public[:])
	logger.Infof("Your public key is %s", publicstr)
	logger.Infof("Your IPv6 address is %s", address.String())
	logger.Infof("Your IPv6 subnet is %s", subnet.String())
	logger.Infof("Your Yggstack resolver name is %s%s", publicstr, types.NameMappingSuffix)

	s, err := netstack.CreateYggdrasilNetstack(&n.Core)
	if err != nil {
		logger.Fatalln(err)
	}

	if *socks != "" {
		resolver := types.NewNameResolver(s, *nameserver)
		server := socks5.NewServer(
			socks5.WithDial(s.DialContext),
			socks5.WithResolver(resolver),
		)
		go server.ListenAndServe("tcp", *socks) // nolint:errcheck
	}

	for _, mapping := range expose {
		go func(mapping types.TCPMapping) {
			listener, err := s.ListenTCP(mapping.Listen)
			if err != nil {
				panic(err)
			}
			logger.Infof("Mapping Yggdrasil port %d to %s", mapping.Listen.Port, mapping.Mapped)
			for {
				c, err := listener.Accept()
				if err != nil {
					panic(err)
				}
				r, err := net.DialTCP("tcp", nil, mapping.Mapped)
				if err != nil {
					logger.Errorf("Failed to connect to %s: %s", mapping.Mapped, err)
					_ = c.Close()
					continue
				}
				types.ProxyTCP(n.MTU(), c, r)
			}
		}(mapping)
	}

	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)

	select {
	case <-n.Done():
	case <-term:
	}

	n.Close()
}
