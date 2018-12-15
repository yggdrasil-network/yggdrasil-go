package yggdrasil

import (
	"sync"
	"time"
)

type metadata struct {
	core            *Core
	myMetadata      metadataPayload
	myMetadataMutex sync.RWMutex
	callbacks       map[boxPubKey]metadataCallback
	cache           map[boxPubKey]metadataPayload
}

type metadataPayload []byte

type metadataCallback struct {
	call    func(meta *metadataPayload)
	created time.Time
}

// Initialises the metadata cache/callback stuff
func (m *metadata) init(core *Core) {
	m.core = core
	m.callbacks = make(map[boxPubKey]metadataCallback)
	m.cache = make(map[boxPubKey]metadataPayload)

	go func() {
		for {
			for boxPubKey, callback := range m.callbacks {
				if time.Since(callback.created) > time.Minute {
					delete(m.callbacks, boxPubKey)
				}
			}
			time.Sleep(time.Second * 5)
		}
	}()
}

// Handles the callback, if there is one
func (m *metadata) callback(sender boxPubKey, meta metadataPayload) {
	if callback, ok := m.callbacks[sender]; ok {
		callback.call(&meta)
		delete(m.callbacks, sender)
	}
}

// Get the metadata
func (m *metadata) getMetadata() metadataPayload {
	m.myMetadataMutex.RLock()
	defer m.myMetadataMutex.RUnlock()
	return m.myMetadata
}

// Set the metadata
func (m *metadata) setMetadata(meta metadataPayload) {
	m.myMetadataMutex.Lock()
	defer m.myMetadataMutex.Unlock()
	m.myMetadata = meta
}

// Handles a meta request/response.
func (m *metadata) handleMetadata(meta *sessionMeta) {
	if meta.IsResponse {
		m.core.metadata.callback(meta.SendPermPub, meta.Metadata)
	} else {
		m.sendMetadata(meta.SendPermPub, meta.SendCoords, true)
	}
}

// Send metadata request or response
func (m *metadata) sendMetadata(key boxPubKey, coords []byte, isResponse bool) {
	table := m.core.switchTable.table.Load().(lookupTable)
	meta := sessionMeta{
		SendCoords: table.self.getCoords(),
		IsResponse: isResponse,
		Metadata:   m.core.metadata.getMetadata(),
	}
	bs := meta.encode()
	shared := m.core.sessions.getSharedKey(&m.core.boxPriv, &key)
	payload, nonce := boxSeal(shared, bs, nil)
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
