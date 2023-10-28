package admin

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net"
	"sync"
	"time"

	"github.com/Arceliar/ironwood/network"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

func (c *AdminSocket) _applyOption(opt SetupOption) {
	switch v := opt.(type) {
	case ListenAddress:
		c.config.listenaddr = v
	case LogLookups:
		c.logLookups()
	}
}

type SetupOption interface {
	isSetupOption()
}

type ListenAddress string

func (a ListenAddress) isSetupOption() {}

type LogLookups struct{}

func (l LogLookups) isSetupOption() {}

func (a *AdminSocket) logLookups() {
	type resi struct {
		Address string   `json:"addr"`
		Key     string   `json:"key"`
		Path    []uint64 `json:"path"`
		Time    int64    `json:"time"`
	}
	type res struct {
		Infos []resi `json:"infos"`
	}
	type info struct {
		path []uint64
		time time.Time
	}
	type edk [ed25519.PublicKeySize]byte
	infos := make(map[edk]info)
	var m sync.Mutex
	a.core.PacketConn.PacketConn.Debug.SetDebugLookupLogger(func(l network.DebugLookupInfo) {
		var k edk
		copy(k[:], l.Key[:])
		m.Lock()
		infos[k] = info{path: l.Path, time: time.Now()}
		m.Unlock()
	})
	_ = a.AddHandler(
		"lookups", "Dump a record of lookups received in the past hour", []string{},
		func(in json.RawMessage) (interface{}, error) {
			m.Lock()
			rs := make([]resi, 0, len(infos))
			for k, v := range infos {
				if time.Since(v.time) > 24*time.Hour {
					// TODO? automatic cleanup, so we don't need to call lookups periodically to prevent leaks
					delete(infos, k)
				}
				a := address.AddrForKey(ed25519.PublicKey(k[:]))
				addr := net.IP(a[:]).String()
				rs = append(rs, resi{Address: addr, Key: hex.EncodeToString(k[:]), Path: v.path, Time: v.time.Unix()})
			}
			m.Unlock()
			return &res{Infos: rs}, nil
		},
	)
}
