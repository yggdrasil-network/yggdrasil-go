package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"inet.af/netstack/tcpip"
	"inet.af/netstack/tcpip/adapters/gonet"
	"inet.af/netstack/tcpip/buffer"
	"inet.af/netstack/tcpip/header"
	"inet.af/netstack/tcpip/network/ipv6"
	"inet.af/netstack/tcpip/stack"
	"inet.af/netstack/tcpip/transport/icmp"
	"inet.af/netstack/tcpip/transport/tcp"

	"github.com/gologme/log"
	gsyslog "github.com/hashicorp/go-syslog"
	"github.com/hjson/hjson-go"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/ipv6rwc"
	"github.com/yggdrasil-network/yggdrasil-go/src/setup"

	"github.com/yggdrasil-network/yggdrasil-go/src/version"

	_ "net/http/pprof"
)

// The main function is responsible for configuring and starting Yggdrasil.
func main() {
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
		fmt.Println(config.GenerateConfigJSON(args.ConfJSON))
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
	logger.Infof("Your public key is %s", hex.EncodeToString(public[:]))
	logger.Infof("Your IPv6 address is %s", address.String())
	logger.Infof("Your IPv6 subnet is %s", subnet.String())

	iprwc := ipv6rwc.NewReadWriteCloser(&n.Core)
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, icmp.NewProtocol6},
		HandleLocal:        true,
		IPTables:           &stack.IPTables{},
	})
	endpoint := &TCPIPEndpoint{
		stack:    s,
		ipv6rwc:  iprwc,
		readBuf:  make([]byte, iprwc.MTU()),
		writeBuf: make([]byte, iprwc.MTU()),
	}
	if err := s.CreateNIC(1, endpoint); err != nil {
		panic(err)
	}
	if err := s.AddProtocolAddress(
		1,
		tcpip.ProtocolAddress{
			Protocol:          ipv6.ProtocolNumber,
			AddressWithPrefix: tcpip.Address(address).WithPrefix(),
		},
		stack.AddressProperties{},
	); err != nil {
		panic(err)
	}
	s.AddRoute(tcpip.Route{
		Destination: header.IPv6EmptySubnet,
		NIC:         1,
	})
	s.AllowICMPMessage()
	go func() {
		var rx int
		var err error
		for {
			rx, err = iprwc.Read(endpoint.readBuf)
			if err != nil {
				log.Println(err)
				break
			}
			pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{
				Data: buffer.NewVectorisedView(rx, []buffer.View{
					buffer.NewViewFromBytes(endpoint.readBuf[:rx]),
				}),
			})
			endpoint.dispatcher.DeliverNetworkPacket("", "", ipv6.ProtocolNumber, pkb)
		}
	}()

	listener, err := endpoint.ListenTCP(&net.TCPAddr{Port: 80})
	if err != nil {
		log.Panicln(err)
	}
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, "Hello from userspace TCP "+request.RemoteAddr)
	})
	httpServer := &http.Server{}
	go httpServer.Serve(listener) // nolint:errcheck

	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)

	select {
	case <-n.Done():
	case <-term:
	}

	n.Close()
}

const IPv6HdrSize = 40

type TCPIPEndpoint struct {
	stack      *stack.Stack
	ipv6rwc    *ipv6rwc.ReadWriteCloser
	dispatcher stack.NetworkDispatcher
	readBuf    []byte
	writeBuf   []byte
}

func (e *TCPIPEndpoint) Attach(dispatcher stack.NetworkDispatcher) { e.dispatcher = dispatcher }

func (e *TCPIPEndpoint) IsAttached() bool { return e.dispatcher != nil }

func (e *TCPIPEndpoint) MTU() uint32 { return uint32(e.ipv6rwc.MTU()) }

func (*TCPIPEndpoint) Capabilities() stack.LinkEndpointCapabilities { return stack.CapabilityNone }

func (*TCPIPEndpoint) MaxHeaderLength() uint16 { return IPv6HdrSize }

func (*TCPIPEndpoint) LinkAddress() tcpip.LinkAddress { return "" }

func (*TCPIPEndpoint) Wait() {}

func (e *TCPIPEndpoint) WritePacket(
	_ stack.RouteInfo,
	_ tcpip.NetworkProtocolNumber,
	pkt *stack.PacketBuffer,
) tcpip.Error {
	vv := buffer.NewVectorisedView(pkt.Size(), pkt.Views())
	n, err := vv.Read(e.writeBuf)
	if err != nil {
		log.Println(err)
		return &tcpip.ErrAborted{}
	}
	_, err = e.ipv6rwc.Write(e.writeBuf[:n])
	if err != nil {
		log.Println(err)
		return &tcpip.ErrAborted{}
	}
	return nil
}

func (e *TCPIPEndpoint) WritePackets(
	stack.RouteInfo,
	stack.PacketBufferList,
	tcpip.NetworkProtocolNumber,
) (int, tcpip.Error) {
	panic("not implemented")
}

func (e *TCPIPEndpoint) WriteRawPacket(*stack.PacketBuffer) tcpip.Error {
	panic("not implemented")
}

func (*TCPIPEndpoint) ARPHardwareType() header.ARPHardwareType {
	return header.ARPHardwareNone
}

func (e *TCPIPEndpoint) AddHeader(tcpip.LinkAddress, tcpip.LinkAddress, tcpip.NetworkProtocolNumber, *stack.PacketBuffer) {
}

func (e *TCPIPEndpoint) Close() error {
	e.stack.RemoveNIC(1)
	e.dispatcher = nil
	return nil
}

func convertToFullAddr(ip net.IP, port int) (tcpip.FullAddress, tcpip.NetworkProtocolNumber) {
	return tcpip.FullAddress{
		NIC:  1,
		Addr: tcpip.Address(ip),
		Port: uint16(port),
	}, ipv6.ProtocolNumber
}

func (e *TCPIPEndpoint) DialTCP(addr *net.TCPAddr) (net.Conn, error) {
	if addr == nil {
		panic("not implemented")
	}
	fa, pn := convertToFullAddr(addr.IP, addr.Port)
	return gonet.DialTCP(e.stack, fa, pn)
}

func (e *TCPIPEndpoint) ListenTCP(addr *net.TCPAddr) (net.Listener, error) {
	if addr == nil {
		panic("not implemented")
	}
	fa, pn := convertToFullAddr(addr.IP, addr.Port)
	return gonet.ListenTCP(e.stack, fa, pn)
}
