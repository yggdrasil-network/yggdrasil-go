package yggdrasil

import (
	"encoding/json"
	"errors"
	"runtime"
	"sync"
	"time"
)

type metadata struct {
	core            *Core
	myMetadata      metadataPayload
	myMetadataMutex sync.RWMutex
	callbacks       map[boxPubKey]metadataCallback
	callbacksMutex  sync.Mutex
	cache           map[boxPubKey]metadataCached
	cacheMutex      sync.RWMutex
}

type metadataPayload []byte

type metadataCached struct {
	payload metadataPayload
	created time.Time
}

type metadataCallback struct {
	call    func(meta *metadataPayload)
	created time.Time
}

// Initialises the metadata cache/callback maps, and starts a goroutine to keep
// the cache/callback maps clean of stale entries
func (m *metadata) init(core *Core) {
	m.core = core
	m.callbacks = make(map[boxPubKey]metadataCallback)
	m.cache = make(map[boxPubKey]metadataCached)

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

// Add a callback for a metadata lookup
func (m *metadata) addCallback(sender boxPubKey, call func(meta *metadataPayload)) {
	m.callbacksMutex.Lock()
	defer m.callbacksMutex.Unlock()
	m.callbacks[sender] = metadataCallback{
		created: time.Now(),
		call:    call,
	}
}

// Handles the callback, if there is one
func (m *metadata) callback(sender boxPubKey, meta metadataPayload) {
	m.callbacksMutex.Lock()
	defer m.callbacksMutex.Unlock()
	if callback, ok := m.callbacks[sender]; ok {
		callback.call(&meta)
		delete(m.callbacks, sender)
	}
}

// Get the current node's metadata
func (m *metadata) getMetadata() metadataPayload {
	m.myMetadataMutex.RLock()
	defer m.myMetadataMutex.RUnlock()
	return m.myMetadata
}

// Set the current node's metadata
func (m *metadata) setMetadata(given interface{}) error {
	m.myMetadataMutex.Lock()
	defer m.myMetadataMutex.Unlock()
	newmeta := map[string]interface{}{
		"buildname":     GetBuildName(),
		"buildversion":  GetBuildVersion(),
		"buildplatform": runtime.GOOS,
		"buildarch":     runtime.GOARCH,
	}
	if metamap, ok := given.(map[string]interface{}); ok {
		for key, value := range metamap {
			if _, ok := newmeta[key]; ok {
				continue
			}
			newmeta[key] = value
		}
	}
	if newjson, err := json.Marshal(newmeta); err == nil {
		m.myMetadata = newjson
		return nil
	} else {
		return err
	}
}

// Add metadata into the cache for a node
func (m *metadata) addCachedMetadata(key boxPubKey, payload metadataPayload) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()
	m.cache[key] = metadataCached{
		created: time.Now(),
		payload: payload,
	}
}

// Get a metadata entry from the cache
func (m *metadata) getCachedMetadata(key boxPubKey) (metadataPayload, error) {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()
	if meta, ok := m.cache[key]; ok {
		return meta.payload, nil
	}
	return metadataPayload{}, errors.New("No cache entry found")
}

// Handles a meta request/response - called from the router
func (m *metadata) handleMetadata(meta *sessionMeta) {
	if meta.IsResponse {
		m.callback(meta.SendPermPub, meta.Metadata)
		m.addCachedMetadata(meta.SendPermPub, meta.Metadata)
	} else {
		m.sendMetadata(meta.SendPermPub, meta.SendCoords, true)
	}
}

// Send metadata request or response - called from the router
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
