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

## [0.3.12] - 2019-11-24
### Added
- New API functions `SetMaximumSessionMTU` and `GetMaximumSessionMTU`
- New command line parameters `-address` and `-subnet` for getting the address/subnet from the config file, for use with `-useconffile` or `-useconf`
- A warning is now produced in the Yggdrasil output at startup when the MTU in the config is invalid or has been adjusted for some reason

### Changed
- On Linux, outgoing `InterfacePeers` connections now use `SO_BINDTODEVICE` to prefer an outgoing interface
- The `genkeys` utility is now in `cmd` rather than `misc`

### Fixed
- A data race condition has been fixed when updating session coordinates
- A crash when shutting down when no multicast interfaces are configured has been fixed
- A deadlock when calling `AddPeer` multiple times has been fixed
- A typo in the systemd unit file (for some Linux packages) has been fixed
- The NodeInfo and admin socket now report `unknown` correctly when no build name/version is available in the environment at build time
- The MTU calculation now correctly accounts for ethernet headers when running in TAP mode

## [0.3.11] - 2019-10-25
### Added
- Support for TLS listeners and peers has been added, allowing the use of `tls://host:port` in `Peers`, `InterfacePeers` and `Listen` configuration settings - this allows hiding Yggdrasil peerings inside regular TLS connections

### Changed
- Go 1.13 or later is now required for building Yggdrasil
- Some exported API functions have been updated to work with standard Go interfaces:
  - `net.Conn` instead of `yggdrasil.Conn`
  - `net.Dialer` (the interface it would satisfy if it wasn't a concrete type) instead of `yggdrasil.Dialer`
  - `net.Listener` instead of `yggdrasil.Listener`
- Session metadata is now updated correctly when a search completes for a node to which we already have an open session
- Multicast module reloading behaviour has been improved

### Fixed
- An incorrectly held mutex in the crypto-key routing code has been fixed
- Multicast module no longer opens a listener socket if no multicast interfaces are configured

## [0.3.10] - 2019-10-10
### Added
- The core library now includes several unit tests for peering and `yggdrasil.Conn` connections

### Changed
- On recent Linux kernels, Yggdrasil will now set the `tcp_congestion_control` algorithm used for its own TCP sockets to [BBR](https://github.com/google/bbr), which reduces latency under load
- The systemd service configuration in `contrib` (and, by extension, some of our packages) now attempts to load the `tun` module, in case TUN/TAP support is available but not loaded, and it restricts Yggdrasil to the `CAP_NET_ADMIN` capability for managing the TUN/TAP adapter, rather than letting it do whatever the (typically `root`) user can do

### Fixed
- The `yggdrasil.Conn.RemoteAddr()` function no longer blocks, fixing a deadlock when CKR is used while under heavy load

## [0.3.9] - 2019-09-27
### Added
- Yggdrasil will now complain more verbosely when a peer URI is incorrectly formatted
- Soft-shutdown methods have been added, allowing a node to shut down gracefully when terminated
- New multicast interval logic which sends multicast beacons more often when Yggdrasil is first started to increase the chance of finding nearby nodes quickly after startup

### Changed
- The switch now buffers packets more eagerly in an attempt to give the best link a chance to send, which appears to reduce packet reordering when crossing aggregate sets of peerings
- Substantial amounts of the codebase have been refactored to use the actor model, which should substantially reduce the chance of deadlocks
- Nonce tracking in sessions has been modified so that memory usage is reduced whilst still only allowing duplicate packets within a small window
- Soft-reconfiguration support has been simplified using new actor functions
- The garbage collector threshold has been adjusted for mobile builds
- The maximum queue size is now managed exclusively by the switch rather than by the core

### Fixed
- The broken `hjson-go` dependency which affected builds of the previous version has now been resolved in the module manifest
- Some minor memory leaks in the switch have been fixed, which improves memory usage on mobile builds
- A memory leak in the add-peer loop has been fixed
- The admin socket now reports the correct URI strings for SOCKS peers in `getPeers`
- A race condition when dialing a remote node by both the node address and routed prefix simultaneously has been fixed
- A race condition between the router and the dial code resulting in a panic has been fixed
- A panic which could occur when the TUN/TAP interface disappears (e.g. during soft-shutdown) has been fixed
- A bug in the semantic versioning script which accompanies Yggdrasil for builds has been fixed
- A panic which could occur when the TUN/TAP interface reads an undersized/corrupted packet has been fixed

### Removed
- A number of legacy debug functions have now been removed and a number of exported API functions are now better documented

## [0.3.8] - 2019-08-21
### Changed
- Yggdrasil can now send multiple packets from the switch at once, which results in improved throughput with smaller packets or lower MTUs
- Performance has been slightly improved by not allocating cancellations where not necessary
- Crypto-key routing options have been renamed for clarity
  - `IPv4Sources` is now named `IPv4LocalSubnets`
  - `IPv6Sources` is now named `IPv6LocalSubnets`
  - `IPv4Destinations` is now named `IPv4RemoteSubnets`
  - `IPv6Destinations` is now named `IPv6RemoteSubnets`
  - The old option names will continue to be accepted by the configuration parser for now but may not be indefinitely
- When presented with multiple paths between two nodes, the switch now prefers the most recently used port when possible instead of the least recently used, helping to reduce packet reordering
- New nonce tracking should help to reduce the number of packets dropped as a result of multiple/aggregate paths or congestion control in the switch

### Fixed
- A deadlock was fixed in the session code which could result in Yggdrasil failing to pass traffic after some time

### Security
- Address verification was not strict enough, which could result in a malicious session sending traffic with unexpected or spoofed source or destination addresses which Yggdrasil could fail to reject
  - Versions `0.3.6` and `0.3.7` are vulnerable - users of these versions should upgrade as soon as possible
  - Versions `0.3.5` and earlier are not affected

## [0.3.7] - 2019-08-14
### Changed
- The switch should now forward packets along a single path more consistently in cases where congestion is low and multiple equal-length paths exist, which should improve stability and result in fewer out-of-order packets
- Sessions should now be more tolerant of out-of-order packets, by replacing a bitmask with a variable sized heap+map structure to track recently received nonces, which should reduce the number of packets dropped due to reordering when multiple paths are used or multiple independent flows are transmitted through the same session
- The admin socket can no longer return a dotfile representation of the known parts of the network, this could be rebuilt by clients using information from `getSwitchPeers`,`getDHT` and `getSessions`

### Fixed
- A number of significant performance regressions introduced in version 0.3.6 have been fixed, resulting in better performance
- Flow labels are now used to prioritise traffic flows again correctly
- In low-traffic scenarios where there are multiple peerings between a pair of nodes, Yggdrasil now prefers the most active peering instead of the least active, helping to reduce packet reordering
- The `Listen` statement, when configured as a string rather than an array, will now be parsed correctly
- The admin socket now returns `coords` as a correct array of unsigned 64-bit integers, rather than the internal representation
- The admin socket now returns `box_pub_key` in string format again
- Sessions no longer leak/block when no listener (e.g. TUN/TAP) is configured
- Incoming session connections no longer block when a session already exists, which results in less leaked goroutines
- Flooded sessions will no longer block other sessions
- Searches are now cleaned up properly and a couple of edge-cases with duplicate searches have been fixed
- A number of minor allocation and pointer fixes

## [0.3.6] - 2019-08-03
### Added
- Yggdrasil now has a public API with interfaces such as `yggdrasil.ConnDialer`, `yggdrasil.ConnListener` and `yggdrasil.Conn` for using Yggdrasil as a transport directly within applications
- Session gatekeeper functions, part of the API, which can be used to control whether to allow or reject incoming or outgoing sessions dynamically (compared to the previous fixed whitelist/blacklist approach)
- Support for logging to files or syslog (where supported)
- Platform defaults now include the ability to set sane defaults for multicast interfaces

### Changed
- Following a massive refactoring exercise, Yggdrasil's codebase has now been broken out into modules
- Core node functionality in the `yggdrasil` package with a public API
  - This allows Yggdrasil to be integrated directly into other applications and used as a transport
  - IP-specific code has now been moved out of the core `yggdrasil` package, making Yggdrasil effectively protocol-agnostic
- Multicast peer discovery functionality is now in the `multicast` package
- Admin socket functionality is now in the `admin` package and uses the Yggdrasil public API
- TUN/TAP, ICMPv6 and all IP-specific functionality is now in the `tuntap` package
- `PPROF` debug output is now sent to `stderr` instead of `stdout`
- Node IPv6 addresses on macOS are now configured as `secured`
- Upstream dependency references have been updated, which includes a number of fixes in the Water library

### Fixed
- Multicast discovery is no longer disabled if the nominated interfaces aren't available on the system yet, e.g. during boot
- Multicast interfaces are now re-evaluated more frequently so that Yggdrasil doesn't need to be restarted to use interfaces that have become available since startup
- Admin socket error cases are now handled better
- Various fixes in the TUN/TAP module, particularly surrounding Windows platform support
- Invalid keys will now cause the node to fail to start, rather than starting but silently not working as before
- Session MTUs are now always calculated correctly, in some cases they were incorrectly defaulting to 1280 before
- Multiple searches now don't take place for a single connection
- Concurrency bugs fixed
- Fixed a number of bugs in the ICMPv6 neighbor solicitation in the TUN/TAP code
- A case where peers weren't always added correctly if one or more peers were unreachable has been fixed
- Searches which include the local node are now handled correctly
- Lots of small bug tweaks and clean-ups throughout the codebase

## [0.3.5] - 2019-03-13
### Fixed
- The `AllowedEncryptionPublicKeys` option has now been fixed to handle incoming connections properly and no longer blocks outgoing connections (this was broken in v0.3.4)
- Multicast TCP listeners will now be stopped correctly when the link-local address on the interface changes or disappears altogether

## [0.3.4] - 2019-03-12
### Added
- Support for multiple listeners (although currently only TCP listeners are supported)
- New multicast behaviour where each multicast interface is given its own link-local listener and does not depend on the `Listen` configuration
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
- Iterative DHT (added sometime between v0.1.0 and here).
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
