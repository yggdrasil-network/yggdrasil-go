package core

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"time"

	iwt "github.com/Arceliar/ironwood/types"
	"github.com/Arceliar/phony"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

type nodeinfo struct {
	phony.Inbox
	proto      *protoHandler
	myNodeInfo json.RawMessage
	callbacks  map[keyArray]nodeinfoCallback
}

type nodeinfoCallback struct {
	call    func(nodeinfo json.RawMessage)
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

func (m *nodeinfo) _addCallback(sender keyArray, call func(nodeinfo json.RawMessage)) {
	m.callbacks[sender] = nodeinfoCallback{
		created: time.Now(),
		call:    call,
	}
}

// Handles the callback, if there is one
func (m *nodeinfo) _callback(sender keyArray, nodeinfo json.RawMessage) {
	if callback, ok := m.callbacks[sender]; ok {
		callback.call(nodeinfo)
		delete(m.callbacks, sender)
	}
}

func (m *nodeinfo) _getNodeInfo() json.RawMessage {
	return m.myNodeInfo
}

// Set the current node's nodeinfo
func (m *nodeinfo) setNodeInfo(given map[string]interface{}, privacy bool) (err error) {
	phony.Block(m, func() {
		err = m._setNodeInfo(given, privacy)
	})
	return
}

func (m *nodeinfo) _setNodeInfo(given map[string]interface{}, privacy bool) error {
	newnodeinfo := make(map[string]interface{}, len(given))
	for k, v := range given {
		newnodeinfo[k] = v
	}
	if !privacy {
		newnodeinfo["buildname"] = version.BuildName()
		newnodeinfo["buildversion"] = version.BuildVersion()
		newnodeinfo["buildplatform"] = runtime.GOOS
		newnodeinfo["buildarch"] = runtime.GOARCH
	}
	newjson, err := json.Marshal(newnodeinfo)
	switch {
	case err != nil:
		return fmt.Errorf("NodeInfo marshalling failed: %w", err)
	case len(newjson) > 16384:
		return fmt.Errorf("NodeInfo exceeds max length of 16384 bytes")
	default:
		m.myNodeInfo = newjson
		return nil
	}
}

func (m *nodeinfo) sendReq(from phony.Actor, key keyArray, callback func(nodeinfo json.RawMessage)) {
	m.Act(from, func() {
		m._sendReq(key, callback)
	})
}

func (m *nodeinfo) _sendReq(key keyArray, callback func(nodeinfo json.RawMessage)) {
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

func (m *nodeinfo) handleRes(from phony.Actor, key keyArray, info json.RawMessage) {
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
type GetNodeInfoResponse map[string]json.RawMessage

func (m *nodeinfo) nodeInfoAdminHandler(in json.RawMessage) (interface{}, error) {
	var req GetNodeInfoRequest
	if err := json.Unmarshal(in, &req); err != nil {
		return nil, err
	}
	if req.Key == "" {
		return nil, fmt.Errorf("No remote public key supplied")
	}
	var key keyArray
	var kbs []byte
	var err error
	if kbs, err = hex.DecodeString(req.Key); err != nil {
		return nil, fmt.Errorf("Failed to decode public key: %w", err)
	}
	copy(key[:], kbs)
	ch := make(chan []byte, 1)
	m.sendReq(nil, key, func(info json.RawMessage) {
		ch <- info
	})
	timer := time.NewTimer(6 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil, errors.New("Timed out waiting for response")
	case info := <-ch:
		var msg json.RawMessage
		if err := msg.UnmarshalJSON(info); err != nil {
			return nil, err
		}
		key := hex.EncodeToString(kbs[:])
		res := GetNodeInfoResponse{key: msg}
		return res, nil
	}
}
