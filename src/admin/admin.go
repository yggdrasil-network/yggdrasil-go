package admin

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

// TODO: Add authentication

type AdminSocket struct {
	core       *yggdrasil.Core
	log        *log.Logger
	listenaddr string
	listener   net.Listener
	handlers   map[string]handler
	started    bool
}

// Info refers to information that is returned to the admin socket handler.
type Info map[string]interface{}

type handler struct {
	args    []string                 // List of human-readable argument names
	handler func(Info) (Info, error) // First is input map, second is output
}

// AddHandler is called for each admin function to add the handler and help documentation to the API.
func (a *AdminSocket) AddHandler(name string, args []string, handlerfunc func(Info) (Info, error)) error {
	if _, ok := a.handlers[strings.ToLower(name)]; ok {
		return errors.New("handler already exists")
	}
	a.handlers[strings.ToLower(name)] = handler{
		args:    args,
		handler: handlerfunc,
	}
	return nil
}

// Init runs the initial admin setup.
func (a *AdminSocket) Init(c *yggdrasil.Core, state *config.NodeState, log *log.Logger, options interface{}) error {
	a.core = c
	a.log = log
	a.handlers = make(map[string]handler)
	current := state.GetCurrent()
	a.listenaddr = current.AdminListen
	a.AddHandler("list", []string{}, func(in Info) (Info, error) {
		handlers := make(map[string]interface{})
		for handlername, handler := range a.handlers {
			handlers[handlername] = Info{"fields": handler.args}
		}
		return Info{"list": handlers}, nil
	})
	return nil
}

func (a *AdminSocket) UpdateConfig(config *config.NodeConfig) {
	a.log.Debugln("Reloading admin configuration...")
	if a.listenaddr != config.AdminListen {
		a.listenaddr = config.AdminListen
		if a.IsStarted() {
			a.Stop()
		}
		a.Start()
	}
}

func (a *AdminSocket) SetupAdminHandlers(na *AdminSocket) {
	a.AddHandler("getSelf", []string{}, func(in Info) (Info, error) {
		ip := a.core.Address().String()
		subnet := a.core.Subnet()
		return Info{
			"self": Info{
				ip: Info{
					"box_pub_key":   a.core.EncryptionPublicKey(),
					"build_name":    version.BuildName(),
					"build_version": version.BuildVersion(),
					"coords":        fmt.Sprintf("%v", a.core.Coords()),
					"subnet":        subnet.String(),
				},
			},
		}, nil
	})
	a.AddHandler("getPeers", []string{}, func(in Info) (Info, error) {
		peers := make(Info)
		for _, p := range a.core.GetPeers() {
			addr := *address.AddrForNodeID(crypto.GetNodeID(&p.PublicKey))
			so := net.IP(addr[:]).String()
			peers[so] = Info{
				"port":        p.Port,
				"uptime":      p.Uptime.Seconds(),
				"bytes_sent":  p.BytesSent,
				"bytes_recvd": p.BytesRecvd,
				"proto":       p.Protocol,
				"endpoint":    p.Endpoint,
				"box_pub_key": hex.EncodeToString(p.PublicKey[:]),
			}
		}
		return Info{"peers": peers}, nil
	})
	a.AddHandler("getSwitchPeers", []string{}, func(in Info) (Info, error) {
		switchpeers := make(Info)
		for _, s := range a.core.GetSwitchPeers() {
			addr := *address.AddrForNodeID(crypto.GetNodeID(&s.PublicKey))
			so := fmt.Sprint(s.Port)
			switchpeers[so] = Info{
				"ip":          net.IP(addr[:]).String(),
				"coords":      fmt.Sprintf("%v", s.Coords),
				"port":        s.Port,
				"bytes_sent":  s.BytesSent,
				"bytes_recvd": s.BytesRecvd,
				"proto":       s.Protocol,
				"endpoint":    s.Endpoint,
				"box_pub_key": hex.EncodeToString(s.PublicKey[:]),
			}
		}
		return Info{"switchpeers": switchpeers}, nil
	})
	/*
		a.AddHandler("getSwitchQueues", []string{}, func(in Info) (Info, error) {
			queues := a.core.GetSwitchQueues()
			return Info{"switchqueues": queues.asMap()}, nil
		})
	*/
	a.AddHandler("getDHT", []string{}, func(in Info) (Info, error) {
		dht := make(Info)
		for _, d := range a.core.GetDHT() {
			addr := *address.AddrForNodeID(crypto.GetNodeID(&d.PublicKey))
			so := net.IP(addr[:]).String()
			dht[so] = Info{
				"coords":      fmt.Sprintf("%v", d.Coords),
				"last_seen":   d.LastSeen.Seconds(),
				"box_pub_key": hex.EncodeToString(d.PublicKey[:]),
			}
		}
		return Info{"dht": dht}, nil
	})
	a.AddHandler("getSessions", []string{}, func(in Info) (Info, error) {
		sessions := make(Info)
		for _, s := range a.core.GetSessions() {
			addr := *address.AddrForNodeID(crypto.GetNodeID(&s.PublicKey))
			so := net.IP(addr[:]).String()
			sessions[so] = Info{
				"coords":        fmt.Sprintf("%v", s.Coords),
				"bytes_sent":    s.BytesSent,
				"bytes_recvd":   s.BytesRecvd,
				"mtu":           s.MTU,
				"uptime":        s.Uptime.Seconds(),
				"was_mtu_fixed": s.WasMTUFixed,
				"box_pub_key":   hex.EncodeToString(s.PublicKey[:]),
			}
		}
		return Info{"sessions": sessions}, nil
	})
	a.AddHandler("addPeer", []string{"uri", "[interface]"}, func(in Info) (Info, error) {
		// Set sane defaults
		intf := ""
		// Has interface been specified?
		if itf, ok := in["interface"]; ok {
			intf = itf.(string)
		}
		if a.core.AddPeer(in["uri"].(string), intf) == nil {
			return Info{
				"added": []string{
					in["uri"].(string),
				},
			}, nil
		}
		return Info{
			"not_added": []string{
				in["uri"].(string),
			},
		}, errors.New("Failed to add peer")
	})
	a.AddHandler("disconnectPeer", []string{"port"}, func(in Info) (Info, error) {
		port, err := strconv.ParseInt(fmt.Sprint(in["port"]), 10, 64)
		if err != nil {
			return Info{}, err
		}
		if a.core.DisconnectPeer(uint64(port)) == nil {
			return Info{
				"disconnected": []string{
					fmt.Sprint(port),
				},
			}, nil
		} else {
			return Info{
				"not_disconnected": []string{
					fmt.Sprint(port),
				},
			}, errors.New("Failed to disconnect peer")
		}
	})
	a.AddHandler("removePeer", []string{"uri", "[interface]"}, func(in Info) (Info, error) {
		// Set sane defaults
		intf := ""
		// Has interface been specified?
		if itf, ok := in["interface"]; ok {
			intf = itf.(string)
		}
		if a.core.RemovePeer(in["uri"].(string), intf) == nil {
			return Info{
				"removed": []string{
					in["uri"].(string),
				},
			}, nil
		} else {
			return Info{
				"not_removed": []string{
					in["uri"].(string),
				},
			}, errors.New("Failed to remove peer")
		}
		return Info{
			"not_removed": []string{
				in["uri"].(string),
			},
		}, errors.New("Failed to remove peer")
	})
	a.AddHandler("getAllowedEncryptionPublicKeys", []string{}, func(in Info) (Info, error) {
		return Info{"allowed_box_pubs": a.core.GetAllowedEncryptionPublicKeys()}, nil
	})
	a.AddHandler("addAllowedEncryptionPublicKey", []string{"box_pub_key"}, func(in Info) (Info, error) {
		if a.core.AddAllowedEncryptionPublicKey(in["box_pub_key"].(string)) == nil {
			return Info{
				"added": []string{
					in["box_pub_key"].(string),
				},
			}, nil
		}
		return Info{
			"not_added": []string{
				in["box_pub_key"].(string),
			},
		}, errors.New("Failed to add allowed key")
	})
	a.AddHandler("removeAllowedEncryptionPublicKey", []string{"box_pub_key"}, func(in Info) (Info, error) {
		if a.core.RemoveAllowedEncryptionPublicKey(in["box_pub_key"].(string)) == nil {
			return Info{
				"removed": []string{
					in["box_pub_key"].(string),
				},
			}, nil
		}
		return Info{
			"not_removed": []string{
				in["box_pub_key"].(string),
			},
		}, errors.New("Failed to remove allowed key")
	})
	a.AddHandler("dhtPing", []string{"box_pub_key", "coords", "[target]"}, func(in Info) (Info, error) {
		var reserr error
		var result yggdrasil.DHTRes
		if in["target"] == nil {
			in["target"] = "none"
		}
		coords := util.DecodeCoordString(in["coords"].(string))
		var boxPubKey crypto.BoxPubKey
		if b, err := hex.DecodeString(in["box_pub_key"].(string)); err == nil {
			copy(boxPubKey[:], b)
			if n, err := hex.DecodeString(in["target"].(string)); err == nil {
				var targetNodeID crypto.NodeID
				copy(targetNodeID[:], n)
				result, reserr = a.core.DHTPing(boxPubKey, coords, &targetNodeID)
			} else {
				result, reserr = a.core.DHTPing(boxPubKey, coords, nil)
			}
		} else {
			return Info{}, err
		}
		if reserr != nil {
			return Info{}, reserr
		}
		infos := make(map[string]map[string]string, len(result.Infos))
		for _, dinfo := range result.Infos {
			info := map[string]string{
				"box_pub_key": hex.EncodeToString(dinfo.PublicKey[:]),
				"coords":      fmt.Sprintf("%v", dinfo.Coords),
			}
			addr := net.IP(address.AddrForNodeID(crypto.GetNodeID(&dinfo.PublicKey))[:]).String()
			infos[addr] = info
		}
		return Info{"nodes": infos}, nil
	})
	a.AddHandler("getNodeInfo", []string{"[box_pub_key]", "[coords]", "[nocache]"}, func(in Info) (Info, error) {
		var nocache bool
		if in["nocache"] != nil {
			nocache = in["nocache"].(string) == "true"
		}
		var boxPubKey crypto.BoxPubKey
		var coords []uint64
		if in["box_pub_key"] == nil && in["coords"] == nil {
			nodeinfo := a.core.MyNodeInfo()
			var jsoninfo interface{}
			if err := json.Unmarshal(nodeinfo, &jsoninfo); err != nil {
				return Info{}, err
			}
			return Info{"nodeinfo": jsoninfo}, nil
		} else if in["box_pub_key"] == nil || in["coords"] == nil {
			return Info{}, errors.New("Expecting both box_pub_key and coords")
		} else {
			if b, err := hex.DecodeString(in["box_pub_key"].(string)); err == nil {
				copy(boxPubKey[:], b)
			} else {
				return Info{}, err
			}
			coords = util.DecodeCoordString(in["coords"].(string))
		}
		result, err := a.core.GetNodeInfo(boxPubKey, coords, nocache)
		if err == nil {
			var m map[string]interface{}
			if err = json.Unmarshal(result, &m); err == nil {
				return Info{"nodeinfo": m}, nil
			}
			return Info{}, err
		}
		return Info{}, err
	})
}

// Start runs the admin API socket to listen for / respond to admin API calls.
func (a *AdminSocket) Start() error {
	if a.listenaddr != "none" && a.listenaddr != "" {
		go a.listen()
		a.started = true
	}
	return nil
}

// IsStarted returns true if the module has been started.
func (a *AdminSocket) IsStarted() bool {
	return a.started
}

// Stop will stop the admin API and close the socket.
func (a *AdminSocket) Stop() error {
	if a.listener != nil {
		a.started = false
		return a.listener.Close()
	}
	return nil
}

// listen is run by start and manages API connections.
func (a *AdminSocket) listen() {
	u, err := url.Parse(a.listenaddr)
	if err == nil {
		switch strings.ToLower(u.Scheme) {
		case "unix":
			if _, err := os.Stat(a.listenaddr[7:]); err == nil {
				a.log.Debugln("Admin socket", a.listenaddr[7:], "already exists, trying to clean up")
				if _, err := net.DialTimeout("unix", a.listenaddr[7:], time.Second*2); err == nil || err.(net.Error).Timeout() {
					a.log.Errorln("Admin socket", a.listenaddr[7:], "already exists and is in use by another process")
					os.Exit(1)
				} else {
					if err := os.Remove(a.listenaddr[7:]); err == nil {
						a.log.Debugln(a.listenaddr[7:], "was cleaned up")
					} else {
						a.log.Errorln(a.listenaddr[7:], "already exists and was not cleaned up:", err)
						os.Exit(1)
					}
				}
			}
			a.listener, err = net.Listen("unix", a.listenaddr[7:])
			if err == nil {
				switch a.listenaddr[7:8] {
				case "@": // maybe abstract namespace
				default:
					if err := os.Chmod(a.listenaddr[7:], 0660); err != nil {
						a.log.Warnln("WARNING:", a.listenaddr[:7], "may have unsafe permissions!")
					}
				}
			}
		case "tcp":
			a.listener, err = net.Listen("tcp", u.Host)
		default:
			// err = errors.New(fmt.Sprint("protocol not supported: ", u.Scheme))
			a.listener, err = net.Listen("tcp", a.listenaddr)
		}
	} else {
		a.listener, err = net.Listen("tcp", a.listenaddr)
	}
	if err != nil {
		a.log.Errorf("Admin socket failed to listen: %v", err)
		os.Exit(1)
	}
	a.log.Infof("%s admin socket listening on %s",
		strings.ToUpper(a.listener.Addr().Network()),
		a.listener.Addr().String())
	defer a.listener.Close()
	for {
		conn, err := a.listener.Accept()
		if err == nil {
			go a.handleRequest(conn)
		}
	}
}

// handleRequest calls the request handler for each request sent to the admin API.
func (a *AdminSocket) handleRequest(conn net.Conn) {
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	encoder.SetIndent("", "  ")
	recv := make(Info)
	send := make(Info)

	defer func() {
		r := recover()
		if r != nil {
			send = Info{
				"status": "error",
				"error":  "Check your syntax and input types",
			}
			a.log.Debugln("Admin socket error:", r)
			if err := encoder.Encode(&send); err != nil {
				a.log.Debugln("Admin socket JSON encode error:", err)
			}
			conn.Close()
		}
	}()

	for {
		// Start with a clean slate on each request
		recv = Info{}
		send = Info{}

		// Decode the input
		if err := decoder.Decode(&recv); err != nil {
			a.log.Debugln("Admin socket JSON decode error:", err)
			return
		}

		// Send the request back with the response, and default to "error"
		// unless the status is changed below by one of the handlers
		send["request"] = recv
		send["status"] = "error"

		n := strings.ToLower(recv["request"].(string))

		if _, ok := recv["request"]; !ok {
			send["error"] = "No request sent"
			goto respond
		}

		if h, ok := a.handlers[n]; ok {
			// Check that we have all the required arguments
			for _, arg := range h.args {
				// An argument in [square brackets] is optional and not required,
				// so we can safely ignore those
				if strings.HasPrefix(arg, "[") && strings.HasSuffix(arg, "]") {
					continue
				}
				// Check if the field is missing
				if _, ok := recv[arg]; !ok {
					send = Info{
						"status":    "error",
						"error":     "Expected field missing: " + arg,
						"expecting": arg,
					}
					goto respond
				}
			}

			// By this point we should have all the fields we need, so call
			// the handler
			response, err := h.handler(recv)
			if err != nil {
				send["error"] = err.Error()
				if response != nil {
					send["response"] = response
					goto respond
				}
			} else {
				send["status"] = "success"
				if response != nil {
					send["response"] = response
					goto respond
				}
			}
		} else {
			// Start with a clean response on each request, which defaults to an error
			// state. If a handler is found below then this will be overwritten
			send = Info{
				"request": recv,
				"status":  "error",
				"error":   fmt.Sprintf("Unknown action '%s', try 'list' for help", recv["request"].(string)),
			}
			goto respond
		}

		// Send the response back
	respond:
		if err := encoder.Encode(&send); err != nil {
			return
		}

		// If "keepalive" isn't true then close the connection
		if keepalive, ok := recv["keepalive"]; !ok || !keepalive.(bool) {
			conn.Close()
		}
	}
}
