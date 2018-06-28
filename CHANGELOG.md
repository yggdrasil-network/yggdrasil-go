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

## [Unreleased]
### Added
- Begin keeping changelog (incomplete and possibly inaccurate information before this point).
- Build RPMs in CircleCI using alien.

### Changed
- Local backpressure improvements.
- Change `box_pub_key` to `key` in admin API.
- Session cleanup.

## [0.2.2] - 2018-06-21
### Added
- Add yggdrasilconf for testing with vyatta-yggdrasil.
- Add a randomized retry delay after TCP disconnects, to prevent synchronization livelocks.

### Changed
- Update build script ot strip by default, allow debug `-d` and UPX `-u` flags.
- Start pprof in debug builds based on an environment variable (e.g. `PPROFLISTEN=localhost:6060`), instead of a flag.

### Fixed
- Fix typo in big-endian BOM.

## [0.2.1] - 2018-06-15
### Changed
- The address range was moved from `fd00::/8` to `200::/7`.

### Fixed
- UTF-16 conversion for configuration files.
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
- Per peer bandwidth estimation.

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

