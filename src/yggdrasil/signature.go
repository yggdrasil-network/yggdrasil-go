package yggdrasil

// This is where we record which signatures we've previously checked
// It's so we can avoid needlessly checking them again

import "sync"
import "time"

type sigManager struct {
	mutex       sync.RWMutex
	checked     map[sigBytes]knownSig
	lastCleaned time.Time
}

type knownSig struct {
	bs   []byte
	time time.Time
}

func (m *sigManager) init() {
	m.checked = make(map[sigBytes]knownSig)
}

func (m *sigManager) check(key *sigPubKey, sig *sigBytes, bs []byte) bool {
	if m.isChecked(sig, bs) {
		return true
	}
	verified := verify(key, bs, sig)
	if verified {
		m.putChecked(sig, bs)
	}
	return verified
}

func (m *sigManager) isChecked(sig *sigBytes, bs []byte) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	k, isIn := m.checked[*sig]
	if !isIn {
		return false
	}
	if len(bs) != len(k.bs) {
		return false
	}
	for idx := 0; idx < len(bs); idx++ {
		if bs[idx] != k.bs[idx] {
			return false
		}
	}
	k.time = time.Now()
	return true
}

func (m *sigManager) putChecked(newsig *sigBytes, bs []byte) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	now := time.Now()
	if time.Since(m.lastCleaned) > 60*time.Second {
		// Since we have the write lock anyway, do some cleanup
		for s, k := range m.checked {
			if time.Since(k.time) > 60*time.Second {
				delete(m.checked, s)
			}
		}
		m.lastCleaned = now
	}
	k := knownSig{bs: bs, time: now}
	m.checked[*newsig] = k
}
