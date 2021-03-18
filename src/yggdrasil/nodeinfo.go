package yggdrasil

import (
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"time"

	"github.com/Arceliar/phony"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

type nodeinfo struct {
	phony.Inbox
	core       *Core
	myNodeInfo NodeInfoPayload
	callbacks  map[crypto.BoxPubKey]nodeinfoCallback
	cache      map[crypto.BoxPubKey]nodeinfoCached
	table      *lookupTable
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
	m.Act(nil, func() {
		m._init(core)
	})
}

func (m *nodeinfo) _init(core *Core) {
	m.core = core
	m.callbacks = make(map[crypto.BoxPubKey]nodeinfoCallback)
	m.cache = make(map[crypto.BoxPubKey]nodeinfoCached)

	m._cleanup()
}

func (m *nodeinfo) _cleanup() {
	for boxPubKey, callback := range m.callbacks {
		if time.Since(callback.created) > time.Minute {
			delete(m.callbacks, boxPubKey)
		}
	}
	for boxPubKey, cache := range m.cache {
		if time.Since(cache.created) > time.Hour {
			delete(m.cache, boxPubKey)
		}
	}
	time.AfterFunc(time.Second*30, func() {
		m.Act(nil, m._cleanup)
	})
}

// Add a callback for a nodeinfo lookup
func (m *nodeinfo) addCallback(sender crypto.BoxPubKey, call func(nodeinfo *NodeInfoPayload)) {
	m.Act(nil, func() {
		m._addCallback(sender, call)
	})
}

func (m *nodeinfo) _addCallback(sender crypto.BoxPubKey, call func(nodeinfo *NodeInfoPayload)) {
	m.callbacks[sender] = nodeinfoCallback{
		created: time.Now(),
		call:    call,
	}
}

// Handles the callback, if there is one
func (m *nodeinfo) _callback(sender crypto.BoxPubKey, nodeinfo NodeInfoPayload) {
	if callback, ok := m.callbacks[sender]; ok {
		callback.call(&nodeinfo)
		delete(m.callbacks, sender)
	}
}

// Get the current node's nodeinfo
func (m *nodeinfo) getNodeInfo() (p NodeInfoPayload) {
	phony.Block(m, func() {
		p = m._getNodeInfo()
	})
	return
}

func (m *nodeinfo) _getNodeInfo() NodeInfoPayload {
	return m.myNodeInfo
}

// Set the current node's nodeinfo
func (m *nodeinfo) setNodeInfo(given interface{}, privacy bool) (err error) {
	phony.Block(m, func() {
		err = m._setNodeInfo(given, privacy)
	})
	return
}

func (m *nodeinfo) _setNodeInfo(given interface{}, privacy bool) error {
	defaults := map[string]interface{}{
		"buildname":     version.BuildName(),
		"buildversion":  version.BuildVersion(),
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
	newjson, err := json.Marshal(newnodeinfo)
	if err == nil {
		if len(newjson) > 16384 {
			return errors.New("NodeInfo exceeds max length of 16384 bytes")
		}
		m.myNodeInfo = newjson
		return nil
	}
	return err
}

// Add nodeinfo into the cache for a node
func (m *nodeinfo) _addCachedNodeInfo(key crypto.BoxPubKey, payload NodeInfoPayload) {
	m.cache[key] = nodeinfoCached{
		created: time.Now(),
		payload: payload,
	}
}

// Get a nodeinfo entry from the cache
func (m *nodeinfo) _getCachedNodeInfo(key crypto.BoxPubKey) (NodeInfoPayload, error) {
	if nodeinfo, ok := m.cache[key]; ok {
		return nodeinfo.payload, nil
	}
	return NodeInfoPayload{}, errors.New("No cache entry found")
}

// Handles a nodeinfo request/response - called from the router
func (m *nodeinfo) handleNodeInfo(from phony.Actor, nodeinfo *nodeinfoReqRes) {
	m.Act(from, func() {
		m._handleNodeInfo(nodeinfo)
	})
}

func (m *nodeinfo) _handleNodeInfo(nodeinfo *nodeinfoReqRes) {
	if nodeinfo.IsResponse {
		m._callback(nodeinfo.SendPermPub, nodeinfo.NodeInfo)
		m._addCachedNodeInfo(nodeinfo.SendPermPub, nodeinfo.NodeInfo)
	} else {
		m._sendNodeInfo(nodeinfo.SendPermPub, nodeinfo.SendCoords, true)
	}
}

// Send nodeinfo request or response - called from the router
func (m *nodeinfo) sendNodeInfo(key crypto.BoxPubKey, coords []byte, isResponse bool) {
	m.Act(nil, func() {
		m._sendNodeInfo(key, coords, isResponse)
	})
}

func (m *nodeinfo) _sendNodeInfo(key crypto.BoxPubKey, coords []byte, isResponse bool) {
	loc := m.table.self
	nodeinfo := nodeinfoReqRes{
		SendCoords: loc.getCoords(),
		IsResponse: isResponse,
		NodeInfo:   m._getNodeInfo(),
	}
	bs := nodeinfo.encode()
	shared := m.core.router.sessions.getSharedKey(&m.core.boxPriv, &key)
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
