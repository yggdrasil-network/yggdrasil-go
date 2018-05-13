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

import "golang.org/x/net/ipv6"

import "yggdrasil"
import "yggdrasil/config"

import "github.com/kardianos/minwinsvc"
import "github.com/neilalexander/hjson-go"
import "github.com/mitchellh/mapstructure"

type nodeConfig = config.NodeConfig
type Core = yggdrasil.Core

type node struct {
	core Core
	sock *ipv6.PacketConn
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
	ifceExpr, err := regexp.Compile(cfg.LinkLocal)
	if err != nil {
		panic(err)
	}
	n.core.DEBUG_setIfceExpr(ifceExpr)

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
	cfg.Multicast = true
	cfg.LinkLocal = ""
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

var multicastAddr = "[ff02::114]:9001"

func (n *node) listen() {
	groupAddr, err := net.ResolveUDPAddr("udp6", multicastAddr)
	if err != nil {
		panic(err)
	}
	bs := make([]byte, 2048)
	for {
		nBytes, rcm, fromAddr, err := n.sock.ReadFrom(bs)
		if err != nil {
			panic(err)
		}
		//if rcm == nil { continue } // wat
		//fmt.Println("DEBUG:", "packet from:", fromAddr.String())
		if rcm != nil {
			// Windows can't set the flag needed to return a non-nil value here
			// So only make these checks if we get something useful back
			// TODO? Skip them always, I'm not sure if they're really needed...
			if !rcm.Dst.IsLinkLocalMulticast() {
				continue
			}
			if !rcm.Dst.Equal(groupAddr.IP) {
				continue
			}
		}
		anAddr := string(bs[:nBytes])
		addr, err := net.ResolveTCPAddr("tcp6", anAddr)
		if err != nil {
			panic(err)
			continue
		} // Panic for testing, remove later
		from := fromAddr.(*net.UDPAddr)
		//fmt.Println("DEBUG:", "heard:", addr.IP.String(), "from:", from.IP.String())
		if addr.IP.String() != from.IP.String() {
			continue
		}
		addr.Zone = from.Zone
		saddr := addr.String()
		//if _, isIn := n.peers[saddr]; isIn { continue }
		//n.peers[saddr] = struct{}{}
		n.core.DEBUG_addTCPConn(saddr)
		//fmt.Println("DEBUG:", "added multicast peer:", saddr)
	}
}

func (n *node) announce() {
	groupAddr, err := net.ResolveUDPAddr("udp6", multicastAddr)
	if err != nil {
		panic(err)
	}
	var anAddr net.TCPAddr
	myAddr := n.core.DEBUG_getGlobalTCPAddr()
	anAddr.Port = myAddr.Port
	destAddr, err := net.ResolveUDPAddr("udp6", multicastAddr)
	if err != nil {
		panic(err)
	}
	for {
		ifaces, err := net.Interfaces()
		if err != nil {
			panic(err)
		}
		for _, iface := range ifaces {
			n.sock.JoinGroup(&iface, groupAddr)
			//err := n.sock.JoinGroup(&iface, groupAddr)
			//if err != nil { panic(err) }
			addrs, err := iface.Addrs()
			if err != nil {
				panic(err)
			}
			for _, addr := range addrs {
				addrIP, _, _ := net.ParseCIDR(addr.String())
				if addrIP.To4() != nil {
					continue
				} // IPv6 only
				if !addrIP.IsLinkLocalUnicast() {
					continue
				}
				anAddr.IP = addrIP
				anAddr.Zone = iface.Name
				destAddr.Zone = iface.Name
				msg := []byte(anAddr.String())
				n.sock.WriteTo(msg, nil, destAddr)
				break
			}
			time.Sleep(time.Second)
		}
		time.Sleep(time.Second)
	}
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
	if cfg.Multicast {
		addr, err := net.ResolveUDPAddr("udp", multicastAddr)
		if err != nil {
			panic(err)
		}
		listenString := fmt.Sprintf("[::]:%v", addr.Port)
		conn, err := net.ListenPacket("udp6", listenString)
		if err != nil {
			panic(err)
		}
		//defer conn.Close() // Let it close on its own when the application exits
		n.sock = ipv6.NewPacketConn(conn)
		if err = n.sock.SetControlMessage(ipv6.FlagDst, true); err != nil {
			// Windows can't set this flag, so we need to handle it in other ways
			//panic(err)
		}
		go n.listen()
		go n.announce()
	}
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
