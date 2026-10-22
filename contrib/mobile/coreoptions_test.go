package mobile

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// fullConfig returns a configuration which populates every field that the
// desktop daemon (cmd/yggdrasil/main.go) turns into a core setup option.
func fullConfig() *config.NodeConfig {
	return &config.NodeConfig{
		Peers:             []string{"tls://127.0.0.1:1001"},
		InterfacePeers:    map[string][]string{"eth0": {"tls://127.0.0.1:1002"}},
		Listen:            []string{"tls://127.0.0.1:0"},
		AllowedPublicKeys: []string{hex.EncodeToString(make([]byte, ed25519.PublicKeySize))},
		GroupPassword:     "correct horse battery staple",
		NodeInfo:          map[string]interface{}{"name": "mobile-parity"},
		NodeInfoPrivacy:   true,
	}
}

func optionType(opt core.SetupOption) string {
	return fmt.Sprintf("%T", opt)
}

func optionTypes(opts []core.SetupOption) []string {
	types := make([]string, 0, len(opts))
	for _, opt := range opts {
		types = append(types, optionType(opt))
	}
	return types
}

func hasOptionType(types []string, want string) bool {
	for _, typ := range types {
		if typ == want {
			return true
		}
	}
	return false
}

// TestCoreOptionsMatchDesktopFields pins the exact set of options that the
// mobile code path hands to core.New for a fully populated configuration.
// The expected list mirrors the option block of cmd/yggdrasil/main.go; if a
// field is added there but not here, the mobile node silently ignores it.
func TestCoreOptionsMatchDesktopFields(t *testing.T) {
	cfg := fullConfig()
	opts := coreOptions(cfg)

	got := optionTypes(opts)
	want := []string{
		"core.NodeInfo",
		"core.NodeInfoPrivacy",
		"core.PeerFilter",
		"core.ListenAddress",
		"core.Peer",
		"core.GroupPassword",
		"core.Peer",
		"core.AllowedPublicKey",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected core option set\n got: %v\nwant: %v", got, want)
	}

	var (
		seenNodeInfo        bool
		seenNodeInfoPrivacy bool
		seenGroupPassword   bool
		seenListen          bool
		seenPeer            bool
		seenInterfacePeer   bool
		seenAllowedKey      bool
		seenPeerFilter      bool
	)
	for _, opt := range opts {
		switch v := opt.(type) {
		case core.NodeInfo:
			seenNodeInfo = true
			if !reflect.DeepEqual(map[string]interface{}(v), cfg.NodeInfo) {
				t.Errorf("NodeInfo option = %v, want %v", v, cfg.NodeInfo)
			}
		case core.NodeInfoPrivacy:
			seenNodeInfoPrivacy = true
			if bool(v) != cfg.NodeInfoPrivacy {
				t.Errorf("NodeInfoPrivacy option = %v, want %v", bool(v), cfg.NodeInfoPrivacy)
			}
		case core.GroupPassword:
			seenGroupPassword = true
			if string(v) != cfg.GroupPassword {
				t.Errorf("GroupPassword option = %q, want %q", string(v), cfg.GroupPassword)
			}
		case core.ListenAddress:
			seenListen = true
			if string(v) != cfg.Listen[0] {
				t.Errorf("ListenAddress option = %q, want %q", string(v), cfg.Listen[0])
			}
		case core.Peer:
			if v.SourceInterface == "" {
				seenPeer = true
				if v.URI != cfg.Peers[0] {
					t.Errorf("Peer option = %q, want %q", v.URI, cfg.Peers[0])
				}
				continue
			}
			seenInterfacePeer = true
			if v.URI != cfg.InterfacePeers["eth0"][0] || v.SourceInterface != "eth0" {
				t.Errorf("InterfacePeers option = %+v, want URI %q on eth0", v, cfg.InterfacePeers["eth0"][0])
			}
		case core.AllowedPublicKey:
			seenAllowedKey = true
			if !reflect.DeepEqual([]byte(v), mustHex(t, cfg.AllowedPublicKeys[0])) {
				t.Errorf("AllowedPublicKey option = %x, want %s", []byte(v), cfg.AllowedPublicKeys[0])
			}
		case core.PeerFilter:
			seenPeerFilter = true
			// The filter must keep 200::/7 out of reach, exactly like the
			// desktop daemon does.
			if v(net.ParseIP("200::1")) {
				t.Error("PeerFilter accepted 200::1, want rejected")
			}
			if !v(net.ParseIP("400::1")) {
				t.Error("PeerFilter rejected 400::1 (outside 200::/7), want accepted")
			}
			if !v(net.ParseIP("100::1")) {
				t.Error("PeerFilter rejected 100::1 (outside 200::/7), want accepted")
			}
		default:
			t.Errorf("unexpected core option type %T", opt)
		}
	}
	for name, seen := range map[string]bool{
		"NodeInfo":         seenNodeInfo,
		"NodeInfoPrivacy":  seenNodeInfoPrivacy,
		"GroupPassword":    seenGroupPassword,
		"ListenAddress":    seenListen,
		"Peer":             seenPeer,
		"InterfacePeers":   seenInterfacePeer,
		"AllowedPublicKey": seenAllowedKey,
		"PeerFilter":       seenPeerFilter,
	} {
		if !seen {
			t.Errorf("core option %s was not produced", name)
		}
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

// TestCoreOptionsGroupPasswordOmittedWhenEmpty checks that an empty
// GroupPassword does not produce a GroupPassword option, matching the
// "if cfg.GroupPassword != \"\"" guard in the desktop daemon.
func TestCoreOptionsGroupPasswordOmittedWhenEmpty(t *testing.T) {
	cfg := fullConfig()
	cfg.GroupPassword = ""
	types := optionTypes(coreOptions(cfg))
	if hasOptionType(types, "core.GroupPassword") {
		t.Fatalf("empty GroupPassword must not produce a GroupPassword option, got %v", types)
	}

	empty := optionTypes(coreOptions(&config.NodeConfig{}))
	want := []string{"core.NodeInfo", "core.NodeInfoPrivacy", "core.PeerFilter"}
	if !reflect.DeepEqual(empty, want) {
		t.Fatalf("unexpected minimal option set\n got: %v\nwant: %v", empty, want)
	}
}

// TestCoreOptionsCoverDaemonFields parses the desktop daemon and collects every
// core option that is built from a config field. Every such option must also be
// produced by the mobile code path -- this is the regression guard for the
// mobile/desktop parity bug class (GroupPassword, NodeInfo, NodeInfoPrivacy
// were previously dropped on mobile).
func TestCoreOptionsCoverDaemonFields(t *testing.T) {
	daemonFields := daemonConfigOptions(t)
	if len(daemonFields) == 0 {
		t.Fatal("no config-derived core options found in cmd/yggdrasil/main.go; the parity guard is broken")
	}
	types := optionTypes(coreOptions(fullConfig()))
	for name := range daemonFields {
		if !hasOptionType(types, "core."+name) {
			t.Errorf("desktop daemon passes core.%s(cfg....) but the mobile path does not; mobile nodes would ignore it", name)
		}
	}
}

// daemonConfigOptions returns the names of core setup options that
// cmd/yggdrasil/main.go derives from a field of the parsed config.
func daemonConfigOptions(t *testing.T) map[string]bool {
	t.Helper()
	path := filepath.Join("..", "..", "cmd", "yggdrasil", "main.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("cannot parse desktop daemon %s: %v", path, err)
	}
	found := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := fn.X.(*ast.Ident)
		if !ok || pkg.Name != "core" {
			return true
		}
		// core.New consumes the options, it is not one of them.
		if fn.Sel.Name == "New" {
			return true
		}
		for _, arg := range call.Args {
			sel, ok := arg.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if recv, ok := sel.X.(*ast.Ident); ok && recv.Name == "cfg" {
				found[fn.Sel.Name] = true
			}
		}
		return true
	})
	return found
}

// TestCoreOptionsAcceptedByCore starts a real core with the options built from
// a generated config, so that the option list is known to be valid and the
// node still starts with the parity fix in place.
func TestCoreOptionsAcceptedByCore(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.GroupPassword = "group-password"
	cfg.NodeInfo = map[string]interface{}{"name": "mobile-parity"}
	cfg.NodeInfoPrivacy = true

	logger := log.New(io.Discard, "", 0)
	logger.EnableLevel("error")
	c, err := core.New(cfg.Certificate, logger, coreOptions(cfg)...)
	if err != nil {
		t.Fatalf("core.New rejected the mobile option list: %v", err)
	}
	defer c.Stop()
	if addr := c.Address(); addr == nil {
		t.Fatal("core has no address")
	}
}
