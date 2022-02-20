package types

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"github.com/yggdrasil-network/yggdrasil-go/contrib/netstack"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

const NameMappingSuffix = ".pk.ygg"

type NameResolver struct {
	resolver *net.Resolver
}

func NewNameResolver(stack *netstack.YggdrasilNetstack, nameserver string) *NameResolver {
	res := &NameResolver{
		resolver: &net.Resolver{
			PreferGo: true,
		},
	}
	if nameserver != "" {
		res.resolver.Dial = func(ctx context.Context, network, address string) (net.Conn, error) { // nolint:staticcheck
			address = fmt.Sprintf("[%s]:53", nameserver) // nolint:staticcheck
			if nameserver == "" {
				return nil, fmt.Errorf("no nameserver configured")
			}
			return stack.DialContext(ctx, network, address)
		}
	}
	return res
}

func (r *NameResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	if strings.HasSuffix(name, NameMappingSuffix) {
		name = strings.TrimSuffix(name, NameMappingSuffix)
		var pk [ed25519.PublicKeySize]byte
		if b, err := hex.DecodeString(name); err != nil {
			return nil, nil, fmt.Errorf("hex.DecodeString: %w", err)
		} else {
			copy(pk[:], b)
			return ctx, net.IP(address.AddrForKey(pk[:])[:]), nil
		}
	}
	ip := net.ParseIP(name)
	if ip == nil {
		addrs, err := r.resolver.LookupIP(ctx, "ip6", name)
		if err != nil {
			fmt.Println("failed to lookup", name, "due to error:", err)
			return nil, nil, fmt.Errorf("failed to lookup %q: %s", name, err)
		}
		if len(addrs) == 0 {
			fmt.Println("failed to lookup", name, "due to no addresses")
			return nil, nil, fmt.Errorf("no addresses for %q", name)
		}
		return ctx, addrs[0], nil
	}
	return ctx, ip, nil
}
