# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

<!-- Use this as a template
## [X.Y.Z] - YYYY-MM-DD
### Added
- for new features.

### Changed
- for changes in existing functionality.

### Deprecated
- for soon-to-be removed features.

### Removed
- for now removed features.

### Fixed
- for any bug fixes.

### Security
- in case of vulnerabilities.
-->

## [0.3.4] - 2019-03-12
### Added
- Support for multiple listeners (although currently only TCP listeners are supported)
- New multicast behaviour where each multicast interface is given it's own link-local listener and does not depend on the `Listen` configuration
- Blocking detection in the switch to avoid parenting a blocked peer
- Support for adding and removing listeners and multicast interfaces when reloading configuration during runtime
- Yggdrasil will now attempt to clean up UNIX admin sockets on startup if left behind by a previous crash
- Admin socket `getTunnelRouting` and `setTunnelRouting` calls for enabling and disabling crypto-key routing during runtime
- On macOS, Yggdrasil will now try to wake up AWDL on start-up when `awdl0` is a configured multicast interface, to keep it awake after system sleep, and to stop waking it when no longer needed
- Added `LinkLocalTCPPort` option for controlling the port number that link-local TCP listeners will listen on by default when setting up `MulticastInterfaces` (a node restart is currently required for changes to `LinkLocalTCPPort` to take effect - it cannot be updated by reloading config during runtime)

### Changed
- The `Listen` configuration statement is now an array instead of a string
- The `Listen` configuration statement should now conform to the same formatting as peers with the protocol prefix, e.g. `tcp://[::]:0`
- Session workers are now non-blocking
- Multicast interval is now fixed at every 15 seconds and network interfaces are reevaluated for eligibility on each interval (where before the interval depended upon the number of configured multicast interfaces and evaluation only took place at startup)
- Dead connections are now closed in the link handler as opposed to the switch
- Peer forwarding is now prioritised instead of randomised

### Fixed
- Admin socket `getTunTap` call now returns properly instead of claiming no interface is enabled in all cases
- Handling of `getRoutes` etc in `yggdrasilctl` is now working
- Local interface names are no longer leaked in multicast packets
- Link-local TCP connections, particularly those initiated because of multicast beacons, are now always correctly scoped for the target interface
- Yggdrasil now correctly responds to multicast interfaces going up and down during runtime

## [0.3.3] - 2019-02-18
### Added
- Dynamic reconfiguration, which allows reloading the configuration file to make changes during runtime by sending a `SIGHUP` signal (note: this only works with `-useconffile` and not `-useconf` and currently reconfiguring TUN/TAP is not supported)
- Support for building Yggdrasil as an iOS or Android framework if the appropriate tools (e.g. `gomobile`/`gobind` + SDKs) are available
- Connection contexts used for TCP connections which allow more exotic socket options to be set, e.g.
  - Reusing the multicast socket to allow multiple running Yggdrasil instances without having to disable multicast
  - Allowing supported Macs to peer with other nearby Macs that aren't even on the same Wi-Fi network using AWDL
- Flexible logging support, which allows for logging at different levels of verbosity

### Changed
- Switch changes to improve parent selection
- Node configuration is now stored centrally, rather than having fragments/copies distributed at startup time
- Significant refactoring in various areas, including for link types (TCP, AWDL etc), generic streams and adapters
- macOS builds through CircleCI are now 64-bit only

### Fixed
- Simplified `systemd` service now in `contrib`

### Removed
- `ReadTimeout` option is now deprecated

## [0.3.2] - 2018-12-26
### Added
- The admin socket is now multithreaded, greatly improving performance of the crawler and allowing concurrent lookups to take place
- The ability to hide NodeInfo defaults through either setting the `NodeInfoPrivacy` option or through setting individual `NodeInfo` attributes to `null`

### Changed
- The `armhf` build now targets ARMv6 instead of ARMv7, adding support for Raspberry Pi Zero and other older models, amongst others

### Fixed
- DHT entries are now populated using a copy in memory to fix various potential DHT bugs
- DHT traffic should now throttle back exponentially to reduce idle traffic
- Adjust how nodes are inserted into the DHT which should help to reduce some incorrect DHT traffic
- In TAP mode, the NDP target address is now correctly used when populating the peer MAC table. This fixes serious connectivity problems when in TAP mode, particularly on BSD
- In TUN mode, ICMPv6 packets are now ignored whereas they were incorrectly processed before

## [0.3.1] - 2018-12-17
### Added
- Build name and version is now imprinted onto the binaries if available/specified during build
- Ability to disable admin socket with `AdminListen: none`
- `AF_UNIX` domain sockets for the admin socket
- Cache size restriction for crypto-key routes
- `NodeInfo` support for specifying node information, e.g. node name or contact, which can be used in network crawls or surveys
- `getNodeInfo` request added to admin socket
- Adds flags `-c`, `-l` and `-t` to `build` script for specifying `GCFLAGS`, `LDFLAGS` or whether to keep symbol/DWARF tables

### Changed
- Default `AdminListen` in newly generated config is now `unix:///var/run/yggdrasil.sock`
- Formatting of `getRoutes` in the admin socket has been improved
- Debian package now adds `yggdrasil` group to assist with `AF_UNIX` admin socket permissions
- Crypto, address and other utility code refactored into separate Go packages

### Fixed
- Switch peer convergence is now much faster again (previously it was taking up to a minute once the peering was established)
- `yggdrasilctl` is now less prone to crashing when parameters are specified incorrectly
- Panic fixed when `Peers` or `InterfacePeers` was commented out

## [0.3.0] - 2018-12-12
### Added
- Crypto-key routing support for tunnelling both IPv4 and IPv6 over Yggdrasil
- Add advanced `SwitchOptions` in configuration file for tuning the switch
- Add `dhtPing` to the admin socket to aid in crawling the network
- New macOS .pkgs built automatically by CircleCI
- Add Dockerfile to repository for Docker support
- Add `-json` command line flag for generating and normalising configuration in plain JSON instead of HJSON
- Build name and version numbers are now imprinted onto the build, accessible through `yggdrasil -version` and `yggdrasilctl getSelf`
- Add ability to disable admin socket by setting `AdminListen` to `"none"`
- `yggdrasilctl` now tries to look for the default configuration file to find `AdminListen` if `-endpoint` is not specified
- `yggdrasilctl` now returns more useful logging in the event of a fatal error

### Changed
- Switched to Chord DHT (instead of Kademlia, although still compatible at the protocol level)
- The `AdminListen` option and `yggdrasilctl` now default to `unix:///var/run/yggdrasil.sock` on BSDs, macOS and Linux
- Cleaned up some of the parameter naming in the admin socket
- Latency-based parent selection for the switch instead of uptime-based (should help to avoid high latency links somewhat)
- Real peering endpoints now shown in the admin socket `getPeers` call to help identify peerings
- Reuse the multicast port on supported platforms so that multiple Yggdrasil processes can run
- `yggdrasilctl` now has more useful help text (with `-help` or when no arguments passed)

### Fixed
- Memory leaks in the DHT fixed
- Crash fixed where the ICMPv6 NDP goroutine would incorrectly start in TUN mode
- Removing peers from the switch table if they stop sending switch messages but keep the TCP connection alive

## [0.2.7] - 2018-10-13
### Added
- Session firewall, which makes it possible to control who can open sessions with your node
- Add `getSwitchQueues` to admin socket
- Add `InterfacePeers` for configuring static peerings via specific network interfaces
- More output shown in `getSwitchPeers`
- FreeBSD service script in `contrib`

## Changed
- CircleCI builds are now built with Go 1.11 instead of Go 1.9

## Fixed
- Race condition in the switch table, reported by trn
- Debug builds are now tested by CircleCI as well as platform release builds
- Port number fixed on admin graph from unknown nodes

## [0.2.6] - 2018-07-31
### Added
- Configurable TCP timeouts to assist in peering over Tor/I2P
- Prefer IPv6 flow label when extending coordinates to sort backpressure queues
- `arm64` builds through CircleCI

### Changed
- Sort dot graph links by integer value

## [0.2.5] - 2018-07-19
### Changed
- Make `yggdrasilctl` less case sensitive
- More verbose TCP disconnect messages

### Fixed
- Fixed debug builds
- Cap maximum MTU on Linux in TAP mode
- Process successfully-read TCP traffic before checking for / handling errors (fixes EOF behavior)

## [0.2.4] - 2018-07-08
### Added
- Support for UNIX domain sockets for the admin socket using `unix:///path/to/file.sock`
- Centralised platform-specific defaults

### Changed
- Backpressure tuning, including reducing resource consumption

### Fixed
- macOS local ping bug, which previously prevented you from pinging your own `utun` adapter's IPv6 address

## [0.2.3] - 2018-06-29
### Added
- Begin keeping changelog (incomplete and possibly inaccurate information before this point).
- Build RPMs in CircleCI using alien. This provides package support for Fedora, Red Hat Enterprise Linux, CentOS and other RPM-based distributions.

### Changed
- Local backpressure improvements.
- Change `box_pub_key` to `key` in admin API for simplicity.
- Session cleanup.

## [0.2.2] - 2018-06-21
### Added
- Add `yggdrasilconf` utility for testing with the `vyatta-yggdrasil` package.
- Add a randomized retry delay after TCP disconnects, to prevent synchronization livelocks.

### Changed
- Update build script to strip by default, which significantly reduces the size of the binary.
- Add debug `-d` and UPX `-u` flags to the `build` script.
- Start pprof in debug builds based on an environment variable (e.g. `PPROFLISTEN=localhost:6060`), instead of a flag.

### Fixed
- Fix typo in big-endian BOM so that both little-endian and big-endian UTF-16 files are detected correctly.

## [0.2.1] - 2018-06-15
### Changed
- The address range was moved from `fd00::/8` to `200::/7`. This range was chosen as it is marked as deprecated. The change prevents overlap with other ULA privately assigned ranges.

### Fixed
- UTF-16 detection conversion for configuration files, which can particularly be a problem on Windows 10 if a configuration file is generated from within PowerShell.
- Fixes to the Debian package control file.
- Fixes to the launchd service for macOS.
- Fixes to the DHT and switch.

## [0.2.0] - 2018-06-13
### Added
- Exchange version information during connection setup, to prevent connections with incompatible versions.

### Changed
- Wire format changes (backwards incompatible).
- Less maintenance traffic per peer.
- Exponential back-off for DHT maintenance traffic (less maintenance traffic for known good peers).
- Iterative DHT (added some time between v0.1.0 and here).
- Use local queue sizes for a sort of local-only backpressure routing, instead of the removed bandwidth estimates, when deciding where to send a packet.

### Removed
- UDP peering, this may be added again if/when a better implementation appears.
- Per peer bandwidth estimation, as this has been replaced with an early local backpressure implementation.

## [0.1.0] - 2018-02-01
### Added
- Adopt semantic versioning.

### Changed
- Wire format changes (backwards incompatible).
- Many other undocumented changes leading up to this release and before the next one.

## [0.0.1] - 2017-12-28
### Added
- First commit.
- Initial public release.
