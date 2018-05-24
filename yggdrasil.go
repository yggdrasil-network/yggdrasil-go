package main

import "encoding/hex"
import "flag"
import "fmt"
import "io/ioutil"
import "net"
import "os"
import "os/signal"
import "syscall"
import "time"
import "regexp"
import "math/rand"

import _ "net/http/pprof"
import "net/http"
import "log"
import "runtime"

import "yggdrasil"
import "yggdrasil/config"

import "github.com/kardianos/minwinsvc"
import "github.com/neilalexander/hjson-go"
import "github.com/mitchellh/mapstructure"

type nodeConfig = config.NodeConfig
type Core = yggdrasil.Core

type node struct {
	core Core
}

func generateConfig(isAutoconf bool) *nodeConfig {
	core := Core{}
	bpub, bpriv := core.NewEncryptionKeys()
	spub, spriv := core.NewSigningKeys()
	cfg := nodeConfig{}
	if isAutoconf {
		cfg.Listen = "[::]:0"
	} else {
		r1 := rand.New(rand.NewSource(time.Now().UnixNano()))
		cfg.Listen = fmt.Sprintf("[::]:%d", r1.Intn(65534-32768)+32768)
	}
	cfg.AdminListen = "[::1]:9001"
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

func doGenconf() string {
	cfg := generateConfig(false)
	bs, err := hjson.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	return string(bs)
}

var pprof = flag.Bool("pprof", false, "Run pprof, see http://localhost:6060/debug/pprof/")
var genconf = flag.Bool("genconf", false, "print a new config to stdout")
var useconf = flag.Bool("useconf", false, "read config from stdin")
var useconffile = flag.String("useconffile", "", "read config from specified file path")
var normaliseconf = flag.Bool("normaliseconf", false, "use in combination with either -useconf or -useconffile, outputs your configuration normalised")
var autoconf = flag.Bool("autoconf", false, "automatic mode (dynamic IP, peer with IPv6 neighbors)")

func main() {
	flag.Parse()
	var cfg *nodeConfig
	switch {
	case *autoconf:
		cfg = generateConfig(true)
	case *useconffile != "" || *useconf:
		var config []byte
		var err error
		if *useconffile != "" {
			config, err = ioutil.ReadFile(*useconffile)
		} else {
			config, err = ioutil.ReadAll(os.Stdin)
		}
		if err != nil {
			panic(err)
		}
		cfg = generateConfig(false)
		var dat map[string]interface{}
		if err := hjson.Unmarshal(config, &dat); err != nil {
			panic(err)
		}
		// For now we will do a little bit to help the user adjust their
		// configuration to match the new configuration format
		changes := map[string]string{
			"Multicast":      "",
			"LinkLocal":      "MulticastInterfaces",
			"BoxPub":         "EncryptionPublicKey",
			"BoxPriv":        "EncryptionPrivateKey",
			"SigPub":         "SigningPublicKey",
			"SigPriv":        "SigningPrivateKey",
			"AllowedBoxPubs": "AllowedEncryptionPublicKeys",
		}
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
					if _, ok := dat[to]; !ok {
						dat[to] = dat[from]
					}
				}
			}
		}
		if err = mapstructure.Decode(dat, &cfg); err != nil {
			panic(err)
		}
		if *normaliseconf {
			bs, err := hjson.Marshal(cfg)
			if err != nil {
				panic(err)
			}
			fmt.Println(string(bs))
			return
		}
	case *genconf:
		fmt.Println(doGenconf())
	default:
		flag.PrintDefaults()
	}
	if cfg == nil {
		return
	}
	logger := log.New(os.Stdout, "", log.Flags())
	if *pprof {
		runtime.SetBlockProfileRate(1)
		go func() { log.Println(http.ListenAndServe("localhost:6060", nil)) }()
	}
	// Setup
	n := node{}
	for _, pBoxStr := range cfg.AllowedEncryptionPublicKeys {
		n.core.AddAllowedEncryptionPublicKey(pBoxStr)
	}
	for _, ll := range cfg.MulticastInterfaces {
		ifceExpr, err := regexp.Compile(ll)
		if err != nil {
			panic(err)
		}
		n.core.AddMulticastInterfaceExpr(ifceExpr)
	}
	if err := n.core.Start(cfg, logger); err != nil {
		logger.Println("An error occured during startup")
		panic(err)
	}
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
	defer func() {
		n.core.Stop()
	}()
	address := (*n.core.GetAddress())[:]
	subnet := (*n.core.GetSubnet())[:]
	subnet = append(subnet, 0, 0, 0, 0, 0, 0, 0, 0)
	logger.Printf("Your IPv6 address is %s", net.IP(address).String())
	logger.Printf("Your IPv6 subnet is %s/64", net.IP(subnet).String())
	// Catch interrupt to exit gracefully
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	// Create a function to capture the service being stopped on Windows
	winTerminate := func() {
		c <- os.Interrupt
	}
	minwinsvc.SetOnExit(winTerminate)
	// Wait for the terminate/interrupt signal
	<-c
}
