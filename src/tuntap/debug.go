package tuntap

import (
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
	ch    chan []byte
	timer time.Timer // time.AfterFunc cleanup
}

type debugHandler struct {
	phony.Inbox
	tun   *TunAdapter
	sreqs struct{} // TODO
	preqs map[keyArray]*reqInfo
	dreqs map[keyArray]*reqInfo
}

func (d *debugHandler) init(tun *TunAdapter) {
	d.tun = tun
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

func (d *debugHandler) _handleGetSelfRequest(key keyArray) {
	// TODO
}

func (d *debugHandler) _handleGetSelfResponse(key keyArray, bs []byte) {
	// TODO
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
		info.ch <- bs
		delete(d.preqs, key)
	}
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
		info.ch <- bs
		delete(d.dreqs, key)
	}
}

func (d *debugHandler) _sendDebug(key keyArray, dType uint8, data []byte) {
	bs := append([]byte{typeSessionDebug, dType}, data...)
	d.tun.core.WriteTo(bs, iwt.Addr(key[:]))
}
