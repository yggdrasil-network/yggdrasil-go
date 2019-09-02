package main

import (
	"encoding/hex"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

func (n *node) sessionFirewall(pubkey *crypto.BoxPubKey, initiator bool) bool {
	current := n.config

	// Allow by default if the session firewall is disabled
	if !current.SessionFirewall.Enable {
		return true
	}

	// Prepare for checking whitelist/blacklist
	var box crypto.BoxPubKey
	// Reject blacklisted nodes
	for _, b := range current.SessionFirewall.BlacklistEncryptionPublicKeys {
		key, err := hex.DecodeString(b)
		if err == nil {
			copy(box[:crypto.BoxPubKeyLen], key)
			if box == *pubkey {
				return false
			}
		}
	}

	// Allow whitelisted nodes
	for _, b := range current.SessionFirewall.WhitelistEncryptionPublicKeys {
		key, err := hex.DecodeString(b)
		if err == nil {
			copy(box[:crypto.BoxPubKeyLen], key)
			if box == *pubkey {
				return true
			}
		}
	}

	// Allow outbound sessions if appropriate
	if current.SessionFirewall.AlwaysAllowOutbound {
		if initiator {
			return true
		}
	}

	// Look and see if the pubkey is that of a direct peer
	var isDirectPeer bool
	for _, peer := range n.core.GetPeers() {
		if peer.PublicKey == *pubkey {
			isDirectPeer = true
			break
		}
	}

	// Allow direct peers if appropriate
	if current.SessionFirewall.AllowFromDirect && isDirectPeer {
		return true
	}

	// Allow remote nodes if appropriate
	if current.SessionFirewall.AllowFromRemote && !isDirectPeer {
		return true
	}

	// Finally, default-deny if not matching any of the above rules
	return false
}
