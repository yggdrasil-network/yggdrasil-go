package core

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	iwt "github.com/Arceliar/ironwood/types"
	"github.com/Arceliar/phony"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

const (
	typeDebugDummy = iota
	typeDebugGetSelfRequest
	typeDebugGetSelfResponse
	typeDebugGetPeersRequest
	typeDebugGetPeersResponse
	typeDebugGetTreeRequest
	typeDebugGetTreeResponse
)

type reqInfo struct {
	callback func([]byte)
	timer    *time.Timer // time.AfterFunc cleanup
}

type keyArray [ed25519.PublicKeySize]byte

type protoHandler struct {
	phony.Inbox

	core     *Core
	nodeinfo nodeinfo

	selfRequests  map[keyArray]*reqInfo
	peersRequests map[keyArray]*reqInfo
	treeRequests  map[keyArray]*reqInfo
}

func (p *protoHandler) init(core *Core) {
	p.core = core
	p.nodeinfo.init(p)

	p.selfRequests = make(map[keyArray]*reqInfo)
	p.peersRequests = make(map[keyArray]*reqInfo)
	p.treeRequests = make(map[keyArray]*reqInfo)
}

// Common functions

func (p *protoHandler) handleProto(from phony.Actor, key keyArray, bs []byte) {
	if len(bs) == 0 {
		return
	}
	switch bs[0] {
	case typeProtoDummy:
	case typeProtoNodeInfoRequest:
		p.nodeinfo.handleReq(p, key)
	case typeProtoNodeInfoResponse:
		p.nodeinfo.handleRes(p, key, bs[1:])
	case typeProtoDebug:
		p.handleDebug(from, key, bs[1:])
	}
}

func (p *protoHandler) handleDebug(from phony.Actor, key keyArray, bs []byte) {
	p.Act(from, func() {
		p._handleDebug(key, bs)
	})
}

func (p *protoHandler) _handleDebug(key keyArray, bs []byte) {
	if len(bs) == 0 {
		return
	}
	switch bs[0] {
	case typeDebugDummy:
	case typeDebugGetSelfRequest:
		p._handleGetSelfRequest(key)
	case typeDebugGetSelfResponse:
		p._handleGetSelfResponse(key, bs[1:])
	case typeDebugGetPeersRequest:
		p._handleGetPeersRequest(key)
	case typeDebugGetPeersResponse:
		p._handleGetPeersResponse(key, bs[1:])
	case typeDebugGetTreeRequest:
		p._handleGetTreeRequest(key)
	case typeDebugGetTreeResponse:
		p._handleGetTreeResponse(key, bs[1:])
	}
}

func (p *protoHandler) _sendDebug(key keyArray, dType uint8, data []byte) {
	bs := append([]byte{typeSessionProto, typeProtoDebug, dType}, data...)
	_, _ = p.core.PacketConn.WriteTo(bs, iwt.Addr(key[:]))
}

// Get self

func (p *protoHandler) sendGetSelfRequest(key keyArray, callback func([]byte)) {
	p.Act(nil, func() {
		if info := p.selfRequests[key]; info != nil {
			info.timer.Stop()
			delete(p.selfRequests, key)
		}
		info := new(reqInfo)
		info.callback = callback
		info.timer = time.AfterFunc(time.Minute, func() {
			p.Act(nil, func() {
				if p.selfRequests[key] == info {
					delete(p.selfRequests, key)
				}
			})
		})
		p.selfRequests[key] = info
		p._sendDebug(key, typeDebugGetSelfRequest, nil)
	})
}

func (p *protoHandler) _handleGetSelfRequest(key keyArray) {
	self := p.core.GetSelf()
	res := map[string]string{
		"key":             hex.EncodeToString(self.Key[:]),
		"routing_entries": fmt.Sprintf("%v", self.RoutingEntries),
	}
	bs, err := json.Marshal(res) // FIXME this puts keys in base64, not hex
	if err != nil {
		return
	}
	p._sendDebug(key, typeDebugGetSelfResponse, bs)
}

func (p *protoHandler) _handleGetSelfResponse(key keyArray, bs []byte) {
	if info := p.selfRequests[key]; info != nil {
		info.timer.Stop()
		info.callback(bs)
		delete(p.selfRequests, key)
	}
}

// Get peers

func (p *protoHandler) sendGetPeersRequest(key keyArray, callback func([]byte)) {
	p.Act(nil, func() {
		if info := p.peersRequests[key]; info != nil {
			info.timer.Stop()
			delete(p.peersRequests, key)
		}
		info := new(reqInfo)
		info.callback = callback
		info.timer = time.AfterFunc(time.Minute, func() {
			p.Act(nil, func() {
				if p.peersRequests[key] == info {
					delete(p.peersRequests, key)
				}
			})
		})
		p.peersRequests[key] = info
		p._sendDebug(key, typeDebugGetPeersRequest, nil)
	})
}

func (p *protoHandler) _handleGetPeersRequest(key keyArray) {
	peers := p.core.GetPeers()
	var bs []byte
	for _, pinfo := range peers {
		tmp := append(bs, pinfo.Key[:]...)
		const responseOverhead = 2 // 1 debug type, 1 getpeers type
		if uint64(len(tmp))+responseOverhead > p.core.MTU() {
			break
		}
		bs = tmp
	}
	p._sendDebug(key, typeDebugGetPeersResponse, bs)
}

func (p *protoHandler) _handleGetPeersResponse(key keyArray, bs []byte) {
	if info := p.peersRequests[key]; info != nil {
		info.timer.Stop()
		info.callback(bs)
		delete(p.peersRequests, key)
	}
}

// Get Tree

func (p *protoHandler) sendGetTreeRequest(key keyArray, callback func([]byte)) {
	p.Act(nil, func() {
		if info := p.treeRequests[key]; info != nil {
			info.timer.Stop()
			delete(p.treeRequests, key)
		}
		info := new(reqInfo)
		info.callback = callback
		info.timer = time.AfterFunc(time.Minute, func() {
			p.Act(nil, func() {
				if p.treeRequests[key] == info {
					delete(p.treeRequests, key)
				}
			})
		})
		p.treeRequests[key] = info
		p._sendDebug(key, typeDebugGetTreeRequest, nil)
	})
}

func (p *protoHandler) _handleGetTreeRequest(key keyArray) {
	dinfos := p.core.GetTree()
	var bs []byte
	for _, dinfo := range dinfos {
		tmp := append(bs, dinfo.Key[:]...)
		const responseOverhead = 2 // 1 debug type, 1 gettree type
		if uint64(len(tmp))+responseOverhead > p.core.MTU() {
			break
		}
		bs = tmp
	}
	p._sendDebug(key, typeDebugGetTreeResponse, bs)
}

func (p *protoHandler) _handleGetTreeResponse(key keyArray, bs []byte) {
	if info := p.treeRequests[key]; info != nil {
		info.timer.Stop()
		info.callback(bs)
		delete(p.treeRequests, key)
	}
}

// Admin socket stuff for "Get self"

type DebugGetSelfRequest struct {
	Key string `json:"key"`
}

type DebugGetSelfResponse map[string]interface{}

func (p *protoHandler) getSelfHandler(in json.RawMessage) (interface{}, error) {
	var req DebugGetSelfRequest
	if err := json.Unmarshal(in, &req); err != nil {
		return nil, err
	}
	var key keyArray
	var kbs []byte
	var err error
	if kbs, err = hex.DecodeString(req.Key); err != nil {
		return nil, err
	}
	if len(kbs) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length")
	}
	copy(key[:], kbs)
	ch := make(chan []byte, 1)
	p.sendGetSelfRequest(key, func(info []byte) {
		ch <- info
	})
	select {
	case <-time.After(6 * time.Second):
		return nil, errors.New("timeout")
	case info := <-ch:
		var msg json.RawMessage
		if err := msg.UnmarshalJSON(info); err != nil {
			return nil, err
		}
		ip := net.IP(address.AddrForKey(kbs)[:])
		res := DebugGetSelfResponse{ip.String(): msg}
		return res, nil
	}
}

// Admin socket stuff for "Get peers"

type DebugGetPeersRequest struct {
	Key string `json:"key"`
}

type DebugGetPeersResponse map[string]interface{}

func (p *protoHandler) getPeersHandler(in json.RawMessage) (interface{}, error) {
	var req DebugGetPeersRequest
	if err := json.Unmarshal(in, &req); err != nil {
		return nil, err
	}
	var key keyArray
	var kbs []byte
	var err error
	if kbs, err = hex.DecodeString(req.Key); err != nil {
		return nil, err
	}
	if len(kbs) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length")
	}
	copy(key[:], kbs)
	ch := make(chan []byte, 1)
	p.sendGetPeersRequest(key, func(info []byte) {
		ch <- info
	})
	select {
	case <-time.After(6 * time.Second):
		return nil, errors.New("timeout")
	case info := <-ch:
		ks := make(map[string][]string)
		bs := info
		for len(bs) >= len(key) {
			ks["keys"] = append(ks["keys"], hex.EncodeToString(bs[:len(key)]))
			bs = bs[len(key):]
		}
		js, err := json.Marshal(ks)
		if err != nil {
			return nil, err
		}
		var msg json.RawMessage
		if err := msg.UnmarshalJSON(js); err != nil {
			return nil, err
		}
		ip := net.IP(address.AddrForKey(kbs)[:])
		res := DebugGetPeersResponse{ip.String(): msg}
		return res, nil
	}
}

// Admin socket stuff for "Get Tree"

type DebugGetTreeRequest struct {
	Key string `json:"key"`
}

type DebugGetTreeResponse map[string]interface{}

func (p *protoHandler) getTreeHandler(in json.RawMessage) (interface{}, error) {
	var req DebugGetTreeRequest
	if err := json.Unmarshal(in, &req); err != nil {
		return nil, err
	}
	var key keyArray
	var kbs []byte
	var err error
	if kbs, err = hex.DecodeString(req.Key); err != nil {
		return nil, err
	}
	if len(kbs) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length")
	}
	copy(key[:], kbs)
	ch := make(chan []byte, 1)
	p.sendGetTreeRequest(key, func(info []byte) {
		ch <- info
	})
	select {
	case <-time.After(6 * time.Second):
		return nil, errors.New("timeout")
	case info := <-ch:
		ks := make(map[string][]string)
		bs := info
		for len(bs) >= len(key) {
			ks["keys"] = append(ks["keys"], hex.EncodeToString(bs[:len(key)]))
			bs = bs[len(key):]
		}
		js, err := json.Marshal(ks)
		if err != nil {
			return nil, err
		}
		var msg json.RawMessage
		if err := msg.UnmarshalJSON(js); err != nil {
			return nil, err
		}
		ip := net.IP(address.AddrForKey(kbs)[:])
		res := DebugGetTreeResponse{ip.String(): msg}
		return res, nil
	}
}
