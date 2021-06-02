package tuntap

import (
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
	typeDebugGetDHTRequest
	typeDebugGetDHTResponse
)

type reqInfo struct {
	callback func([]byte)
	timer    *time.Timer // time.AfterFunc cleanup
}

type protoHandler struct {
	phony.Inbox
	tun      *TunAdapter
	nodeinfo nodeinfo
	sreqs    map[keyArray]*reqInfo
	preqs    map[keyArray]*reqInfo
	dreqs    map[keyArray]*reqInfo
}

func (p *protoHandler) init(tun *TunAdapter) {
	p.tun = tun
	p.nodeinfo.init(p)
	p.sreqs = make(map[keyArray]*reqInfo)
	p.preqs = make(map[keyArray]*reqInfo)
	p.dreqs = make(map[keyArray]*reqInfo)
}

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
		p._handleDebug(key, bs[1:])
	}
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
	case typeDebugGetDHTRequest:
		p._handleGetDHTRequest(key)
	case typeDebugGetDHTResponse:
		p._handleGetDHTResponse(key, bs[1:])
	}
}

func (p *protoHandler) sendGetSelfRequest(key keyArray, callback func([]byte)) {
	p.Act(nil, func() {
		if info := p.sreqs[key]; info != nil {
			info.timer.Stop()
			delete(p.sreqs, key)
		}
		info := new(reqInfo)
		info.callback = callback
		info.timer = time.AfterFunc(time.Minute, func() {
			p.Act(nil, func() {
				if p.sreqs[key] == info {
					delete(p.sreqs, key)
				}
			})
		})
		p.sreqs[key] = info
		p._sendDebug(key, typeDebugGetSelfRequest, nil)
	})
}

func (p *protoHandler) _handleGetSelfRequest(key keyArray) {
	self := p.tun.core.GetSelf()
	res := map[string]string{
		"key":    hex.EncodeToString(self.Key[:]),
		"coords": fmt.Sprintf("%v", self.Coords),
	}
	bs, err := json.Marshal(res) // FIXME this puts keys in base64, not hex
	if err != nil {
		return
	}
	p._sendDebug(key, typeDebugGetSelfResponse, bs)
}

func (p *protoHandler) _handleGetSelfResponse(key keyArray, bs []byte) {
	if info := p.sreqs[key]; info != nil {
		info.timer.Stop()
		info.callback(bs)
		delete(p.sreqs, key)
	}
}

func (p *protoHandler) sendGetPeersRequest(key keyArray, callback func([]byte)) {
	p.Act(nil, func() {
		if info := p.preqs[key]; info != nil {
			info.timer.Stop()
			delete(p.preqs, key)
		}
		info := new(reqInfo)
		info.callback = callback
		info.timer = time.AfterFunc(time.Minute, func() {
			p.Act(nil, func() {
				if p.preqs[key] == info {
					delete(p.preqs, key)
				}
			})
		})
		p.preqs[key] = info
		p._sendDebug(key, typeDebugGetPeersRequest, nil)
	})
}

func (p *protoHandler) _handleGetPeersRequest(key keyArray) {
	peers := p.tun.core.GetPeers()
	var bs []byte
	for _, pinfo := range peers {
		tmp := append(bs, pinfo.Key[:]...)
		const responseOverhead = 2 // 1 debug type, 1 getpeers type
		if uint64(len(tmp))+responseOverhead > p.tun.maxSessionMTU() {
			break
		}
		bs = tmp
	}
	p._sendDebug(key, typeDebugGetPeersResponse, bs)
}

func (p *protoHandler) _handleGetPeersResponse(key keyArray, bs []byte) {
	if info := p.preqs[key]; info != nil {
		info.timer.Stop()
		info.callback(bs)
		delete(p.preqs, key)
	}
}

func (p *protoHandler) sendGetDHTRequest(key keyArray, callback func([]byte)) {
	p.Act(nil, func() {
		if info := p.dreqs[key]; info != nil {
			info.timer.Stop()
			delete(p.dreqs, key)
		}
		info := new(reqInfo)
		info.callback = callback
		info.timer = time.AfterFunc(time.Minute, func() {
			p.Act(nil, func() {
				if p.dreqs[key] == info {
					delete(p.dreqs, key)
				}
			})
		})
		p.dreqs[key] = info
		p._sendDebug(key, typeDebugGetDHTRequest, nil)
	})
}

func (p *protoHandler) _handleGetDHTRequest(key keyArray) {
	dinfos := p.tun.core.GetDHT()
	var bs []byte
	for _, dinfo := range dinfos {
		tmp := append(bs, dinfo.Key[:]...)
		const responseOverhead = 2 // 1 debug type, 1 getdht type
		if uint64(len(tmp))+responseOverhead > p.tun.maxSessionMTU() {
			break
		}
		bs = tmp
	}
	p._sendDebug(key, typeDebugGetDHTResponse, bs)
}

func (p *protoHandler) _handleGetDHTResponse(key keyArray, bs []byte) {
	if info := p.dreqs[key]; info != nil {
		info.timer.Stop()
		info.callback(bs)
		delete(p.dreqs, key)
	}
}

func (p *protoHandler) _sendDebug(key keyArray, dType uint8, data []byte) {
	bs := append([]byte{typeSessionProto, typeProtoDebug, dType}, data...)
	_, _ = p.tun.core.WriteTo(bs, iwt.Addr(key[:]))
}

// Admin socket stuff

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
	copy(key[:], kbs)
	ch := make(chan []byte, 1)
	p.sendGetSelfRequest(key, func(info []byte) {
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
		res := DebugGetSelfResponse{ip.String(): msg}
		return res, nil
	}
}

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
	copy(key[:], kbs)
	ch := make(chan []byte, 1)
	p.sendGetPeersRequest(key, func(info []byte) {
		ch <- info
	})
	timer := time.NewTimer(6 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
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

type DebugGetDHTRequest struct {
	Key string `json:"key"`
}

type DebugGetDHTResponse map[string]interface{}

func (p *protoHandler) getDHTHandler(in json.RawMessage) (interface{}, error) {
	var req DebugGetDHTRequest
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
	p.sendGetDHTRequest(key, func(info []byte) {
		ch <- info
	})
	timer := time.NewTimer(6 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
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
		res := DebugGetDHTResponse{ip.String(): msg}
		return res, nil
	}
}
