package core

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"runtime"
	"strings"
	"time"

	iwt "github.com/Arceliar/ironwood/types"
	"github.com/Arceliar/phony"

	//"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

// NodeInfoPayload represents a RequestNodeInfo response, in bytes.
type NodeInfoPayload []byte

type nodeinfo struct {
	phony.Inbox
	proto      *protoHandler
	myNodeInfo NodeInfoPayload
	callbacks  map[keyArray]nodeinfoCallback
}

type nodeinfoCallback struct {
	call    func(nodeinfo NodeInfoPayload)
	created time.Time
}

// Initialises the nodeinfo cache/callback maps, and starts a goroutine to keep
// the cache/callback maps clean of stale entries
func (m *nodeinfo) init(proto *protoHandler) {
	m.Act(nil, func() {
		m._init(proto)
	})
}

func (m *nodeinfo) _init(proto *protoHandler) {
	m.proto = proto
	m.callbacks = make(map[keyArray]nodeinfoCallback)
	m._cleanup()
}

func (m *nodeinfo) _cleanup() {
	for boxPubKey, callback := range m.callbacks {
		if time.Since(callback.created) > time.Minute {
			delete(m.callbacks, boxPubKey)
		}
	}
	time.AfterFunc(time.Second*30, func() {
		m.Act(nil, m._cleanup)
	})
}

func (m *nodeinfo) _addCallback(sender keyArray, call func(nodeinfo NodeInfoPayload)) {
	m.callbacks[sender] = nodeinfoCallback{
		created: time.Now(),
		call:    call,
	}
}

// Handles the callback, if there is one
func (m *nodeinfo) _callback(sender keyArray, nodeinfo NodeInfoPayload) {
	if callback, ok := m.callbacks[sender]; ok {
		callback.call(nodeinfo)
		delete(m.callbacks, sender)
	}
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

func (m *nodeinfo) sendReq(from phony.Actor, key keyArray, callback func(nodeinfo NodeInfoPayload)) {
	m.Act(from, func() {
		m._sendReq(key, callback)
	})
}

func (m *nodeinfo) _sendReq(key keyArray, callback func(nodeinfo NodeInfoPayload)) {
	if callback != nil {
		m._addCallback(key, callback)
	}
	_, _ = m.proto.core.PacketConn.WriteTo([]byte{typeSessionProto, typeProtoNodeInfoRequest}, iwt.Addr(key[:]))
}

func (m *nodeinfo) handleReq(from phony.Actor, key keyArray) {
	m.Act(from, func() {
		m._sendRes(key)
	})
}

func (m *nodeinfo) handleRes(from phony.Actor, key keyArray, info NodeInfoPayload) {
	m.Act(from, func() {
		m._callback(key, info)
	})
}

func (m *nodeinfo) _sendRes(key keyArray) {
	bs := append([]byte{typeSessionProto, typeProtoNodeInfoResponse}, m._getNodeInfo()...)
	_, _ = m.proto.core.PacketConn.WriteTo(bs, iwt.Addr(key[:]))
}

// Admin socket stuff

type GetNodeInfoRequest struct {
	Key string `json:"key"`
}
type GetNodeInfoResponse map[string]interface{}

func (m *nodeinfo) nodeInfoAdminHandler(in json.RawMessage) (interface{}, error) {
	var req GetNodeInfoRequest
	if err := json.Unmarshal(in, &req); err != nil {
		return nil, err
	}
	var key keyArray
	var kbs []byte
	var err error
	if kbs, err = hex.DecodeString(req.Key); err != nil {
		return nil, err
	}
	copy(key[:], kbs)
	ch := make(chan []byte, 1)
	m.sendReq(nil, key, func(info NodeInfoPayload) {
		ch <- info
	})
	timer := time.NewTimer(6 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil, errors.New("timeout")
	case info := <-ch:
		var msg json.RawMessage
		if err := msg.UnmarshalJSON(info); err != nil {
			return nil, err
		}
		ip := net.IP(address.AddrForKey(kbs)[:])
		res := GetNodeInfoResponse{ip.String(): msg}
		return res, nil
	}
}
