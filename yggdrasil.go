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

func (n *node) init(cfg *nodeConfig, logger *log.Logger) {
	boxPub, err := hex.DecodeString(cfg.BoxPub)
	if err != nil {
		panic(err)
	}
	boxPriv, err := hex.DecodeString(cfg.BoxPriv)
	if err != nil {
		panic(err)
	}
	sigPub, err := hex.DecodeString(cfg.SigPub)
	if err != nil {
		panic(err)
	}
	sigPriv, err := hex.DecodeString(cfg.SigPriv)
	if err != nil {
		panic(err)
	}
	n.core.DEBUG_init(boxPub, boxPriv, sigPub, sigPriv)
	n.core.DEBUG_setLogger(logger)

	logger.Println("Starting interface...")
	n.core.DEBUG_setupAndStartGlobalTCPInterface(cfg.Listen) // Listen for peers on TCP
	n.core.DEBUG_setupAndStartGlobalUDPInterface(cfg.Listen) // Also listen on UDP, TODO allow separate configuration for ip/port to listen on each of these
	logger.Println("Started interface")
	logger.Println("Starting admin socket...")
	n.core.DEBUG_setupAndStartAdminInterface(cfg.AdminListen)
	logger.Println("Started admin socket")
	for _, pBoxStr := range cfg.AllowedBoxPubs {
		n.core.DEBUG_addAllowedBoxPub(pBoxStr)
	}
	logger.Println(cfg.LinkLocal)
	for _, ll := range cfg.LinkLocal {
		logger.Println("Adding expression", ll)
		ifceExpr, err := regexp.Compile(ll)
		if err != nil {
			panic(err)
		}
		logger.Println("Added expression", ifceExpr)
		n.core.DEBUG_setIfceExpr(ifceExpr)
	}
	n.core.DEBUG_setupAndStartMulticastInterface()

	go func() {
		if len(cfg.Peers) == 0 {
			return
		}
		for {
			for _, p := range cfg.Peers {
				n.core.DEBUG_addPeer(p)
				time.Sleep(time.Second)
			}
			time.Sleep(time.Minute)
		}
	}()
}

func generateConfig(isAutoconf bool) *nodeConfig {
	core := Core{}
	bpub, bpriv := core.DEBUG_newBoxKeys()
	spub, spriv := core.DEBUG_newSigKeys()
	cfg := nodeConfig{}
	if isAutoconf {
		cfg.Listen = "[::]:0"
	} else {
		r1 := rand.New(rand.NewSource(time.Now().UnixNano()))
		cfg.Listen = fmt.Sprintf("[::]:%d", r1.Intn(65534-32768)+32768)
	}
	cfg.AdminListen = "[::1]:9001"
	cfg.BoxPub = hex.EncodeToString(bpub[:])
	cfg.BoxPriv = hex.EncodeToString(bpriv[:])
	cfg.SigPub = hex.EncodeToString(spub[:])
	cfg.SigPriv = hex.EncodeToString(spriv[:])
	cfg.Peers = []string{}
	cfg.AllowedBoxPubs = []string{}
	cfg.Multicast = false
	cfg.LinkLocal = []string{}
	cfg.IfName = core.DEBUG_GetTUNDefaultIfName()
	cfg.IfMTU = core.DEBUG_GetTUNDefaultIfMTU()
	cfg.IfTAPMode = core.DEBUG_GetTUNDefaultIfTAPMode()

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
		if err = mapstructure.Decode(dat, &cfg); err != nil {
			panic(err)
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
	logger.Println("Initializing...")
	n := node{}
	n.init(cfg, logger)
	if cfg.IfName != "none" {
		logger.Println("Starting TUN/TAP...")
	} else {
		logger.Println("Not starting TUN/TAP")
	}
	//n.core.DEBUG_startTun(cfg.IfName) // 1280, the smallest supported MTU
	n.core.DEBUG_startTunWithMTU(cfg.IfName, cfg.IfTAPMode, cfg.IfMTU) // Largest supported MTU
	defer func() {
		logger.Println("Closing...")
		n.core.DEBUG_stopTun()
	}()
	logger.Println("Started...")
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
	logger.Println("Stopping...")
}
