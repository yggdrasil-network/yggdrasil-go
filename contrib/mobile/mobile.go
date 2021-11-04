package mobile

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
	"github.com/yggdrasil-network/yggdrasil-go/src/ipv6rwc"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"

	_ "golang.org/x/mobile/bind"
)

// Yggdrasil mobile package is meant to "plug the gap" for mobile support, as
// Gomobile will not create headers for Swift/Obj-C etc if they have complex
// (non-native) types. Therefore for iOS we will expose some nice simple
// functions. Note that in the case of iOS we handle reading/writing to/from TUN
// in Swift therefore we use the "dummy" TUN interface instead.
type Yggdrasil struct {
	core      core.Core
	iprwc     *ipv6rwc.ReadWriteCloser
	config    *config.NodeConfig
	multicast multicast.Multicast
	log       MobileLogger
}

// StartAutoconfigure starts a node with a randomly generated config
func (m *Yggdrasil) StartAutoconfigure() error {
	return m.StartJSON([]byte("{}"))
}

// StartJSON starts a node with the given JSON config. You can get JSON config
// (rather than HJSON) by using the GenerateConfigJSON() function
func (m *Yggdrasil) StartJSON(configjson []byte) error {
	logger := log.New(m.log, "", 0)
	logger.EnableLevel("error")
	logger.EnableLevel("warn")
	logger.EnableLevel("info")
	m.config = defaults.GenerateConfig()
	if err := json.Unmarshal(configjson, &m.config); err != nil {
		return err
	}
	m.config.IfName = "none"
	if err := m.core.Start(m.config, logger); err != nil {
		logger.Errorln("An error occured starting Yggdrasil:", err)
		return err
	}
	mtu := m.config.IfMTU
	m.iprwc = ipv6rwc.NewReadWriteCloser(&m.core)
	if m.iprwc.MaxMTU() < mtu {
		mtu = m.iprwc.MaxMTU()
	}
	m.iprwc.SetMTU(mtu)
	if len(m.config.MulticastInterfaces) > 0 {
		if err := m.multicast.Init(&m.core, m.config, logger, nil); err != nil {
			logger.Errorln("An error occurred initialising multicast:", err)
			return err
		}
		if err := m.multicast.Start(); err != nil {
			logger.Errorln("An error occurred starting multicast:", err)
			return err
		}
	}
	return nil
}

// Send sends a packet to Yggdrasil. It should be a fully formed
// IPv6 packet
func (m *Yggdrasil) Send(p []byte) error {
	if m.iprwc == nil {
		return nil
	}
	_, _ = m.iprwc.Write(p)
	return nil
}

// Recv waits for and reads a packet coming from Yggdrasil. It
// will be a fully formed IPv6 packet
func (m *Yggdrasil) Recv() ([]byte, error) {
	if m.iprwc == nil {
		return nil, nil
	}
	var buf [65535]byte
	n, _ := m.iprwc.Read(buf[:])
	return buf[:n], nil
}

// Stop the mobile Yggdrasil instance
func (m *Yggdrasil) Stop() error {
	logger := log.New(m.log, "", 0)
	logger.EnableLevel("info")
	logger.Infof("Stop the mobile Yggdrasil instance %s", "")
	if err := m.multicast.Stop(); err != nil {
		return err
	}
	m.core.Stop()
	return nil
}

// GenerateConfigJSON generates mobile-friendly configuration in JSON format
func GenerateConfigJSON() []byte {
	nc := defaults.GenerateConfig()
	nc.IfName = "none"
	if json, err := json.Marshal(nc); err == nil {
		return json
	}
	return nil
}

// GetAddressString gets the node's IPv6 address
func (m *Yggdrasil) GetAddressString() string {
	ip := m.core.Address()
	return ip.String()
}

// GetSubnetString gets the node's IPv6 subnet in CIDR notation
func (m *Yggdrasil) GetSubnetString() string {
	subnet := m.core.Subnet()
	return subnet.String()
}

// GetPublicKeyString gets the node's public key in hex form
func (m *Yggdrasil) GetPublicKeyString() string {
	return hex.EncodeToString(m.core.GetSelf().Key)
}

// GetCoordsString gets the node's coordinates
func (m *Yggdrasil) GetCoordsString() string {
	return fmt.Sprintf("%v", m.core.GetSelf().Coords)
}

func (m *Yggdrasil) GetPeersJSON() (result string) {
	peers := []struct {
		core.Peer
		IP string
	}{}
	for _, v := range m.core.GetPeers() {
		a := address.AddrForKey(v.Key)
		ip := net.IP(a[:]).String()
		peers = append(peers, struct {
			core.Peer
			IP string
		}{
			Peer: v,
			IP:   ip,
		})
	}
	if res, err := json.Marshal(peers); err == nil {
		return string(res)
	} else {
		return "{}"
	}
}

func (m *Yggdrasil) GetDHTJSON() (result string) {
	if res, err := json.Marshal(m.core.GetDHT()); err == nil {
		return string(res)
	} else {
		return "{}"
	}
}

// GetMTU returns the configured node MTU. This must be called AFTER Start.
func (m *Yggdrasil) GetMTU() int {
	return int(m.core.MTU())
}

func GetVersion() string {
	return version.BuildVersion()
}
