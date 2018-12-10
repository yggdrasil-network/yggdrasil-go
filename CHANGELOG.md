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
