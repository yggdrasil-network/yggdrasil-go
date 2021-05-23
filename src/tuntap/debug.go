package tuntap

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	iwt "github.com/Arceliar/ironwood/types"
	"github.com/Arceliar/phony"
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

type debugHandler struct {
	phony.Inbox
	tun   *TunAdapter
	sreqs map[keyArray]*reqInfo
	preqs map[keyArray]*reqInfo
	dreqs map[keyArray]*reqInfo
}

func (d *debugHandler) init(tun *TunAdapter) {
	d.tun = tun
	d.sreqs = make(map[keyArray]*reqInfo)
	d.preqs = make(map[keyArray]*reqInfo)
	d.dreqs = make(map[keyArray]*reqInfo)
}

func (d *debugHandler) handleDebug(from phony.Actor, key keyArray, bs []byte) {
	d.Act(from, func() {
		d._handleDebug(key, bs)
	})
}

func (d *debugHandler) _handleDebug(key keyArray, bs []byte) {
	if len(bs) == 0 {
		return
	}
	switch bs[0] {
	case typeDebugDummy:
	case typeDebugGetSelfRequest:
		d._handleGetSelfRequest(key)
	case typeDebugGetSelfResponse:
		d._handleGetSelfResponse(key, bs[1:])
	case typeDebugGetPeersRequest:
		d._handleGetPeersRequest(key)
	case typeDebugGetPeersResponse:
		d._handleGetPeersResponse(key, bs[1:])
	case typeDebugGetDHTRequest:
		d._handleGetDHTRequest(key)
	case typeDebugGetDHTResponse:
		d._handleGetDHTResponse(key, bs[1:])
	default:
	}
}

func (d *debugHandler) sendGetSelfRequest(key keyArray, callback func([]byte)) {
	d.Act(nil, func() {
		if info := d.sreqs[key]; info != nil {
			info.timer.Stop()
			delete(d.sreqs, key)
		}
		info := new(reqInfo)
		info.callback = callback
		info.timer = time.AfterFunc(time.Minute, func() {
			d.Act(nil, func() {
				if d.sreqs[key] == info {
					delete(d.sreqs, key)
				}
			})
		})
		d.sreqs[key] = info
		d._sendDebug(key, typeDebugGetSelfRequest, nil)
	})
}

func (d *debugHandler) _handleGetSelfRequest(key keyArray) {
	self := d.tun.core.GetSelf()
	bs, err := json.Marshal(self)
	if err != nil {
		return
	}
	d._sendDebug(key, typeDebugGetSelfResponse, bs)
}

func (d *debugHandler) _handleGetSelfResponse(key keyArray, bs []byte) {
	if info := d.sreqs[key]; info != nil {
		info.timer.Stop()
		info.callback(bs)
		delete(d.sreqs, key)
	}
}

func (d *debugHandler) sendGetPeersRequest(key keyArray, callback func([]byte)) {
	d.Act(nil, func() {
		if info := d.preqs[key]; info != nil {
			info.timer.Stop()
			delete(d.preqs, key)
		}
		info := new(reqInfo)
		info.callback = callback
		info.timer = time.AfterFunc(time.Minute, func() {
			d.Act(nil, func() {
				if d.preqs[key] == info {
					delete(d.preqs, key)
				}
			})
		})
		d.preqs[key] = info
		d._sendDebug(key, typeDebugGetPeersRequest, nil)
	})
}

func (d *debugHandler) _handleGetPeersRequest(key keyArray) {
	peers := d.tun.core.GetPeers()
	var bs []byte
	for _, p := range peers {
		tmp := append(bs, p.Key[:]...)
		const responseOverhead = 1
		if uint64(len(tmp))+1 > d.tun.maxSessionMTU() {
			break
		}
		bs = tmp
	}
	d._sendDebug(key, typeDebugGetPeersResponse, bs)
}

func (d *debugHandler) _handleGetPeersResponse(key keyArray, bs []byte) {
	if info := d.preqs[key]; info != nil {
		info.timer.Stop()
		info.callback(bs)
		delete(d.preqs, key)
	}
}

func (d *debugHandler) sendGetDHTRequest(key keyArray, callback func([]byte)) {
	d.Act(nil, func() {
		if info := d.dreqs[key]; info != nil {
			info.timer.Stop()
			delete(d.dreqs, key)
		}
		info := new(reqInfo)
		info.callback = callback
		info.timer = time.AfterFunc(time.Minute, func() {
			d.Act(nil, func() {
				if d.dreqs[key] == info {
					delete(d.dreqs, key)
				}
			})
		})
		d.dreqs[key] = info
		d._sendDebug(key, typeDebugGetDHTRequest, nil)
	})
}

func (d *debugHandler) _handleGetDHTRequest(key keyArray) {
	dinfos := d.tun.core.GetDHT()
	var bs []byte
	for _, dinfo := range dinfos {
		tmp := append(bs, dinfo.Key[:]...)
		const responseOverhead = 1
		if uint64(len(tmp))+1 > d.tun.maxSessionMTU() {
			break
		}
		bs = tmp
	}
	d._sendDebug(key, typeDebugGetDHTResponse, bs)
}

func (d *debugHandler) _handleGetDHTResponse(key keyArray, bs []byte) {
	if info := d.dreqs[key]; info != nil {
		info.timer.Stop()
		info.callback(bs)
		delete(d.dreqs, key)
	}
}

func (d *debugHandler) _sendDebug(key keyArray, dType uint8, data []byte) {
	bs := append([]byte{typeSessionDebug, dType}, data...)
	d.tun.core.WriteTo(bs, iwt.Addr(key[:]))
}

// Admin socket stuff

type DebugGetSelfRequest struct {
	Key string `json:"key"`
}

type DebugGetSelfResponse map[string]interface{}

func (d *debugHandler) getSelfHandler(in json.RawMessage) (interface{}, error) {
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
	d.sendGetSelfRequest(key, func(info []byte) {
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
		res := DebugGetSelfResponse{req.Key: msg}
		return res, nil
	}
}

type DebugGetPeersRequest struct {
	Key string `json:"key"`
}

type DebugGetPeersResponse map[string]interface{}

func (d *debugHandler) getPeersHandler(in json.RawMessage) (interface{}, error) {
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
	d.sendGetPeersRequest(key, func(info []byte) {
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
		res := DebugGetPeersResponse{req.Key: msg}
		return res, nil
	}
}

type DebugGetDHTRequest struct {
	Key string `json:"key"`
}

type DebugGetDHTResponse map[string]interface{}

func (d *debugHandler) getDHTHandler(in json.RawMessage) (interface{}, error) {
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
	d.sendGetDHTRequest(key, func(info []byte) {
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
		res := DebugGetDHTResponse{req.Key: msg}
		return res, nil
	}
}
