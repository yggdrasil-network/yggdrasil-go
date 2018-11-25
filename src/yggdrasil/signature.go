package yggdrasil

// This is where we record which signatures we've previously checked
// It's so we can avoid needlessly checking them again

import (
	"sync"
	"time"
)

// This keeps track of what signatures have already been checked.
// It's used to skip expensive crypto operations, given that many signatures are likely to be the same for the average node's peers.
type sigManager struct {
	mutex       sync.RWMutex
	checked     map[sigBytes]knownSig
	lastCleaned time.Time
}

// Represents a known signature.
// Includes the key, the signature bytes, the bytes that were signed, and the time it was last used.
type knownSig struct {
	key  sigPubKey
	sig  sigBytes
	bs   []byte
	time time.Time
}

// Initializes the signature manager.
func (m *sigManager) init() {
	m.checked = make(map[sigBytes]knownSig)
}

// Checks if a key and signature match the supplied bytes.
// If the same key/sig/bytes have been checked before, it returns true from the cached results.
// If not, it checks the key, updates it in the cache if successful, and returns the checked results.
func (m *sigManager) check(key *sigPubKey, sig *sigBytes, bs []byte) bool {
	if m.isChecked(key, sig, bs) {
		return true
	}
	verified := verify(key, bs, sig)
	if verified {
		m.putChecked(key, sig, bs)
	}
	return verified
}

// Checks the cache to see if this key/sig/bytes combination has already been verified.
// Returns true if it finds a match.
func (m *sigManager) isChecked(key *sigPubKey, sig *sigBytes, bs []byte) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	k, isIn := m.checked[*sig]
	if !isIn {
		return false
	}
	if k.key != *key || k.sig != *sig || len(bs) != len(k.bs) {
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

// Puts a new result into the cache.
// This result is then used by isChecked to skip the expensive crypto verification if it's needed again.
// This is useful because, for nodes with multiple peers, there is often a lot of overlap between the signatures provided by each peer.
func (m *sigManager) putChecked(key *sigPubKey, newsig *sigBytes, bs []byte) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	k := knownSig{key: *key, sig: *newsig, bs: bs, time: time.Now()}
	m.checked[*newsig] = k
}

func (m *sigManager) cleanup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if time.Since(m.lastCleaned) < time.Minute {
		return
	}
	for s, k := range m.checked {
		if time.Since(k.time) > time.Minute {
			delete(m.checked, s)
		}
	}
	newChecked := make(map[sigBytes]knownSig, len(m.checked))
	for s, k := range m.checked {
		newChecked[s] = k
	}
	m.checked = newChecked
	m.lastCleaned = time.Now()
}
