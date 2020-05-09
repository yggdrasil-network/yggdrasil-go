# Yggdrasil

[![CircleCI](https://circleci.com/gh/yggdrasil-network/yggdrasil-go.svg?style=shield&circle-token=:circle-token
)](https://circleci.com/gh/yggdrasil-network/yggdrasil-go)

## Introduction

Yggdrasil is an early-stage implementation of a fully end-to-end encrypted IPv6
network. It is lightweight, self-arranging, supported on multiple platforms and
allows pretty much any IPv6-capable application to communicate securely with
other Yggdrasil nodes. Yggdrasil does not require you to have IPv6 Internet
connectivity - it also works over IPv4.

Although Yggdrasil shares many similarities with
[cjdns](https://github.com/cjdelisle/cjdns), it employs a different routing
algorithm based on a globally-agreed spanning tree and greedy routing in a
metric space, and aims to implement some novel local backpressure routing
techniques. In theory, Yggdrasil should scale well on networks with
internet-like topologies.

## Supported Platforms

We actively support the following platforms, and packages are available for
some of the below:

- Linux
  - `.deb` and `.rpm` packages are built by CI for Debian and Red Hat-based
    distributions
  - Arch, Nix, Void packages also available within their respective repositories
- macOS
  - `.pkg` packages are built by CI
- Ubiquiti EdgeOS
  - `.deb` Vyatta packages are built by CI
- Windows
- FreeBSD
- OpenBSD
- OpenWrt

Please see our [Platforms](https://yggdrasil-network.github.io/platforms.html) pages for more
specific information about each of our supported platforms, including
installation steps and caveats.

You may also find other platform-specific wrappers, scripts or tools in the
`contrib` folder.

## Building

If you want to build from source, as opposed to installing one of the pre-built
packages:

1. Install [Go](https://golang.org) (requires Go 1.13 or later)
2. Clone this repository
2. Run `./build`

Note that you can cross-compile for other platforms and architectures by
specifying the `GOOS` and `GOARCH` environment variables, e.g. `GOOS=windows
./build` or `GOOS=linux GOARCH=mipsle ./build`.

## Running

### Generate configuration

To generate static configuration, either generate a HJSON file (human-friendly,
complete with comments):

```
./yggdrasil -genconf > /path/to/yggdrasil.conf
```

... or generate a plain JSON file (which is easy to manipulate
programmatically):

```
./yggdrasil -genconf -json > /path/to/yggdrasil.conf
```

You will need to edit the `yggdrasil.conf` file to add or remove peers, modify
other configuration such as listen addresses or multicast addresses, etc.

### Run Yggdrasil

To run with the generated static configuration:
```
./yggdrasil -useconffile /path/to/yggdrasil.conf
```

To run in auto-configuration mode (which will use sane defaults and random keys
at each startup, instead of using a static configuration file):

```
./yggdrasil -autoconf
```

You will likely need to run Yggdrasil as a privileged user or under `sudo`,
unless you have permission to create TUN/TAP adapters. On Linux this can be done
by giving the Yggdrasil binary the `CAP_NET_ADMIN` capability.

## Documentation

Documentation is available on our [GitHub
Pages](https://yggdrasil-network.github.io) site, or in the base submodule
repository within `doc/yggdrasil-network.github.io`.

- [Configuration file options](https://yggdrasil-network.github.io/configuration.html)
- [Platform-specific documentation](https://yggdrasil-network.github.io/platforms.html)
- [Frequently asked questions](https://yggdrasil-network.github.io/faq.html)
- [Admin API documentation](https://yggdrasil-network.github.io/admin.html)
- [Version changelog](CHANGELOG.md)

## Community

Feel free to join us on our [Matrix
channel](https://matrix.to/#/#yggdrasil:matrix.org) at `#yggdrasil:matrix.org`
or in the `#yggdrasil` IRC channel on Freenode.

## License

This code is released under the terms of the LGPLv3, but with an added exception
that was shamelessly taken from [godeb](https://github.com/niemeyer/godeb).
Under certain circumstances, this exception permits distribution of binaries
that are (statically or dynamically) linked with this code, without requiring
the distribution of Minimal Corresponding Source or Minimal Application Code.
For more details, see: [LICENSE](LICENSE).
