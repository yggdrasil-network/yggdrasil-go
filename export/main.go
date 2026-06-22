package main

import "C"
import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"crypto/ed25519"
	"encoding/json"

	"github.com/gologme/log"
	gsyslog "github.com/hashicorp/go-syslog"
	"github.com/hjson/hjson-go/v4"
	"github.com/kardianos/minwinsvc"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/ipv6rwc"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
	"github.com/yggdrasil-network/yggdrasil-go/src/tun"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

type node struct {
	core      *core.Core
	tun       *tun.TunAdapter
	multicast *multicast.Multicast
	admin     *admin.AdminSocket
}

var logger *log.Logger
var cfg *config.NodeConfig
var loglevel = false

func main() {}

// Catch interrupts from the operating system to exit gracefully.
var ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

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

//export Start
func Start(useconffilec *C.char) (int, *C.char) {
	useconffile := C.GoString(useconffilec)
	code, message := run(useconffile, ctx)
	return code, C.CString(string(message))
}

func run(useconffile string, ctx context.Context) (int, string) {
	minwinsvc.SetOnExit(cancel)

	if logger == nil {
		logger = log.New(os.Stdout, "", log.Flags())
		logger.Warnln("Logging defaulting to stdout")
	}

	if !loglevel {
		setLogLevel("error", logger)
	}

	cfg = config.GenerateConfig()
	var err error

	if useconffile != "" {
		f, err := os.Open(useconffile)
		if err != nil {
			return 1, err.Error()
		}
		if _, err := cfg.ReadFrom(f); err != nil {
			return 1, err.Error()
		}
		_ = f.Close()
		fmt.Printf("Config file %s loaded successfully\n", f.Name())
	}
	n := &node{}

	// Set up the Yggdrasil node itself.
	{
		options := []core.SetupOption{
			core.NodeInfo(cfg.NodeInfo),
			core.NodeInfoPrivacy(cfg.NodeInfoPrivacy),
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
				return 1, err.Error()
			}
			options = append(options, core.AllowedPublicKey(k[:]))
		}
		if n.core, err = core.New(cfg.Certificate, logger, options...); err != nil {
			return 1, err.Error()
		}
		address, subnet := n.core.Address(), n.core.Subnet()
		logger.Printf("Your public key is %s", hex.EncodeToString(n.core.PublicKey()))
		logger.Printf("Your IPv6 address is %s", address.String())
		logger.Printf("Your IPv6 subnet is %s", subnet.String())
	}

	// Setup the admin socket.
	{
		options := []admin.SetupOption{
			admin.ListenAddress(cfg.AdminListen),
		}
		if cfg.LogLookups {
			options = append(options, admin.LogLookups{})
		}
		if n.admin, err = admin.New(n.core, logger, options...); err != nil {
			return 1, err.Error()
		}
		if n.admin != nil {
			n.admin.SetupAdminHandlers()
		}
	}

	// Set up the multicast module.
	{
		options := []multicast.SetupOption{}
		for _, intf := range cfg.MulticastInterfaces {
			options = append(options, multicast.MulticastInterface{
				Regex:    regexp.MustCompile(intf.Regex),
				Beacon:   intf.Beacon,
				Listen:   intf.Listen,
				Port:     intf.Port,
				Priority: uint8(intf.Priority),
				Password: intf.Password,
			})
		}
		if n.multicast, err = multicast.New(n.core, logger, options...); err != nil {
			return 1, err.Error()
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
		if n.tun, err = tun.New(ipv6rwc.NewReadWriteCloser(n.core), logger, options...); err != nil {
			return 1, err.Error()
		}
		if n.admin != nil && n.tun != nil {
			n.tun.SetupAdminHandlers(n.admin)
		}
	}

	// Block until we are told to shut down.
	<-ctx.Done()

	// Shut down the node.
	//_ = w.Stop()
	_ = n.admin.Stop()
	_ = n.multicast.Stop()
	_ = n.tun.Stop()
	n.core.Stop()
	return 0, "Exit gracefully"
}

//export Exit
func Exit() int {
	cancel()
	return 1
}

//export GetBuildName
func GetBuildName() *C.char {
	result := version.BuildName()
	return C.CString(result)
}

//export GetVersion
func GetVersion() *C.char {
	result := version.BuildVersion()
	return C.CString(result)
}

//export GetAddress
func GetAddress(cpath *C.char) (int, *C.char) {
	cfg = config.GenerateConfig()
	path := C.GoString(cpath)
	err := ReadConfigFile(path)
	if err != nil {
		return 1, C.CString(err.Error())
	}
	privateKey := ed25519.PrivateKey(cfg.PrivateKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	result := Address(publicKey)
	return 0, C.CString(result)
}

//export GetSnet
func GetSnet(cpath *C.char) (int, *C.char) {
	cfg = config.GenerateConfig()
	path := C.GoString(cpath)
	err := ReadConfigFile(path)
	if err != nil {
		return 1, C.CString(err.Error())
	}
	privateKey := ed25519.PrivateKey(cfg.PrivateKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	result := Snet(publicKey)
	return 0, C.CString(result)
}

//export GetPkey
func GetPkey(cpath *C.char) (int, *C.char) {
	cfg = config.GenerateConfig()
	path := C.GoString(cpath)
	err := ReadConfigFile(path)
	if err != nil {
		return 1, C.CString(err.Error())
	}
	privateKey := ed25519.PrivateKey(cfg.PrivateKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	result := hex.EncodeToString(publicKey)
	return 0, C.CString(result)
}

//export GetPemKey
func GetPemKey(cpath *C.char) (int, *C.char) {
	cfg = config.GenerateConfig()
	path := C.GoString(cpath)
	err := ReadConfigFile(path)
	if err != nil {
		return 1, C.CString(err.Error())
	}
	val, result := Exportkey(cfg)
	return val, C.CString(result)
}

//export GenConfigFile
func GenConfigFile(confjson int) (int, *C.char) {
	cfg = config.GenerateConfig()
	var err error
	cfg.AdminListen = ""
	var bs []byte
	if confjson == 1 {
		bs, err = json.MarshalIndent(cfg, "", "  ")
	} else {
		bs, err = hjson.Marshal(cfg)
	}
	if err != nil {
		return 1, C.CString(err.Error())
	}
	f, err := os.Create("yggdrasil.conf")
	if err != nil {
		return 1, C.CString(err.Error())
	}
	defer f.Close()
	_, err = f.WriteString(string(bs))
	if err != nil {
		return 1, C.CString(err.Error())
	}
	fmt.Printf("Config file %s created successfully\n", f.Name())
	return 0, C.CString(string(bs))
}

//export NormaliseConfing
func NormaliseConfing(cpath *C.char, confjson int) (int, *C.char) {
	cfg = config.GenerateConfig()
	path := C.GoString(cpath)
	err := ReadConfigFile(path)
	if err != nil {
		return 1, C.CString(err.Error())
	}
	cfg.AdminListen = ""
	if cfg.PrivateKeyPath != "" {
		cfg.PrivateKey = nil
	}
	var bs []byte
	if confjson == 1 {
		bs, err = json.MarshalIndent(cfg, "", "  ")
	} else {
		bs, err = hjson.Marshal(cfg)
	}
	if err != nil {
		return 1, C.CString(err.Error())
	}
	return 0, C.CString(string(bs))
}

//export Logto
func Logto(argsc *C.char) {
	args := C.GoString(argsc)
	switch args {
	case "stdout":
		fmt.Println("stdout is set")
		logger = log.New(os.Stdout, "", log.Flags())
	case "syslog":
		fmt.Println("syslog is set")
		if syslogger, err := gsyslog.NewLogger(gsyslog.LOG_NOTICE, "DAEMON", version.BuildName()); err == nil {
			logger = log.New(syslogger, "", log.Flags())
		}
	default:
		if logfd, err := os.OpenFile(args, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			logger = log.New(logfd, "", log.Flags())
		}
		fmt.Printf("Path %s is set\n", args)
	}
}

//export SetLogLevel
func SetLogLevel(argsc *C.char) (int, *C.char) {
	if logger == nil {
		return 1, C.CString("Logger is not created")
	}
	args := C.GoString(argsc)
	setLogLevel(args, logger)
	loglevel = true
	return 0, C.CString("Loglevel has been set")
}

func Snet(publicKey ed25519.PublicKey) string {
	snet := address.SubnetForKey(publicKey)
	ipnet := net.IPNet{
		IP:   append(snet[:], 0, 0, 0, 0, 0, 0, 0, 0),
		Mask: net.CIDRMask(len(snet)*8, 128),
	}
	return ipnet.String()
}

func Address(publicKey ed25519.PublicKey) string {
	addr := address.AddrForKey(publicKey)
	ip := net.IP(addr[:])
	return ip.String()
}

func Exportkey(cfg *config.NodeConfig) (int, string) {
	pem, err := cfg.MarshalPEMPrivateKey()
	if err != nil {
		return 1, err.Error()
	}
	return 0, string(pem)
}

func ReadConfigFile(filepath string) error {
	f, err := os.Open(filepath)
	if err != nil {
		return err
	}
	if _, err := cfg.ReadFrom(f); err != nil {
		return err
	}
	_ = f.Close()
	return nil
}
