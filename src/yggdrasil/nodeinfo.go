package yggdrasil

import (
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

type nodeinfo struct {
	core            *Core
	myNodeInfo      NodeInfoPayload
	myNodeInfoMutex sync.RWMutex
	callbacks       map[crypto.BoxPubKey]nodeinfoCallback
	callbacksMutex  sync.Mutex
	cache           map[crypto.BoxPubKey]nodeinfoCached
	cacheMutex      sync.RWMutex
}

type nodeinfoCached struct {
	payload NodeInfoPayload
	created time.Time
}

type nodeinfoCallback struct {
	call    func(nodeinfo *NodeInfoPayload)
	created time.Time
}

// Represents a session nodeinfo packet.
type nodeinfoReqRes struct {
	SendPermPub crypto.BoxPubKey // Sender's permanent key
	SendCoords  []byte           // Sender's coords
	IsResponse  bool
	NodeInfo    NodeInfoPayload
}

// Initialises the nodeinfo cache/callback maps, and starts a goroutine to keep
// the cache/callback maps clean of stale entries
func (m *nodeinfo) init(core *Core) {
	m.core = core
	m.callbacks = make(map[crypto.BoxPubKey]nodeinfoCallback)
	m.cache = make(map[crypto.BoxPubKey]nodeinfoCached)

	go func() {
		for {
			m.callbacksMutex.Lock()
			for boxPubKey, callback := range m.callbacks {
				if time.Since(callback.created) > time.Minute {
					delete(m.callbacks, boxPubKey)
				}
			}
			m.callbacksMutex.Unlock()
			m.cacheMutex.Lock()
			for boxPubKey, cache := range m.cache {
				if time.Since(cache.created) > time.Hour {
					delete(m.cache, boxPubKey)
				}
			}
			m.cacheMutex.Unlock()
			time.Sleep(time.Second * 30)
		}
	}()
}

// Add a callback for a nodeinfo lookup
func (m *nodeinfo) addCallback(sender crypto.BoxPubKey, call func(nodeinfo *NodeInfoPayload)) {
	m.callbacksMutex.Lock()
	defer m.callbacksMutex.Unlock()
	m.callbacks[sender] = nodeinfoCallback{
		created: time.Now(),
		call:    call,
	}
}

// Handles the callback, if there is one
func (m *nodeinfo) callback(sender crypto.BoxPubKey, nodeinfo NodeInfoPayload) {
	m.callbacksMutex.Lock()
	defer m.callbacksMutex.Unlock()
	if callback, ok := m.callbacks[sender]; ok {
		callback.call(&nodeinfo)
		delete(m.callbacks, sender)
	}
}

// Get the current node's nodeinfo
func (m *nodeinfo) getNodeInfo() NodeInfoPayload {
	m.myNodeInfoMutex.RLock()
	defer m.myNodeInfoMutex.RUnlock()
	return m.myNodeInfo
}

// Set the current node's nodeinfo
func (m *nodeinfo) setNodeInfo(given interface{}, privacy bool) error {
	m.myNodeInfoMutex.Lock()
	defer m.myNodeInfoMutex.Unlock()
	defaults := map[string]interface{}{
		"buildname":     BuildName(),
		"buildversion":  BuildVersion(),
		"buildplatform": runtime.GOOS,
		"buildarch":     runtime.GOARCH,
	}
	newnodeinfo := make(map[string]interface{})
	if !privacy {
		for k, v := range defaults {
			newnodeinfo[k] = v
		}
	}
	if nodeinfomap, ok := given.(map[string]interface{}); ok {
		for key, value := range nodeinfomap {
			if _, ok := defaults[key]; ok {
				if strvalue, strok := value.(string); strok && strings.EqualFold(strvalue, "null") || value == nil {
					delete(newnodeinfo, key)
				}
				continue
			}
			newnodeinfo[key] = value
		}
	}
	if newjson, err := json.Marshal(newnodeinfo); err == nil {
		if len(newjson) > 16384 {
			return errors.New("NodeInfo exceeds max length of 16384 bytes")
		}
		m.myNodeInfo = newjson
		return nil
	} else {
		return err
	}
}

// Add nodeinfo into the cache for a node
func (m *nodeinfo) addCachedNodeInfo(key crypto.BoxPubKey, payload NodeInfoPayload) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()
	m.cache[key] = nodeinfoCached{
		created: time.Now(),
		payload: payload,
	}
}

// Get a nodeinfo entry from the cache
func (m *nodeinfo) getCachedNodeInfo(key crypto.BoxPubKey) (NodeInfoPayload, error) {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()
	if nodeinfo, ok := m.cache[key]; ok {
		return nodeinfo.payload, nil
	}
	return NodeInfoPayload{}, errors.New("No cache entry found")
}

// Handles a nodeinfo request/response - called from the router
func (m *nodeinfo) handleNodeInfo(nodeinfo *nodeinfoReqRes) {
	if nodeinfo.IsResponse {
		m.callback(nodeinfo.SendPermPub, nodeinfo.NodeInfo)
		m.addCachedNodeInfo(nodeinfo.SendPermPub, nodeinfo.NodeInfo)
	} else {
		m.sendNodeInfo(nodeinfo.SendPermPub, nodeinfo.SendCoords, true)
	}
}

// Send nodeinfo request or response - called from the router
func (m *nodeinfo) sendNodeInfo(key crypto.BoxPubKey, coords []byte, isResponse bool) {
	table := m.core.switchTable.table.Load().(lookupTable)
	nodeinfo := nodeinfoReqRes{
		SendCoords: table.self.getCoords(),
		IsResponse: isResponse,
		NodeInfo:   m.getNodeInfo(),
	}
	bs := nodeinfo.encode()
	shared := m.core.sessions.getSharedKey(&m.core.boxPriv, &key)
	payload, nonce := crypto.BoxSeal(shared, bs, nil)
	p := wire_protoTrafficPacket{
		Coords:  coords,
		ToKey:   key,
		FromKey: m.core.boxPub,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	m.core.router.out(packet)
}
