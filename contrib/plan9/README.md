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
