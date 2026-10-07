# Plan 9 / 9front Support for Yggdrasil

This directory provides scripts to install and manage Yggdrasil on Plan 9 (9front).

## Installation

1. Copy the cross-compiled `yggdrasil` and `yggdrasilctl` binaries to your 9front machine (e.g. into `/n/9fat` or any temporary directory).
2. Run `install.rc`:

```rc
rc install.rc /n/9fat
```

The installer performs the following:
- Copies `yggdrasil` and `yggdrasilctl` to `/$cputype/bin/` (mode 755).
- Generates a default configuration at `/lib/yggdrasil.conf` (or preserves/copies an existing configuration) with mode 600.
- Installs `/rc/bin/yggstart` and `/rc/bin/yggkill`.
- Adds an entry in `/rc/bin/cpurc.local` to start Yggdrasil automatically at system boot.

## Service Management

- **Start**: `/rc/bin/yggstart` (configures loopback on `127.1 /128` if not already present, then launches `yggdrasil -useconffile /lib/yggdrasil.conf` in the background).
- **Stop**: `/rc/bin/yggkill` (sends the `interrupt` note to cleanly terminate threads, followed by `kill` note if needed, and unbinds lingering packet interfaces).
- **Status**: `yggdrasilctl getself` or `yggdrasilctl getpeers`.

## Notes

- **Admin Socket**: The admin socket listens on TCP loopback (`tcp://localhost:9001`) and requires a configured loopback interface (`ipconfig loopback /dev/null 127.1 /128`).
- **Shutdown**: Do not use `slay` to stop Yggdrasil. `slay` writes `kill` to `/proc/$p/ctl`, which does not stop Go runtime shared-memory threads and leaves interfaces bound. Use `/rc/bin/yggkill`.
- **Peers**: A newly generated `/lib/yggdrasil.conf` has an empty `Peers: []` list. While local multicast discovers peers on your LAN, communicating with the wider mesh requires adding remote or public peer URIs (e.g. from https://publicpeers.neilalexander.dev/) to `Peers: [...]`. Note that local multicast peers on other OSes may also drop incoming ICMPv6 or TCP packets if their host firewalls block inbound traffic on virtual interfaces.

## Diagnosing missing ping or TCP traffic

Use `diagnostic.rc` with `diagnostic-tcp.go` against a **known working Yggdrasil peer**. The target must have an open TCP port that another Yggdrasil node can reach. For an HTTP test, start a temporary server on a Linux peer, replacing `ADDRESS` with that peer's Yggdrasil IPv6 address:

```sh
python3 -m http.server 18080 --bind ADDRESS
```

Build the probe on a machine with Go, matching the 9front machine's architecture (`amd64` or `386`), then copy the probe and `diagnostic.rc` to the 9front machine:

```sh
GOOS=plan9 GOARCH=amd64 CGO_ENABLED=0 go build -o ygg-diagnostic-tcp ./contrib/plan9/diagnostic-tcp.go
```

On 9front, run the following from the directory containing those two files. Replace `ADDRESS` with the numeric Yggdrasil IPv6 address of the test server:

```rc
rc diagnostic.rc ADDRESS 18080 ./ygg-diagnostic-tcp http > /tmp/ygg-diagnostic.txt >[2=1]
```

For a known open TCP port that does not speak HTTP, omit `http`. The diagnostic runs ten ICMP requests and five TCP connections. It records Yggdrasil build and peer state, the `200::/7` route, packet interface counters, and target-only ICMP/TCP headers. It does not read the configuration file or private key, or capture packet payloads. It reads `/net/ipifc/N/snoop` through `snoopy`; do not copy or read `/net/ipifc/N/data`, which is Yggdrasil's live packet queue.

Before sharing `/tmp/ygg-diagnostic.txt`, review the peer URIs, Yggdrasil addresses, and system name. Please also report the 9front image/build date or source revision, the Go version used to build Yggdrasil, and whether the target works from another Yggdrasil node. If `getpeers` has no `Up` peer, configure a reachable peer first; multicast discovery only covers the local link. A missing `200::/7` route points to interface setup. Outbound echo requests or TCP SYNs without replies point to the peer path, target service, or firewall. A SYN and SYN-ACK in the packet trace with a failed TCP dial points closer to the 9front TCP path.

If the 9front trace shows requests without replies, compare it with a header-only capture on the Linux target during the same run. Replace `CLIENT` with the 9front Yggdrasil IPv6 address and `tun0` with the target's Yggdrasil interface:

```sh
sudo tcpdump -ni tun0 'host CLIENT and (icmp6 or tcp port 18080)'
```

Requests absent at the target indicate a path or peer problem; requests present without replies indicate a target firewall or service problem.
