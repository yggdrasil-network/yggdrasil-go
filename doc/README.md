# Yggdrasil-go

## What is it?

This is a toy implementation of an encrypted IPv6 network.
A number of years ago, I started to spend some of my free time studying and routing schemes, and eventually decided that it made sense to come up with my own.
After much time spent reflecting on the problem, and a few failed starts, I eventually cobbled together one that seemed to have, more or less, the performance characteristics I was looking for.
I resolved to eventually write a proof-of-principle / test implementation, and I thought it would make sense to include many of the nice bells and whistles that I've grown accustomed to from using [cjdns](https://github.com/cjdelisle/cjdns), plus a few additional features that I wanted to test.
Fast forward through a couple years of procrastination, and I've finally started working on it in my limited spare time.
I've found that it's now marginally more interesting than embarrassing, so here it is.

The routing scheme was designed for scalable name-independent routing on graphs with an internet-like topology.
By internet-like, I mean that the network has a densely connected core with many triangles, a diameter that increases slowly with network size, and where any sparse edges tend to be relatively tree-like, all of which appear to be common features of large graphs describing "organically" grown relationships.
By scalable name-independent routing, I mean:

1. Scalable: resource consumption should grow slowly with the size of the network.
In particular, for internet-like networks, the goal is to use only a (poly)logarithmic amount of memory, use a logarithmic amount of bandwidth per one-hop neighbor for control traffic, and to maintain low average multiplicative path stretch (introducing overhead of perhaps a few percent) that does not become worse as the network grows.

2. Name-independent: a node's identifier should be independent of network topology and state, such that a node may freely change their identifier in a static network, or keep it static under state changes in a dynamic network.
In particular, addresses are self-assigned and derived from a public key, which circumvents the use of a centralized addressing authority or public key infrastructure.

Running this code will:

1. Set up a `tun` device and assign it a Unique Local Address (ULA) in `fd00::/8`.
2. Connect to other nodes running the software.
3. Route traffic for and through other nodes.

A device's ULA is actually from `fd00::/9`, and a matching `/64` prefix is available under `fd80::/9`. This allows the node to advertise a route on its LAN, as a workaround for unsupported devices.

## Building

1. Install Go (tested on 1.9, I use [godeb](https://github.com/niemeyer/godeb)).
2. Clone this repository.
2. `./build`

It's written in Go because I felt like learning a new language, and Go seemed like an easy language to learn while still being a reasonable choice for language to prototype network code.
Note that the build script defines its own `$GOPATH`, so the build and its dependencies should be self contained.
It only works on Linux at this time, because a little code (related to the `tun` device) is platform dependent, and changing that hasn't been a high priority.

## Running

To run the program, you'll need permission to create a `tun` device and configure it using `ip`.
If you don't want to mess with capabilities for the `tun` device, then using `sudo` should work, with the usual security caveats about running a program as root.

To run with default settings:

1. `./yggdrasil --autoconf`

That will generate a new set of keys (and an IP address) each time the program is run.
The program will bind to all addresses on a random port and listen for incoming connections.
It will send announcements over IPv6 link-local multicast, and attempt to start a connection if it hears an announcement from another device.

In practice, you probably want to run this instead:

1. `./yggdrasil --genconf > conf.json`
2. `./yggdrasil --useconf < conf.json`

The first step generates a configuration file with a set of cryptographic keys and default settings.
The second step runs the program using the configuration provided in that file.
Because ULAs are derived from keys, using a fixed set of keys causes a node to keep the same address each time the program is run.

If you want to use it as an overlay network on top of e.g. the internet, then you can do so by adding the address and port of the device you want to connect to (as a string, e.g. `"1.2.3.4:5678"`) to the list of `Peers` in the configuration file.
This should accept IPv4 and IPv6 addresses, and I think it should resolve host/domain names, but I haven't really tested that, so your mileage may vary.
You can also configure which address and/or port to listen on by editing the configuration file, in case you want to bind to a specific address or listen for incoming connections on a fixed port.

Also note that the nodes is connected to the network through a `tun` device, so it follows point-to-point semantics.
This means it's limited to routing traffic with source and destination addresses in `fd00::/8`--you can't add a prefix to your routing table "via" an address in that range, as the router has no idea who you meant to send it to.
In particular, this means you can't set a working default route that *directly* uses the overlay network, but I've had success *indirectly* using it to connect to an off-the-shelf VPN that I can use as a default route for internet access.

## Optional: advertise a prefix locally

Suppose a node has been given the address: `fd00:1111:2222:3333:4444:5555:6666:7777`

Then the node may also use addresses from the prefix: `fd80:1111:2222:3333::/64` (note the `fd00` -> `fd80`, a separate `/9` is used for prefixes).

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

Now any IPv6-enabled device in the LAN can use stateless address auto-configuration to assign itself a working `fd00::/8` address from the `/64` prefix, and communicate with the wider network through the router, without requiring any special configuration for each device.
I've used this to e.g. get my phone on the network.
Note that there are a some differences when accessing the network this way:

1. There are 64 fewer bits of address space available for self-certifying addresses.
This means that it is 64 bits easier to brute force a prefix collision than collision for a full node's IP address. As such, you may want to change addresses frequently, or else brute force an address with more security bits (see: `misc/genkeys.go`).

2. The LAN depends on the router for cryptography.
So while traffic going through the WAN is encrypted, the LAN is still just a LAN. You may want to secure your network.

3. Related to the above, the cryptography and I/O through the `tun` device both place additional load on the router, above what is normally present from forwarding packets between full nodes in the network, so the router may need more computing power to reach line rate.

## How does it work?

Consider the internet, which uses a network-of-networks model with address aggregation.
Addresses are allocated by a central authority, as blocks of contiguous addresses with a matching prefix.
Within a network, each node may represent one or more prefixes, with each prefix representing a network of one or more nodes.
On the largest scale, BGP is used to route traffic between networks (autonomous systems), and other protocols can be used to route within a network.
The effectiveness of such hierarchical addressing and routing strategies depend on network topology, with the internet's observed topology being the worst case of all known topologies from a scalability standpoint (see [arxiv:0708.2309](https://arxiv.org/abs/0708.2309) for a better explanation of the issue, but the problem is essentially that address aggregation is ineffective in a network with a large number of nodes and a small diameter).

The routing scheme implemented by this code tries a different approach.
Instead of using assigned addresses and a routing table based on prefixes and address aggregation, routing and addressing are handled through a combination of:

1. Self-assigned cryptographically generated addresses, to handle address allocation without a central authority.
2. A kademlia-like distributed hash table, to look up a node's (name-dependent) routing information from their (name-independent routing) IP address.
3. A name-dependent routing scheme based on greedy routing in a metric space, constructed from an arbitrarily rooted spanning tree, which gives a reasonable approximation of the true distance between nodes for certain network topologies (namely the scale-free topology that seems to emerge in many large graphs, including the internet). The spanning tree embedding takes stability into account when selecting which one-hop neighbor to use as a parent, and path selection uses (poorly) estimated available bandwidth as a criteria, subject to the constraint that metric space distances must decrease with each hop. Incidentally, the name `yggdrasil` was selected for this test code because that's obviously what you call an immense tree that connects worlds.

The network then presents itself as having a single "flat" address with no aggregation.
Under the hood, it runs as an overlay on top of existing IP networks.
Link-local IPv6 multicast traffic is used to advertise on the underlying networks, which can as easily be a wired or wireless LAN, a direct (e.g. ethernet) connection between two devices, a wireless ad-hoc network, etc.
Additional connections can be added manually to peer over networks where link-local multicast is insufficient, which allows you to e.g. use the internet to bridge local networks.

The name-dependent routing layer uses cryptographically signed (`Ed25519`) path-vector-like routing messages, similar to S-BGP, which should prevent route poisoning and related attacks.
For encryption, it uses the Go implementation of the `nacl/box` scheme, which is built from a Curve25519 key exchange with XSalsa20 as a stream cypher and Poly1305 for integrity and authentication.
Permanent keys are used for protocol traffic, including the ephemeral key exchange, and a hash of a node's permanent public key is used to construct a node's address.
Ephemeral keys are used for encapsulated IP(v6) traffic, which provides forward secrecy.
Go's `crypto/rand` library is used for nonce generation.
In short, I've tried to not make this a complete security disaster, but the code hasn't been independently audited and I'm nothing close to a security expert, so it should be considered a proof-of-principle rather than a safe implementation.
At a minimum, I know of no way to prevent gray hole attacks.

I realize that this is a terribly short description of how it works, so I may elaborate further in another document if the need arises.
Otherwise, I guess you could try to read my terrible and poorly documented code if you want to know more.

## Related work

A lot of inspiration comes from [cjdns](https://github.com/cjdelisle/cjdns).
I'm a contributor to that project, and I wanted to test out some ideas that weren't convenient to prototype in the existing code base, which is why I wrote this toy.

On the routing side, a lot of influence came from compact routing.
A number of compact routing schemes are evaluated in [arxiv:0708.2309](https://arxiv.org/abs/0708.2309) and may be used as a basis for comparison.
When tested in a simplified simulation environment on CAIDA's 9204-node "skitter" network graph used in that paper, I observed an average multiplicative stretch of about 1.08 with my routing scheme, as implemented here.
This can be lowered to less than 1.02 using a source-routed version of the algorithm and including node degree as an additional parameter of the embedding, which is of academic interest, but degree's unverifiability makes it impractical for this implementation.
In either case, this only requires 1 routing table entry per one-hop neighbor (this averages ~6 for in the skitter network graph), plus a logarithmic number of DHT entries (expected to be ~26, based on extrapolations from networks with a few hundred nodes--running the full implementation on the skitter graph is impractical on my machine).
I don't think stretch is really an appropriate metric, as it doesn't consider the difference to total network cost from a high-stretch short path vs a high-stretch long path.
In this scheme, and I believe in most compact routing schemes, longer paths tend to have lower multiplicative stretch, and shorter paths are more likely to have longer stretch.
I would argue that this is preferable to the alternative.

While I use a slightly different approach, the idea to try a greedy routing scheme was inspired by the use of greedy routing on networks embedded in the hyperbolic plane (such as [Kleinberg's work](https://doi.org/10.1109%2FINFCOM.2007.221) and [Greedy Forwarding on the NDN Testbed](https://www.caida.org/research/routing/greedy_forwarding_ndn/)).
I use distance on a spanning tree as the metric, as seems to work well on the types of networks I'm concerned with, and it simplifies other aspects of the implementation.
The hyperbolic embedding algorithms I'm aware of, or specifically the distributed ones, operate by constructing a spanning tree of the network and then embedding the tree.
So I don't see much harm, at present, of skipping the hyperbolic plane and directly using the tree for the metric space.

## Misc. notes

This is a toy experiment / proof-of-concept.
It's only meant to test if / how well some ideas work.
I have no idea what I'm doing, so for all I know it's entirely possible that it could crash your computer, eat your homework, or set fire to your house.
Some parts are also written to be as bad as I could make them while still being technically correct, in an effort to make bugs obvious if they occur, which means that even when it does work it may be fragile and error prone.

In particular, you should expect it to perform poorly under mobility events, and to converge slowly in dynamic networks. All else being equal, this implementation should tend to prefer long-lived links over short-lived ones when embedding, and (poorly estimated) high bandwidth links over low bandwidth ones when forwarding traffic. As such, in multi-homed or mobile scenarios, there may be some tendency for it to make decisions you disagree with.

While stretch is low on internet-like graphs, the best upper bound I've established on the *additive* stretch of this scheme, after convergence, is the same as for tree routing: proportional to network diameter. For sparse graphs with a large diameter, the scheme may not find particularly efficient paths, even under ideal circumstances. I would argue that such networks tend not to grow large enough for scalability to be an issue, so another routing scheme is better suited to those networks.

Regarding the announce-able prefix thing, what I wanted to do is use `fc00::/7`, where `fc00::/8` is for nodes and `fd00::/8` is for prefixes.
I would also possibly widen the prefixes to `/48`, to match [rfc4193](https://tools.ietf.org/html/rfc4193), and possibly provide an option to keep using a `/64` by splitting it into two `/9` blocks (where `/64` prefixes would continue to live in `fd80::/9`), or else convince myself that the security implications of another 16 bits don't matter (to avoid the complexity of splitting it into two `/9` ranges for prefixes).
Using `fc00::/8` this way would cause issues if trying to also run cjdns.
Since I like cjdns, and want the option of running it on the same nodes, I've decided not to do that.
If I ever give up on avoiding cjdns conflicts, then I may change the addressing scheme to match the above.

Despite the tree being constructed from path-vector-like routing messages, there's no support for routing policy right now.
As a result, peer relationships are bimodal: either you're not connected to someone, or you're connected and you'll route traffic *to* and *through* them.
Nodes also accept all incoming connections, so if you want to limit who can connect then you'll need to provide some other kind of access controls.

The current implementation does all of its setup when the program starts, and then nothing can be reconfigured without restarting the program.
At some point I may add a remote API, so a running node can be reconfigured (to e.g. add/remove peers) without restarting, or probe the internal state of the router to get useful debugging info.
So far, things seem to work the way I want/expect without much trouble, so I haven't felt the need to do this yet.

Some parts of the implementation can take advantage of multiple cores, but other parts that could simply do not.
Some parts are fast, but other parts are slower than they have any right to be, e.g. I can't figure out why some syscalls are as expensive as they are, so the `tun` in particular tends to be a CPU bottleneck (multi-queue could help in some cases, but that just spreads the cost around, and it doesn't help with single streams of traffic).
The Go runtime's GC tends to have short pauses, but it does have pauses.
So even if the ideas that went into this routing scheme turn out to be useful, this implementation is likely to remain mediocre at best for the foreseeable future.
If the is thing works well and the protocol stabilizes, then it's worth considering re-implementation and/or a formal spec and RFC.
In such a case, it's entirely reasonable to change parts of the spec purely to make the efficient implementation easier (e.g. it makes sense to want zero-copy networking, but a couple parts of the current protocol might make that impractical).

