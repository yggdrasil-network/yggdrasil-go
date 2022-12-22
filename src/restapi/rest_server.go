package restapi

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"archive/zip"
	"time"

	"gerace.dev/zipfs"

	"github.com/RiV-chain/RiV-mesh/src/core"
	"github.com/RiV-chain/RiV-mesh/src/defaults"
	"github.com/RiV-chain/RiV-mesh/src/version"
)

type ServerEvent struct {
	Event string
	Data  string
}

type RestServerCfg struct {
	Core          *core.Core
	Log           core.Logger
	ListenAddress string
	WwwRoot       string
	ConfigFn      string
}

type RestServer struct {
	RestServerCfg
	listenUrl         *url.URL
	serverEvents      chan ServerEvent
	serverEventNextId int
	docFsType         string
}

func NewRestServer(cfg RestServerCfg) (*RestServer, error) {
	a := &RestServer{
		RestServerCfg:     cfg,
		serverEvents:      make(chan ServerEvent, 10),
		serverEventNextId: 0,
	}
	if cfg.ListenAddress == "none" || cfg.ListenAddress == "" {
		return nil, errors.New("listening address isn't configured")
	}

	var err error
	a.listenUrl, err = url.Parse(cfg.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("an error occurred parsing http address: %w", err)
	}

	pakReader, err := zip.OpenReader(cfg.WwwRoot)
	if err == nil {
		defer pakReader.Close()
		fs, err := zipfs.NewZipFileSystem(&pakReader.Reader, zipfs.ServeIndexForMissing())
		if err == nil {
			http.Handle("/", http.FileServer(fs))
			a.docFsType = "zipfs"
		}
	}
	if a.docFsType == "" {
		var nocache = func(fs http.Handler) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				addNoCacheHeaders(w)
				fs.ServeHTTP(w, r)
			}
		}
		http.Handle("/", nocache(http.FileServer(http.Dir(cfg.WwwRoot))))
		a.docFsType = "local fs"
	}

	http.HandleFunc("/api", a.apiHandler)
	http.HandleFunc("/api/self", a.apiSelfHandler)
	http.HandleFunc("/api/peers", a.apiPeersHandler)
	http.HandleFunc("/api/ping", a.apiPingHandler)
	http.HandleFunc("/api/sse", a.apiSseHandler)

	var _ = a.Core.PeersChangedSignal.Connect(func(data interface{}) {
		b, err := a.prepareGetPeers()
		if err != nil {
			a.Log.Errorf("get peers failed: %w", err)
			return
		}

		select {
		case a.serverEvents <- ServerEvent{Event: "peers", Data: string(b)}:
		default:
		}
	})
	return a, nil
}

// Start http server
func (a *RestServer) Serve() error {
	l, e := net.Listen("tcp4", a.listenUrl.Host)
	if e != nil {
		return fmt.Errorf("http server start error: %w", e)
	} else {
		a.Log.Infof("Http server is listening on %s and is supplied from %s %s\n", a.ListenAddress, a.docFsType, a.WwwRoot)
	}
	go func() {
		err := http.Serve(l, nil)
		if err != nil {
			a.Log.Errorln(err)
		}
	}()
	return nil
}

func addNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Add("Pragma", "no-cache")
	w.Header().Add("Expires", "0")
}

func (a *RestServer) apiHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Following methods are allowed: GET /api/self, getpeers. litening")
}

func (a *RestServer) apiSelfHandler(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeaders(w)
	switch r.Method {
	case "GET":
		w.Header().Add("Content-Type", "application/json")
		self := a.Core.GetSelf()
		snet := a.Core.Subnet()
		var result = map[string]interface{}{
			"build_name":    a.Core.GetSelf(),
			"build_version": version.BuildVersion(),
			"key":           hex.EncodeToString(self.Key[:]),
			"private_key":   hex.EncodeToString(self.PrivateKey[:]),
			"address":       a.Core.Address().String(),
			"coords":        self.Coords,
			"subnet":        snet.String(),
		}
		b, err := json.Marshal(result)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, string(b[:]))
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (a *RestServer) prepareGetPeers() ([]byte, error) {
	peers := a.Core.GetPeers()
	response := make([]map[string]interface{}, 0, len(peers))
	for _, p := range peers {
		addr := a.Core.AddrForKey(p.Key)
		response = append(response, map[string]interface{}{
			"address":     net.IP(addr[:]).String(),
			"key":         hex.EncodeToString(p.Key),
			"port":        p.Port,
			"priority":    uint64(p.Priority), // can't be uint8 thanks to gobind
			"coords":      p.Coords,
			"remote":      p.Remote,
			"bytes_recvd": p.RXBytes,
			"bytes_sent":  p.TXBytes,
			"uptime":      p.Uptime.Seconds(),
			"multicast":   strings.Contains(p.Remote, "[fe80::"),
		})
	}
	sort.Slice(response, func(i, j int) bool {
		if !response[i]["multicast"].(bool) && response[j]["multicast"].(bool) {
			return true
		}
		if response[i]["priority"].(uint64) < response[j]["priority"].(uint64) {
			return true
		}
		return response[i]["port"].(uint64) < response[j]["port"].(uint64)
	})
	return json.Marshal(response)
}

func (a *RestServer) apiPeersHandler(w http.ResponseWriter, r *http.Request) {
	var handleDelete = func() error {
		err := a.Core.RemovePeers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return err
	}
	var handlePost = func() error {
		var peers []string
		err := json.NewDecoder(r.Body).Decode(&peers)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return err
		}

		for _, peer := range peers {
			if err := a.Core.AddPeer(peer, ""); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return err
			}
		}

		if len(a.ConfigFn) > 0 {
			saveHeaders := r.Header["Riv-Save-Config"]
			if len(saveHeaders) > 0 && saveHeaders[0] == "true" {
				cfg, err := defaults.ReadConfig(a.ConfigFn)
				if err == nil {
					cfg.Peers = peers
					err := defaults.WriteConfig(a.ConfigFn, cfg)
					if err != nil {
						a.Log.Errorln("Config file read error:", err)
					}
				} else {
					a.Log.Errorln("Config file read error:", err)
				}
			}
		}
		return nil
	}

	addNoCacheHeaders(w)
	switch r.Method {
	case "GET":
		w.Header().Add("Content-Type", "application/json")
		b, err := a.prepareGetPeers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, string(b[:]))
	case "POST":
		_ = handlePost()
	case "PUT":
		if handleDelete() == nil {
			if handlePost() == nil {
				http.Error(w, "No content", http.StatusNoContent)
			}
		}
	case "DELETE":
		if handleDelete() == nil {
			http.Error(w, "No content", http.StatusNoContent)
		}
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (a *RestServer) apiPingHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		peer_list := []string{}

		err := json.NewDecoder(r.Body).Decode(&peer_list)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		go a.ping(peer_list)
		http.Error(w, "Accepted", http.StatusAccepted)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (a *RestServer) apiSseHandler(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeaders(w)
	switch r.Method {
	case "GET":
		w.Header().Add("Content-Type", "text/event-stream")
	Loop:
		for {
			select {
			case v := <-a.serverEvents:
				fmt.Fprintln(w, "id:", a.serverEventNextId)
				fmt.Fprintln(w, "event:", v.Event)
				fmt.Fprintln(w, "data:", v.Data)
				fmt.Fprintln(w) //end of event
				a.serverEventNextId += 1
			default:
				break Loop
			}
		}
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (a *RestServer) ping(peers []string) {
	for _, u := range peers {
		go func(u string) {
			data, _ := json.Marshal(map[string]string{"peer": u, "value": strconv.FormatInt(check(u), 10)})
			a.serverEvents <- ServerEvent{Event: "ping", Data: string(data)}
		}(u)
	}
}

func check(peer string) int64 {
	u, e := url.Parse(peer)
	if e != nil {
		return -1
	}
	t := time.Now()
	_, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		return -1
	}
	d := time.Since(t)
	return d.Milliseconds()
}
