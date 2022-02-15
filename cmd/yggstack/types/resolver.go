package types

import (
	"context"
	"fmt"
	"net"

	"github.com/yggdrasil-network/yggdrasil-go/contrib/netstack"
)

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
