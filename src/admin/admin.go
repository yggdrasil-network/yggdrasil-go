package admin

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"

	//"strconv"
	"strings"
	"time"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	//"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	//"github.com/yggdrasil-network/yggdrasil-go/src/util"
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

func (a *AdminSocket) SetupAdminHandlers(na *AdminSocket) {
	a.AddHandler("getSelf", []string{}, func(in Info) (Info, error) {
		ip := a.core.Address().String()
		subnet := a.core.Subnet()
		self := a.core.GetSelf()
		return Info{
			"self": Info{
				ip: Info{
					// TODO"box_pub_key":   a.core.EncryptionPublicKey(),
					"build_name":    version.BuildName(),
					"build_version": version.BuildVersion(),
					"key":           hex.EncodeToString(self.Key[:]),
					"coords":        fmt.Sprintf("%v", self.Coords),
					"subnet":        subnet.String(),
				},
			},
		}, nil
	})
	a.AddHandler("getPeers", []string{}, func(in Info) (Info, error) {
		peers := make(Info)
		for _, p := range a.core.GetPeers() {
			addr := address.AddrForKey(p.Key)
			so := net.IP(addr[:]).String()
			peers[so] = Info{
				"key":    hex.EncodeToString(p.Key[:]),
				"port":   p.Port,
				"coords": fmt.Sprintf("%v", p.Coords),
			}
		}
		return Info{"peers": peers}, nil
	})
	a.AddHandler("getDHT", []string{}, func(in Info) (Info, error) {
		dht := make(Info)
		for _, d := range a.core.GetDHT() {
			addr := address.AddrForKey(d.Key)
			so := net.IP(addr[:]).String()
			dht[so] = Info{
				"key":  hex.EncodeToString(d.Key[:]),
				"port": fmt.Sprintf("%v", d.Port),
				"next": fmt.Sprintf("%v", d.Next),
			}
		}
		return Info{"dht": dht}, nil
	})
	a.AddHandler("getSessions", []string{}, func(in Info) (Info, error) {
		sessions := make(Info)
		for _, s := range a.core.GetSessions() {
			addr := address.AddrForKey(s.Key)
			so := net.IP(addr[:]).String()
			sessions[so] = Info{
				"key": hex.EncodeToString(s.Key[:]),
			}
		}
		return Info{"sessions": sessions}, nil
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
