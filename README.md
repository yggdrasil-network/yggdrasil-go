# Yggdrasil

## What is it?

This is a toy implementation of an encrypted IPv6 network, with many good ideas stolen from [cjdns](https://github.com/cjdelisle/cjdns), which was written to test a particular routing scheme that I cobbled together one random Wednesday afternoon.
It's notably not a shortest path routing scheme, with the goal of scalable name-independent routing on dynamic networks with an internet-like topology.
It's named Yggdrasil after the world tree from Norse mythology, because that seemed like the obvious name given how it works.
For a longer, rambling version of this readme with more information, see: [doc](doc/README.md).
A very early incomplete draft of a [whitepaper](doc/Whitepaper.md) describing the protocol is also available.

This is a toy / proof-of-principle, so it's not even alpha quality software--any nontrivial update is likely to break backwards compatibility with no possibility for a clean upgrade path.
You're encouraged to play with it, but I strongly advise against using it for anything mission critical.

## Building

1. Install Go (tested on 1.9, I use [godeb](https://github.com/niemeyer/godeb)).
2. Clone this repository.
2. `./build`

The build script sets its own `$GOPATH`, so the build environment is self-contained.
This code should work on Linux and macOS, but see the optional example below for a way to share connectivity with the rest of a network.

## Running

To run the program, you'll need permission to create a `tun` device and configure it using `ip`.
If you don't want to mess with capabilities for the `tun` device, then using `sudo` should work, with the usual security caveats about running a program as root.

To run with default settings:

1. `./yggdrasil --autoconf`

That will generate a new set of keys (and an IP address) each time the program is run.
The program will bind to all addresses on a random port and listen for incoming connections.
It will send announcements over IPv6 link-local multicast, and it will attempt to start a connection if it hears an announcement from another device.

In practice, you probably want to run this instead:

1. `./yggdrasil --genconf > conf.json`
2. `./yggdrasil --useconf < conf.json`

This keeps a persistent set of keys (and by extension, IP address) and gives you the option of editing the configuration file.
If you want to use it as an overlay network on top of e.g. the internet, then you can do so by adding the remote devices domain/address and port (as a string, e.g. `"1.2.3.4:5678"`) to the list of `Peers` in the configuration file.

A [Vyatta-based](https://github.com/neilalexander/vyatta-yggdrasil) router configuration is also available.

## Optional: advertise a prefix locally

Suppose a node has generated the address: `fd00:1111:2222:3333:4444:5555:6666:7777`

Then the node may also use addresses from the prefix: `fd80:1111:2222:3333::/64` (note the `fd00` changed to `fd80`, a separate `/9` is used for prefixes, but the rest of the first 64 bits are the same).

To advertise this prefix and a route to `fd00::/8`, the following seems to work for me:

1. Enable IPv6 forwarding (e.g. `sysctl -w net.ipv6.conf.all.forwarding=1` or add it to sysctl.conf).

2. `ip addr add fd80:1111:2222:3333::1/64 dev eth0` or similar, to assign an address for the router to use in that prefix, where the LAN is reachable through `eth0`.

3. Install/run `radvd` with something like the following in `/etc/radvd.conf`:
```
interface eth0
{
        AdvSendAdvert on;
        prefix fd80:1111:2222:3333::/64 {
            AdvOnLink on;
            AdvAutonomous on;
        };
        route fd00::/8 {};
};
```

This is enough to give unsupported devices on my LAN access to the network, with a few security and performance cautions outlined in the [doc](doc/README.md) file.

## How does it work?

I'd rather not try to explain in the readme, but I describe it further in the [doc](doc/README.md) file or the very draft of a [whitepaper](doc/Whitepaper.md), so you can check there if you're interested.
Be warned that it's still not a very good explanation, but it at least gives a high-level overview and links to some relevant work by other people.

## Obligatory performance propaganda

A [simplified model](misc/sim/treesim-forward.py) of this routing scheme has been tested in simulation on the 9204-node [skitter](https://www.caida.org/tools/measurement/skitter/) network topology dataset from [caida](https://www.caida.org/), and compared with results in [arxiv:0708.2309](https://arxiv.org/abs/0708.2309).
Using the routing scheme as implemented in this code, I observe an average multiplicative stretch of 1.08, with an average routing table size of 6 for a name-dependent scheme, and approximately 30 additional (but smaller) entries needed for the name-independent routing table.
The number of name-dependent routing table entries needed is proportional to node degree, so that 6 is the mean of a distribution with a long tail, but I believe this is an acceptable tradeoff.
The size of name-dependent routing table entries is relatively large, due to cryptographic signatures associated with routing table updates, but in the absence of cryptographic overhead I believe each entry is otherwise comparable to the BC routing scheme described in the above paper.
A modified version of this scheme, with the same resource requirements, achieves a multiplicative stretch of 1.02, which drops to 1.01 if source routing is used.
Both of these optimizations are not present in the current implementation, as the former depends on network state information that I haven't found a way to cryptographically secure, and the latter optimization is both tedious to implement and would make debugging other aspects of the implementation more difficult.

## License

This code is released under the terms of the LGPLv3, but with an added exception that was shamelessly taken from [godeb](https://github.com/niemeyer/godeb).
Under certain circumstances, this exception permits distribution of binaries that are (statically or dynamically) linked with this code, without requiring the distribution of Minimal Corresponding Source or Minimal Application Code.
For more details, see: [LICENSE](LICENSE).

