package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/gologme/log"
	gsyslog "github.com/hashicorp/go-syslog"
	"github.com/hjson/hjson-go/v4"
	"github.com/kardianos/minwinsvc"
	"github.com/spf13/cobra"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/ipv6rwc"
	monitoring "github.com/yggdrasil-network/yggdrasil-go/src/monitoring"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
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

var (
	cfg    *config.NodeConfig
	logger *log.Logger
	ctx    context.Context
	cancel context.CancelFunc

	rootCmd = &cobra.Command{
		Use:   "yggdrasil",
		Short: "Yggdrasil managment",
	}

	genconfCmd = &cobra.Command{
		Use:         "genconf",
		Short:       "Print a new config to stdout",
		RunE:        cmdGenconf,
		Annotations: map[string]string{"type": "setup"},
	}

	versionCmd = &cobra.Command{
		Use:         "version",
		Short:       "Prints the version of this build",
		Run:         cmdVersion,
		Annotations: map[string]string{"type": "setup"},
	}

	useconffileCmd = &cobra.Command{
		Use:         "useconffile <path>",
		Args:        cobra.ExactArgs(1),
		Short:       "Read HJSON/JSON config from specified file path",
		RunE:        cmdUseconffile,
		Annotations: map[string]string{"type": "setup"},
	}

	addressCmd = &cobra.Command{
		Use:         "address <path>",
		Args:        cobra.ExactArgs(1),
		Short:       "Outputs your IPv6 address",
		RunE:        cmdAddress,
		Annotations: map[string]string{"type": "setup"},
	}

	snetCmd = &cobra.Command{
		Use:         "subnet <path>",
		Args:        cobra.ExactArgs(1),
		Short:       "Outputs your IPv6 subnet",
		RunE:        cmdSnet,
		Annotations: map[string]string{"type": "setup"},
	}

	pkeyCmd = &cobra.Command{
		Use:         "publickey <path>",
		Args:        cobra.ExactArgs(1),
		Short:       "Outputs your public key",
		RunE:        cmdPkey,
		Annotations: map[string]string{"type": "setup"},
	}

	exportKeyCmd = &cobra.Command{
		Use:         "exportkey <path>",
		Args:        cobra.ExactArgs(1),
		Short:       "Outputs your private key in PEM format",
		RunE:        cmdExportKey,
		Annotations: map[string]string{"type": "setup"},
	}

	logtoCmd = &cobra.Command{
		Use:         "logto <path>",
		Args:        cobra.ExactArgs(1),
		Short:       "File path to log to, \"syslog\" or \"stdout\"",
		Run:         cmdLogto,
		Annotations: map[string]string{"type": "setup"},
	}

	autoconfCmd = &cobra.Command{
		Use:         "autoconf",
		Short:       "Automatic mode (dynamic IP, peer with IPv6 neighbors)",
		RunE:        cmdAutoconf,
		Annotations: map[string]string{"type": "setup"},
	}

	useconfCmd = &cobra.Command{
		Use:         "useconf",
		Short:       "Read HJSON/JSON config from stdin",
		RunE:        cmdUseconf,
		Annotations: map[string]string{"type": "setup"},
	}

	normaliseconfCmd = &cobra.Command{
		Use:         "normaliseconf <path>",
		Args:        cobra.ExactArgs(1),
		Short:       "Outputs your configuration normalised",
		RunE:        cmdNormaliseconf,
		Annotations: map[string]string{"type": "setup"},
	}
)

// The main function is responsible for configuring and starting Yggdrasil.
func init() {
	cfg = config.GenerateConfig()
	genconfCmd.Flags().BoolP("json", "j", false, "print configuration as JSON instead of HJSON")
	normaliseconfCmd.Flags().BoolP("json", "j", false, "print configuration as JSON instead of HJSON")
	autoconfCmd.AddCommand(logtoCmd)
	useconffileCmd.AddCommand(logtoCmd)
	useconfCmd.AddCommand(logtoCmd)
	rootCmd.AddCommand(genconfCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(useconffileCmd)
	rootCmd.AddCommand(addressCmd)
	rootCmd.AddCommand(snetCmd)
	rootCmd.AddCommand(pkeyCmd)
	rootCmd.AddCommand(exportKeyCmd)
	rootCmd.AddCommand(autoconfCmd)
	rootCmd.AddCommand(useconfCmd)
	rootCmd.AddCommand(normaliseconfCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func cmdGenconf(cmd *cobra.Command, args []string) (err error) {
	confjson, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	cfg.AdminListen = ""
	var bs []byte
	if confjson {
		bs, err = json.MarshalIndent(cfg, "", "  ")
	} else {
		bs, err = hjson.Marshal(cfg)
	}
	if err != nil {
		return err
	}
	fmt.Println(string(bs))
	return nil
}

func cmdVersion(cmd *cobra.Command, args []string) {
	fmt.Println("Build name:", version.BuildName())
	fmt.Println("Build version:", version.BuildVersion())
}

func cmdUseconffile(cmd *cobra.Command, args []string) (err error) {
	useconffile := args[0]
	err = ReadConfigFile(useconffile)
	if err != nil {
		return err
	}
	err = run()
	if err != nil {
		return err
	}
	return nil
}

func cmdAutoconf(cmd *cobra.Command, args []string) (err error) {
	err = run()
	if err != nil {
		return err
	}
	return nil
}

func cmdUseconf(cmd *cobra.Command, args []string) (err error) {
	if _, err := cfg.ReadFrom(os.Stdin); err != nil {
		return err
	}
	err = run()
	if err != nil {
		return err
	}
	return nil
}

func cmdAddress(cmd *cobra.Command, args []string) (err error) {
	useconffile := args[0]
	err = ReadConfigFile(useconffile)
	if err != nil {
		return err
	}
	privateKey := ed25519.PrivateKey(cfg.PrivateKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	addr := address.AddrForKey(publicKey)
	ip := net.IP(addr[:])
	fmt.Println(ip.String())
	return nil
}

func cmdSnet(cmd *cobra.Command, args []string) (err error) {
	useconffile := args[0]
	err = ReadConfigFile(useconffile)
	if err != nil {
		return err
	}
	privateKey := ed25519.PrivateKey(cfg.PrivateKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	snet := address.SubnetForKey(publicKey)
	ipnet := net.IPNet{
		IP:   append(snet[:], 0, 0, 0, 0, 0, 0, 0, 0),
		Mask: net.CIDRMask(len(snet)*8, 128),
	}
	fmt.Println(ipnet.String())
	return nil
}

func cmdPkey(cmd *cobra.Command, args []string) (err error) {
	useconffile := args[0]
	err = ReadConfigFile(useconffile)
	if err != nil {
		return err
	}
	privateKey := ed25519.PrivateKey(cfg.PrivateKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	fmt.Println(hex.EncodeToString(publicKey))
	return nil
}

func cmdExportKey(cmd *cobra.Command, args []string) (err error) {
	useconffile := args[0]
	err = ReadConfigFile(useconffile)
	if err != nil {
		return err
	}
	pem, err := cfg.MarshalPEMPrivateKey()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(pem))
	return nil
}

func cmdLogto(cmd *cobra.Command, args []string) {
	logto := args[0]
	switch logto {
	case "stdout":
		logger = log.New(os.Stdout, "", log.Flags())

	case "syslog":
		if syslogger, err := gsyslog.NewLogger(gsyslog.LOG_NOTICE, "DAEMON", version.BuildName()); err == nil {
			logger = log.New(syslogger, "", log.Flags()&^(log.Ldate|log.Ltime))
		}

	default:
		if logfd, err := os.OpenFile(logto, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			logger = log.New(logfd, "", log.Flags())
		}
	}
}

func cmdNormaliseconf(cmd *cobra.Command, args []string) (err error) {
	confjson, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	useconffile := args[0]
	err = ReadConfigFile(useconffile)
	if err != nil {
		return err
	}
	cfg.AdminListen = ""
	if cfg.PrivateKeyPath != "" {
		cfg.PrivateKey = nil
	}
	var bs []byte
	if confjson {
		bs, err = json.MarshalIndent(cfg, "", "  ")
	} else {
		bs, err = hjson.Marshal(cfg)
	}
	if err != nil {
		return err
	}
	fmt.Println(string(bs))
	return nil
}

func run() (err error) {
	ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Capture the service being stopped on Windows.
	minwinsvc.SetOnExit(cancel)

	if logger == nil {
		logger = log.New(os.Stdout, "", log.Flags())
		logger.Warnln("Logging defaulting to stdout")
	}
	setLogLevel("info", logger)
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
				panic(err)
			}
			options = append(options, core.AllowedPublicKey(k[:]))
		}
		if n.core, err = core.New(cfg.Certificate, logger, options...); err != nil {
			panic(err)
		}
		address, subnet := n.core.Address(), n.core.Subnet()
		logger.Printf("Your public key is %s", hex.EncodeToString(n.core.PublicKey()))
		logger.Printf("Your IPv6 address is %s", address.String())
		logger.Printf("Your IPv6 subnet is %s", subnet.String())
	}

	// Set up the admin socket.
	{
		options := []admin.SetupOption{
			admin.ListenAddress(cfg.AdminListen),
		}
		if cfg.LogLookups {
			options = append(options, admin.LogLookups{})
		}
		if n.admin, err = admin.New(n.core, logger, options...); err != nil {
			panic(err)
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
			panic(err)
		}
		if n.admin != nil && n.multicast != nil {
			n.multicast.SetupAdminHandlers(n.admin)
		}
	}

	// Set up the TUN module.
	{
		options := []tun.SetupOption{
			tun.InterfaceName(cfg.IfName),
			tun.InterfaceMTU(cfg.IfMTU),
		}
		if n.tun, err = tun.New(ipv6rwc.NewReadWriteCloser(n.core), logger, options...); err != nil {
			panic(err)
		}
		if n.admin != nil && n.tun != nil {
			n.tun.SetupAdminHandlers(n.admin)
		}
	}

	m, _ := monitoring.New(n.core, logger)

	// Block until we are told to shut down.
	<-ctx.Done()

	// Shut down the node.
	_ = m.Stop()
	_ = n.admin.Stop()
	_ = n.multicast.Stop()
	_ = n.tun.Stop()
	n.core.Stop()
	return nil
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
