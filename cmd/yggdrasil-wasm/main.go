//go:build wasm

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gologme/log"
	"github.com/hjson/hjson-go/v4"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/ipv6rwc"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"syscall/js"
	"net/http"
	"net/url"
	"io"
	"bufio"
)

type node struct {
	core *core.Core
}

// The main function is responsible for configuring and starting Yggdrasil.
func main() {
	genconf := flag.Bool("genconf", false, "print a new config to stdout")
	useconf := flag.Bool("useconf", false, "read HJSON/JSON config from stdin")
	useconffile := flag.String("useconffile", "", "read HJSON/JSON config from specified file path")
	normaliseconf := flag.Bool("normaliseconf", false, "use in combination with either -useconf or -useconffile, outputs your configuration normalised")
	exportkey := flag.Bool("exportkey", false, "use in combination with either -useconf or -useconffile, outputs your private key in PEM format")
	confjson := flag.Bool("json", false, "print configuration from -genconf or -normaliseconf as JSON instead of HJSON")
	autoconf := flag.Bool("autoconf", false, "automatic mode (dynamic IP, peer with IPv6 neighbors)")
	ver := flag.Bool("version", false, "prints the version of this build")
	logto := flag.String("logto", "stdout", "file path to log to, \"syslog\" or \"stdout\"")
	getaddr := flag.Bool("address", false, "use in combination with either -useconf or -useconffile, outputs your IPv6 address")
	getsnet := flag.Bool("subnet", false, "use in combination with either -useconf or -useconffile, outputs your IPv6 subnet")
	getpkey := flag.Bool("publickey", false, "use in combination with either -useconf or -useconffile, outputs your public key")
	loglevel := flag.String("loglevel", "info", "loglevel to enable")
	flag.Parse()

	done := make(chan struct{})
	defer close(done)

	// Catch interrupts from the operating system to exit gracefully.
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// Create a new logger that logs output to stdout.
	var logger *log.Logger
	switch *logto {
	case "stdout":
		logger = log.New(os.Stdout, "", log.Flags())

	default:
		if logfd, err := os.OpenFile(*logto, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			logger = log.New(logfd, "", log.Flags())
		}
	}
	if logger == nil {
		logger = log.New(os.Stdout, "", log.Flags())
		logger.Warnln("Logging defaulting to stdout")
	}
	if *normaliseconf {
		setLogLevel("error", logger)
	} else {
		setLogLevel(*loglevel, logger)
	}

	cfg := config.GenerateConfig()
	var err error
	switch {
	case *ver:
		fmt.Println("Build name:", version.BuildName())
		fmt.Println("Build version:", version.BuildVersion())
		return

	case *autoconf:
		// Use an autoconf-generated config, this will give us random keys and
		// port numbers, and will use an automatically selected TUN interface.

	case *useconf:
		if _, err := cfg.ReadFrom(os.Stdin); err != nil {
			panic(err)
		}

	case *useconffile != "":
		f, err := os.Open(*useconffile)
		if err != nil {
			panic(err)
		}
		if _, err := cfg.ReadFrom(f); err != nil {
			panic(err)
		}
		_ = f.Close()

	case *genconf:
		cfg.AdminListen = ""
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

	default:
		fmt.Println("Usage:")
		flag.PrintDefaults()

		if *getaddr || *getsnet {
			fmt.Println("\nError: You need to specify some config data using -useconf or -useconffile.")
		}

		// In Wasm environment, we assume default execution as an application and skip return
		// return
	}

	privateKey := ed25519.PrivateKey(cfg.PrivateKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	switch {
	case *getaddr:
		addr := address.AddrForKey(publicKey)
		ip := net.IP(addr[:])
		fmt.Println(ip.String())
		return

	case *getsnet:
		snet := address.SubnetForKey(publicKey)
		ipnet := net.IPNet{
			IP:   append(snet[:], 0, 0, 0, 0, 0, 0, 0, 0),
			Mask: net.CIDRMask(len(snet)*8, 128),
		}
		fmt.Println(ipnet.String())
		return

	case *getpkey:
		fmt.Println(hex.EncodeToString(publicKey))
		return

	case *normaliseconf:
		cfg.AdminListen = ""
		if cfg.PrivateKeyPath != "" {
			cfg.PrivateKey = nil
		}
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

	case *exportkey:
		pem, err := cfg.MarshalPEMPrivateKey()
		if err != nil {
			panic(err)
		}
		fmt.Println(string(pem))
		return
	}

	n := &node{}

	// Set up the Yggdrasil node itself.
	{
		iprange := net.IPNet{
			IP:   net.ParseIP("200::"),
			Mask: net.CIDRMask(7, 128),
		}
		options := []core.SetupOption{
			core.NodeInfo(cfg.NodeInfo),
			core.NodeInfoPrivacy(cfg.NodeInfoPrivacy),
			core.PeerFilter(func(ip net.IP) bool {
				return !iprange.Contains(ip)
			}),
		}
		// Always hardcode a public WSS peer, ignore config
		cfg.Peers = []string{"wss://yggno.de:12345"}
		cfg.Listen = []string{}
		for _, addr := range cfg.Listen {
			options = append(options, core.ListenAddress(addr))
		}

		for _, peer := range cfg.Peers {
			options = append(options, core.Peer{URI: peer})
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

	// Setup gVisor netstack
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv6.NewProtocolWithOptions(ipv6.Options{DADConfigs: stack.DADConfigurations{DupAddrDetectTransmits: 0}})},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol},
	})

	endpoint := channel.New(256, 1280, "")
	if err := s.CreateNIC(1, endpoint); err != nil {
		panic(fmt.Sprintf("Failed to create NIC: %v", err))
	}

	ipv6Subnet, _ := tcpip.NewSubnet(
		tcpip.AddrFrom16([16]byte{0x02, 0x00, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
		tcpip.MaskFrom("\xfe\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"),
	)
	s.SetRouteTable([]tcpip.Route{
		{
			Destination: ipv6Subnet,
			NIC:         1,
		},
	})

	rwc := ipv6rwc.NewReadWriteCloser(n.core)

	go func() {
		buf := make([]byte, 65535)
		for {
			n, err := rwc.Read(buf)
			if err != nil {
				logger.Printf("rwc.Read error: %v", err)
				return
			}
			if n > 0 {
				b := make([]byte, n)
				copy(b, buf[:n])
				pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
					Payload: buffer.MakeWithData(b),
				})
				endpoint.InjectInbound(ipv6.ProtocolNumber, pkt)
			}
		}
	}()

	go func() {
		for {
			pkt := endpoint.ReadContext(ctx)
			if pkt == nil {
				return
			}
			view := pkt.ToView()
			_, err := rwc.Write(view.AsSlice())
			if err != nil {
				logger.Printf("rwc.Write error: %v", err)
			}
		}
	}()

	js.Global().Set("YggFetch", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return nil
		}

		targetURL := args[0].String()
		method := "GET"
		if len(args) > 1 {
			method = args[1].String()
		}

		var bodyString string
		if len(args) > 2 {
			bodyString = args[2].String()
		}

		promise := js.Global().Get("Promise").New(js.FuncOf(func(this js.Value, resolveArgs []js.Value) any {
			resolve := resolveArgs[0]
			reject := resolveArgs[1]

			go func() {
				parsedURL, err := url.Parse(targetURL)
				if err != nil {
					reject.Invoke(fmt.Sprintf("Invalid URL: %v", err))
					return
				}

				host := parsedURL.Hostname()
				// Remove brackets if it's an IPv6 literal
				host = strings.TrimPrefix(host, "[")
				host = strings.TrimSuffix(host, "]")

				portStr := parsedURL.Port()
				if portStr == "" {
					portStr = "80"
				}

				var port uint16 = 80
				fmt.Sscanf(portStr, "%d", &port)

				parsedIP := net.ParseIP(host)
				if parsedIP == nil {
					reject.Invoke(fmt.Sprintf("Invalid IP address or domain name resolution not supported: %s", host))
					return
				}

				tcpAddr := tcpip.FullAddress{
					NIC:  1,
					Addr: tcpip.AddrFromSlice(parsedIP.To16()),
					Port: port,
				}

				conn, err := gonet.DialTCP(s, tcpAddr, ipv6.ProtocolNumber)
				if err != nil {
					reject.Invoke(fmt.Sprintf("Dial failed: %v", err))
					return
				}
				defer conn.Close()

				req, err := http.NewRequest(method, targetURL, strings.NewReader(bodyString))
				if err != nil {
					reject.Invoke(fmt.Sprintf("Failed to create request: %v", err))
					return
				}

				err = req.Write(conn)
				if err != nil {
					reject.Invoke(fmt.Sprintf("Failed to write request: %v", err))
					return
				}

				resp, err := http.ReadResponse(bufio.NewReader(conn), req)
				if err != nil {
					reject.Invoke(fmt.Sprintf("Failed to read response: %v", err))
					return
				}
				defer resp.Body.Close()

				bodyBytes, err := io.ReadAll(resp.Body)
				if err != nil {
					reject.Invoke(fmt.Sprintf("Failed to read response body: %v", err))
					return
				}

				responseObject := js.Global().Get("Object").New()
				responseObject.Set("status", resp.StatusCode)
				responseObject.Set("statusText", resp.Status)

				headersObj := js.Global().Get("Object").New()
				for k, v := range resp.Header {
					if len(v) > 0 {
						headersObj.Set(k, v[0])
					}
				}
				responseObject.Set("headers", headersObj)

				// Encode body as base64 string because it might be binary
				// Or we can construct an ArrayBuffer or Uint8Array.
				// For simplicity, we create a Uint8Array:
				uint8Array := js.Global().Get("Uint8Array").New(len(bodyBytes))
				js.CopyBytesToJS(uint8Array, bodyBytes)
				responseObject.Set("bodyBytes", uint8Array)

				resolve.Invoke(responseObject)
			}()

			return nil
		}))

		return promise
	}))

	// Block until we are told to shut down.
	<-ctx.Done()

	// Shut down the node.
	n.core.Stop()
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
