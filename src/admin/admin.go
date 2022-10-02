package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"

	"strings"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// TODO: Add authentication

type AdminSocket struct {
	core     *core.Core
	log      core.Logger
	listener net.Listener
	handlers map[string]handler
	done     chan struct{}
	config   struct {
		listenaddr ListenAddress
	}
}

type AdminSocketRequest struct {
	Name      string          `json:"request"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	KeepAlive bool            `json:"keepalive,omitempty"`
}

type AdminSocketResponse struct {
	Status   string          `json:"status"`
	Error    string          `json:"error,omitempty"`
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

type handler struct {
	desc    string              // What does the endpoint do?
	args    []string            // List of human-readable argument names
	handler core.AddHandlerFunc // First is input map, second is output
}

type ListResponse struct {
	List []ListEntry `json:"list"`
}

type ListEntry struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Fields      []string `json:"fields,omitempty"`
}

// AddHandler is called for each admin function to add the handler and help documentation to the API.
func (a *AdminSocket) AddHandler(name, desc string, args []string, handlerfunc core.AddHandlerFunc) error {
	if _, ok := a.handlers[strings.ToLower(name)]; ok {
		return errors.New("handler already exists")
	}
	a.handlers[strings.ToLower(name)] = handler{
		desc:    desc,
		args:    args,
		handler: handlerfunc,
	}
	return nil
}

// Init runs the initial admin setup.
func New(c *core.Core, log core.Logger, opts ...SetupOption) (*AdminSocket, error) {
	a := &AdminSocket{
		core:     c,
		log:      log,
		handlers: make(map[string]handler),
	}
	for _, opt := range opts {
		a._applyOption(opt)
	}
	if a.config.listenaddr == "none" || a.config.listenaddr == "" {
		return nil, nil
	}
	_ = a.AddHandler("list", "List available commands", []string{}, func(_ json.RawMessage) (interface{}, error) {
		res := &ListResponse{}
		for name, handler := range a.handlers {
			res.List = append(res.List, ListEntry{
				Command:     name,
				Description: handler.desc,
				Fields:      handler.args,
			})
		}
		sort.SliceStable(res.List, func(i, j int) bool {
			return strings.Compare(res.List[i].Command, res.List[j].Command) < 0
		})
		return res, nil
	})
	a.done = make(chan struct{})
	go a.listen()
	return a, a.core.SetAdmin(a)
}

func (a *AdminSocket) SetupAdminHandlers() {
	_ = a.AddHandler(
		"getSelf", "Show details about this node", []string{},
		func(in json.RawMessage) (interface{}, error) {
			req := &GetSelfRequest{}
			res := &GetSelfResponse{}
			if err := json.Unmarshal(in, &req); err != nil {
				return nil, err
			}
			if err := a.getSelfHandler(req, res); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
	_ = a.AddHandler(
		"getPeers", "Show directly connected peers", []string{},
		func(in json.RawMessage) (interface{}, error) {
			req := &GetPeersRequest{}
			res := &GetPeersResponse{}
			if err := json.Unmarshal(in, &req); err != nil {
				return nil, err
			}
			if err := a.getPeersHandler(req, res); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
	_ = a.AddHandler(
		"getDHT", "Show known DHT entries", []string{},
		func(in json.RawMessage) (interface{}, error) {
			req := &GetDHTRequest{}
			res := &GetDHTResponse{}
			if err := json.Unmarshal(in, &req); err != nil {
				return nil, err
			}
			if err := a.getDHTHandler(req, res); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
	_ = a.AddHandler(
		"getPaths", "Show established paths through this node", []string{},
		func(in json.RawMessage) (interface{}, error) {
			req := &GetPathsRequest{}
			res := &GetPathsResponse{}
			if err := json.Unmarshal(in, &req); err != nil {
				return nil, err
			}
			if err := a.getPathsHandler(req, res); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
	_ = a.AddHandler(
		"getSessions", "Show established traffic sessions with remote nodes", []string{},
		func(in json.RawMessage) (interface{}, error) {
			req := &GetSessionsRequest{}
			res := &GetSessionsResponse{}
			if err := json.Unmarshal(in, &req); err != nil {
				return nil, err
			}
			if err := a.getSessionsHandler(req, res); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
	_ = a.AddHandler(
		"addPeer", "Add a peer to the peer list", []string{"uri", "interface"},
		func(in json.RawMessage) (interface{}, error) {
			req := &AddPeerRequest{}
			res := &AddPeerResponse{}
			if err := json.Unmarshal(in, &req); err != nil {
				return nil, err
			}
			if err := a.addPeerHandler(req, res); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
	_ = a.AddHandler(
		"removePeer", "Remove a peer from the peer list", []string{"uri", "interface"},
		func(in json.RawMessage) (interface{}, error) {
			req := &RemovePeerRequest{}
			res := &RemovePeerResponse{}
			if err := json.Unmarshal(in, &req); err != nil {
				return nil, err
			}
			if err := a.removePeerHandler(req, res); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
	//_ = a.AddHandler("getNodeInfo", []string{"key"}, t.proto.nodeinfo.nodeInfoAdminHandler)
	//_ = a.AddHandler("debug_remoteGetSelf", []string{"key"}, t.proto.getSelfHandler)
	//_ = a.AddHandler("debug_remoteGetPeers", []string{"key"}, t.proto.getPeersHandler)
	//_ = a.AddHandler("debug_remoteGetDHT", []string{"key"}, t.proto.getDHTHandler)
}

// IsStarted returns true if the module has been started.
func (a *AdminSocket) IsStarted() bool {
	select {
	case <-a.done:
		// Not blocking, so we're not currently running
		return false
	default:
		// Blocked, so we must have started
		return true
	}
}

// Stop will stop the admin API and close the socket.
func (a *AdminSocket) Stop() error {
	if a == nil {
		return nil
	}
	if a.listener != nil {
		select {
		case <-a.done:
		default:
			close(a.done)
		}
		return a.listener.Close()
	}
	return nil
}

// listen is run by start and manages API connections.
func (a *AdminSocket) listen() {
	listenaddr := string(a.config.listenaddr)
	u, err := url.Parse(listenaddr)
	if err == nil {
		switch strings.ToLower(u.Scheme) {
		case "unix":
			if _, err := os.Stat(listenaddr[7:]); err == nil {
				a.log.Debugln("Admin socket", listenaddr[7:], "already exists, trying to clean up")
				if _, err := net.DialTimeout("unix", listenaddr[7:], time.Second*2); err == nil || err.(net.Error).Timeout() {
					a.log.Errorln("Admin socket", listenaddr[7:], "already exists and is in use by another process")
					os.Exit(1)
				} else {
					if err := os.Remove(listenaddr[7:]); err == nil {
						a.log.Debugln(listenaddr[7:], "was cleaned up")
					} else {
						a.log.Errorln(listenaddr[7:], "already exists and was not cleaned up:", err)
						os.Exit(1)
					}
				}
			}
			a.listener, err = net.Listen("unix", listenaddr[7:])
			if err == nil {
				switch listenaddr[7:8] {
				case "@": // maybe abstract namespace
				default:
					if err := os.Chmod(listenaddr[7:], 0660); err != nil {
						a.log.Warnln("WARNING:", listenaddr[:7], "may have unsafe permissions!")
					}
				}
			}
		case "tcp":
			a.listener, err = net.Listen("tcp", u.Host)
		default:
			// err = errors.New(fmt.Sprint("protocol not supported: ", u.Scheme))
			a.listener, err = net.Listen("tcp", listenaddr)
		}
	} else {
		a.listener, err = net.Listen("tcp", listenaddr)
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
		} else {
			select {
			case <-a.done:
				// Not blocked, so we havent started or already stopped
				return
			default:
				// Blocked, so we're supposed to keep running
			}
		}
	}
}

// handleRequest calls the request handler for each request sent to the admin API.
func (a *AdminSocket) handleRequest(conn net.Conn) {
	decoder := json.NewDecoder(conn)
	decoder.DisallowUnknownFields()

	encoder := json.NewEncoder(conn)
	encoder.SetIndent("", "  ")

	defer conn.Close()

	defer func() {
		r := recover()
		if r != nil {
			a.log.Debugln("Admin socket error:", r)
			if err := encoder.Encode(&ErrorResponse{
				Error: "Check your syntax and input types",
			}); err != nil {
				a.log.Debugln("Admin socket JSON encode error:", err)
			}
			conn.Close()
		}
	}()

	for {
		var err error
		var buf json.RawMessage
		var req AdminSocketRequest
		var resp AdminSocketResponse
		if err := func() error {
			if err = decoder.Decode(&buf); err != nil {
				return fmt.Errorf("Failed to find request")
			}
			if err = json.Unmarshal(buf, &req); err != nil {
				return fmt.Errorf("Failed to unmarshal request")
			}
			if req.Name == "" {
				return fmt.Errorf("No request specified")
			}
			reqname := strings.ToLower(req.Name)
			handler, ok := a.handlers[reqname]
			if !ok {
				return fmt.Errorf("Unknown action '%s', try 'list' for help", reqname)
			}
			res, err := handler.handler(req.Arguments)
			if err != nil {
				return err
			}
			if resp.Response, err = json.Marshal(res); err != nil {
				return fmt.Errorf("Failed to marshal response: %w", err)
			}
			resp.Status = "success"
			return nil
		}(); err != nil {
			resp.Status = "error"
			resp.Error = err.Error()
		}
		if err = encoder.Encode(resp); err != nil {
			a.log.Debugln("Encode error:", err)
		}
		if !req.KeepAlive {
			break
		} else {
			continue
		}
	}
}

type DataUnit uint64

func (d DataUnit) String() string {
	switch {
	case d > 1024*1024*1024*1024:
		return fmt.Sprintf("%2.ftb", float64(d)/1024/1024/1024/1024)
	case d > 1024*1024*1024:
		return fmt.Sprintf("%2.fgb", float64(d)/1024/1024/1024)
	case d > 1024*1024:
		return fmt.Sprintf("%2.fmb", float64(d)/1024/1024)
	default:
		return fmt.Sprintf("%2.fkb", float64(d)/1024)
	}
}
