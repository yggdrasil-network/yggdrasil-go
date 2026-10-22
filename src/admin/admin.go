package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"

	"strings"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// TODO: Add authentication

// maxRequestSize is the maximum number of bytes read from a connection while
// decoding a single request. Admin requests consist of a request name and a
// small argument object, so this leaves a wide margin while keeping the amount
// of memory a client can make the daemon allocate per connection bounded.
const maxRequestSize = 1 << 20 // 1 MiB

// requestReadTimeout is how long the admin socket waits for a request to
// arrive, or for the next request on a keepalive connection, before closing
// the connection. It is a variable rather than a constant so that it can be
// shortened in tests.
var requestReadTimeout = 30 * time.Second

// errRequestTooLarge is returned by limitedReader once a request has grown
// past maxRequestSize.
var errRequestTooLarge = errors.New("admin request too large")

// limitedReader reads at most maxRequestSize bytes per request. Unlike
// io.LimitReader it reports errRequestTooLarge instead of io.EOF when the limit
// is reached, so that an oversized request can be told apart from a truncated
// one. The budget is reset for every request on a keepalive connection.
type limitedReader struct {
	r io.Reader
	n int64
}

func (l *limitedReader) reset() { l.n = maxRequestSize }

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errRequestTooLarge
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

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
	Status   string             `json:"status"`
	Error    string             `json:"error,omitempty"`
	Request  AdminSocketRequest `json:"request"`
	Response json.RawMessage    `json:"response"`
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

	listenaddr := string(a.config.listenaddr)
	u, err := url.Parse(listenaddr)
	if err == nil {
		switch strings.ToLower(u.Scheme) {
		case "unix":
			if _, err := os.Stat(u.Path); err == nil {
				a.log.Debugln("Admin socket", u.Path, "already exists, trying to clean up")
				_, dialErr := net.DialTimeout("unix", u.Path, time.Second*2)
				inUse := dialErr == nil
				if !inUse {
					var netErr net.Error
					if errors.As(dialErr, &netErr) && netErr.Timeout() {
						inUse = true
					}
				}
				if inUse {
					a.log.Errorln("Admin socket", u.Path, "already exists and is in use by another process")
					os.Exit(1)
				} else {
					if err := os.Remove(u.Path); err == nil {
						a.log.Debugln(u.Path, "was cleaned up")
					} else {
						a.log.Errorln(u.Path, "already exists and was not cleaned up:", err)
						os.Exit(1)
					}
				}
			}
			a.listener, err = net.Listen("unix", u.Path)
			if err == nil {
				abstract := u.Path != "" && u.Path[0] == '@'
				if !abstract {
					if err := os.Chmod(u.Path, 0660); err != nil {
						a.log.Warnln("WARNING:", u.Path, "may have unsafe permissions!")
					}
				}
			}
		case "tcp":
			a.listener, err = net.Listen("tcp", u.Host)
		default:
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
		"getPeers", "Show directly connected peers", []string{"sort"},
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
		"getTree", "Show known Tree entries", []string{},
		func(in json.RawMessage) (interface{}, error) {
			req := &GetTreeRequest{}
			res := &GetTreeResponse{}
			if err := json.Unmarshal(in, &req); err != nil {
				return nil, err
			}
			if err := a.getTreeHandler(req, res); err != nil {
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
	limited := &limitedReader{r: conn}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()

	encoder := json.NewEncoder(conn)
	encoder.SetIndent("", "  ")

	defer conn.Close()

	for {
		var err error
		var buf json.RawMessage
		var req AdminSocketRequest
		var resp AdminSocketResponse
		var closeConn bool
		req.Arguments = []byte("{}")
		limited.reset()
		// Give the client a bounded amount of time to deliver the request, so
		// that an idle connection cannot hold a file descriptor and a goroutine
		// forever. The deadline is re-armed for every request on a keepalive
		// connection and only affects reads, not the response write.
		if err := conn.SetReadDeadline(time.Now().Add(requestReadTimeout)); err != nil {
			a.log.Debugln("Failed to set read deadline:", err)
		}
		if err := func() error {
			if err = decoder.Decode(&buf); err != nil {
				closeConn = true
				switch {
				case errors.Is(err, errRequestTooLarge):
					return fmt.Errorf("request too large, limit is %d bytes", maxRequestSize)
				case errors.Is(err, os.ErrDeadlineExceeded):
					return fmt.Errorf("timed out waiting for request")
				default:
					return fmt.Errorf("failed to find request")
				}
			}
			if err = json.Unmarshal(buf, &req); err != nil {
				return fmt.Errorf("failed to unmarshal request")
			}
			resp.Request = req
			if req.Name == "" {
				return fmt.Errorf("no request specified")
			}
			reqname := strings.ToLower(req.Name)
			handler, ok := a.handlers[reqname]
			if !ok {
				return fmt.Errorf("unknown action '%s', try 'list' for help", reqname)
			}
			res, err := handler.handler(req.Arguments)
			if err != nil {
				return err
			}
			if resp.Response, err = json.Marshal(res); err != nil {
				return fmt.Errorf("failed to marshal response: %w", err)
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
		if closeConn || !req.KeepAlive {
			break
		} else {
			continue
		}
	}
}

type DataUnit uint64

func (d DataUnit) String() string {
	switch {
	case d >= 1024*1024*1024*1024:
		return fmt.Sprintf("%2.1fTB", float64(d)/1024/1024/1024/1024)
	case d >= 1024*1024*1024:
		return fmt.Sprintf("%2.1fGB", float64(d)/1024/1024/1024)
	case d >= 1024*1024:
		return fmt.Sprintf("%2.1fMB", float64(d)/1024/1024)
	case d >= 100:
		return fmt.Sprintf("%2.1fKB", float64(d)/1024)
	default:
		return fmt.Sprintf("%dB", d)
	}
}
