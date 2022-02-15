package types

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type TCPMapping struct {
	Listen *net.TCPAddr
	Mapped *net.TCPAddr
}

type TCPMappings []TCPMapping

func (m *TCPMappings) String() string {
	return ""
}

func (m *TCPMappings) Set(value string) error {
	tokens := strings.Split(value, ":")
	if len(tokens) > 2 {
		tokens = strings.SplitN(value, ":", 2)
		host, port, err := net.SplitHostPort(tokens[1])
		if err != nil {
			return fmt.Errorf("failed to split host and port: %w", err)
		}
		tokens = append(tokens[:1], host, port)
	}
	listenport, err := strconv.Atoi(tokens[0])
	if err != nil {
		return fmt.Errorf("listen port is invalid: %w", err)
	}
	if listenport == 0 {
		return fmt.Errorf("listen port must not be zero")
	}
	mapping := TCPMapping{
		Listen: &net.TCPAddr{
			Port: listenport,
		},
		Mapped: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: listenport,
		},
	}
	tokens = tokens[1:]
	if len(tokens) > 0 {
		mappedaddr := net.ParseIP(tokens[0])
		if mappedaddr == nil {
			return fmt.Errorf("invalid mapped address %q", tokens[0])
		}
		mapping.Mapped.IP = mappedaddr
		tokens = tokens[1:]
	}
	if len(tokens) > 0 {
		mappedport, err := strconv.Atoi(tokens[0])
		if err != nil {
			return fmt.Errorf("mapped port is invalid: %w", err)
		}
		if mappedport == 0 {
			return fmt.Errorf("mapped port must not be zero")
		}
		mapping.Mapped.Port = mappedport
	}
	*m = append(*m, mapping)
	return nil
}
