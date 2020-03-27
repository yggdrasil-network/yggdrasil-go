package dhtcrawler

import (
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

const (
	DefaultRetryCount int           = 5
	DefaultExpiration time.Duration = 5 * time.Minute
)

type ApiCache struct {
	networkMap      admin.Info
	networkMapMutex sync.RWMutex
	expiration      int64
}

func (a *ApiCache) Get(crawler *Crawler) admin.Info {
	a.networkMapMutex.Lock()
	defer a.networkMapMutex.Unlock()

	if a.networkMap == nil || time.Now().UnixNano() > a.expiration {
		json_value := crawler.getNetworkMap()
		a.networkMap = admin.Info{"networkMap": json_value}
		a.expiration = time.Now().Add(DefaultExpiration).UnixNano()
	}

	return a.networkMap
}

type Crawler struct {
	core    *yggdrasil.Core
	config  *config.NodeState
	log     *log.Logger
	started bool

	apiCache          ApiCache
	dhtWaitGroup      sync.WaitGroup
	dhtVisited        map[string]attempt
	dhtMutex          sync.RWMutex
	nodeInfoWaitGroup sync.WaitGroup
	nodeInfoVisited   map[string]interface{}
	nodeInfoMutex     sync.RWMutex
}

// This is the structure that we marshal at the end into JSON results
type results struct {
	Meta struct {
		GeneratedAtUTC     int64 `json:"generated_at_utc"`
		NodesAttempted     int   `json:"nodes_attempted"`
		NodesSuccessful    int   `json:"nodes_successful"`
		NodesFailed        int   `json:"nodes_failed"`
		NodeInfoSuccessful int   `json:"nodeinfo_successful"`
		NodeInfoFailed     int   `json:"nodeinfo_failed"`
	} `json:"meta"`
	Topology *map[string]attempt     `json:"topology"`
	NodeInfo *map[string]interface{} `json:"nodeinfo"`
}

type attempt struct {
	NodeID     string   `json:"node_id"`     // the node ID
	IPv6Addr   string   `json:"ipv6_addr"`   // the node address
	IPv6Subnet string   `json:"ipv6_subnet"` // the node subnet
	Coords     []uint64 `json:"coords"`      // the coordinates of the node
	Found      bool     `json:"found"`       // has a search for this node completed successfully?
}

func (s *Crawler) Init(core *yggdrasil.Core, config *config.NodeState, log *log.Logger, options interface{}) error {
	s.core = core
	s.config = config
	s.log = log
	s.started = false

	return nil
}

func (s *Crawler) Stop() error {
	return nil
}

func (s *Crawler) Start() error {
	return nil
}

func (s *Crawler) IsStarted() bool {
	return s.started
}

func (s *Crawler) UpdateConfig(config *config.NodeConfig) {}

func (s *Crawler) SetupAdminHandlers(a *admin.AdminSocket) {
	a.AddHandler("getNetworkMap", []string{}, func(in admin.Info) (admin.Info, error) {
		return s.apiCache.Get(s), nil
	})
}

func (s *Crawler) getNetworkMap() results {
	starttime := time.Now()
	s.dhtVisited = make(map[string]attempt)
	s.nodeInfoVisited = make(map[string]interface{})

	if key, err := hex.DecodeString(s.core.EncryptionPublicKey()); err == nil {
		var pubkey crypto.BoxPubKey
		copy(pubkey[:], key)
		s.dhtWaitGroup.Add(1)
		go s.dhtPing(pubkey, s.core.Coords())
	} else {
		panic("failed to decode pub key")
	}

	s.dhtWaitGroup.Wait()
	s.nodeInfoWaitGroup.Wait()

	s.dhtMutex.Lock()
	defer s.dhtMutex.Unlock()
	s.nodeInfoMutex.Lock()
	defer s.nodeInfoMutex.Unlock()

	s.log.Infoln("The crawl took", time.Since(starttime))

	attempted := len(s.dhtVisited)
	found := 0
	for _, attempt := range s.dhtVisited {
		if attempt.Found {
			found++
		}
	}

	res := results{
		Topology: &s.dhtVisited,
		NodeInfo: &s.nodeInfoVisited,
	}
	res.Meta.GeneratedAtUTC = time.Now().UTC().Unix()
	res.Meta.NodeInfoSuccessful = len(s.nodeInfoVisited)
	res.Meta.NodeInfoFailed = found - len(s.nodeInfoVisited)
	res.Meta.NodesAttempted = attempted
	res.Meta.NodesSuccessful = found
	res.Meta.NodesFailed = attempted - found

	return res
}

func (s *Crawler) dhtPing(pubkey crypto.BoxPubKey, coords []uint64) {
	// Notify the main goroutine that we're done working
	defer s.dhtWaitGroup.Done()

	// Generate useful information about the node, such as it's node ID, address
	// and subnet
	key := hex.EncodeToString(pubkey[:])
	nodeid := crypto.GetNodeID(&pubkey)
	addr := net.IP(address.AddrForNodeID(nodeid)[:])
	upper := append(address.SubnetForNodeID(nodeid)[:], 0, 0, 0, 0, 0, 0, 0, 0)
	subnet := net.IPNet{IP: upper, Mask: net.CIDRMask(64, 128)}

	// If we already have an entry of this node then we should stop what we're
	// doing - it either means that a search is already taking place, or that we
	// have already processed this node
	s.dhtMutex.RLock()
	if info := s.dhtVisited[key]; info.Found {
		s.dhtMutex.RUnlock()
		return
	}
	s.dhtMutex.RUnlock()

	// Make a record of this node and the coordinates so that future goroutines
	// started by a rumour of this node will not repeat this search
	var res yggdrasil.DHTRes
	var err error
	for idx := 0; idx < DefaultRetryCount; idx++ {
		// Randomized delay between attempts, increases exponentially
		time.Sleep(time.Millisecond * time.Duration(rand.Intn(1000)*(1<<idx)))
		// Send out a DHT ping request into the network
		res, err = s.core.DHTPing(
			pubkey,
			coords,
			&crypto.NodeID{},
		)
		if err == nil {
			break
		}
	}

	// Write our new information into the list of visited DHT nodes
	info := attempt{
		NodeID:     nodeid.String(),
		IPv6Addr:   addr.String(),
		IPv6Subnet: subnet.String(),
		Coords:     coords,
		Found:      err == nil,
	}

	// If we successfully found the node then update dhtVisited to say so
	s.dhtMutex.Lock()
	defer s.dhtMutex.Unlock()
	oldInfo := s.dhtVisited[key]
	if info.Found || !oldInfo.Found {
		s.dhtVisited[key] = info
	}

	// If this was the first response from this node then request nodeinfo
	if !oldInfo.Found && info.Found {
		s.nodeInfoWaitGroup.Add(1)
		go s.nodeInfo(pubkey, coords)
	} else {
		// This isn't our first response from the node, so don't do anything
		return
	}

	// Start new DHT search goroutines based on the rumours included in the DHT
	// ping response
	for _, info := range res.Infos {
		s.dhtWaitGroup.Add(1)
		go s.dhtPing(info.PublicKey, info.Coords)
	}
}

func (s *Crawler) nodeInfo(pubkey crypto.BoxPubKey, coords []uint64) {
	// Notify the main goroutine that we're done working
	defer s.nodeInfoWaitGroup.Done()

	// Store information that says that we attempted to query this node for
	// nodeinfo
	key := hex.EncodeToString(pubkey[:])
	s.nodeInfoMutex.RLock()
	if _, ok := s.nodeInfoVisited[key]; ok {
		s.nodeInfoMutex.RUnlock()
		return
	}
	s.nodeInfoMutex.RUnlock()

	// send the nodeinfo request to the given coordinates
	var res yggdrasil.NodeInfoPayload
	var err error
	for idx := 0; idx < DefaultRetryCount; idx++ {
		time.Sleep(time.Millisecond * time.Duration(rand.Intn(1000)*(1<<idx)))
		res, err = s.core.GetNodeInfo(pubkey, coords, false)
		if err == nil {
			break
		}
	}
	if err != nil {
		return
	}

	s.nodeInfoMutex.Lock()
	defer s.nodeInfoMutex.Unlock()

	// If we received nodeinfo back then try to unmarshal it and store it in our
	// received nodeinfos
	var j interface{}
	if err := json.Unmarshal(res, &j); err != nil {
		s.log.Debugln(err)
	} else {
		s.nodeInfoVisited[key] = j
	}
}
