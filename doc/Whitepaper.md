# Yggdasil

Note: This is a very rough early draft.

Yggdrasil is a routing protocol designed for scalable name-independent routing on internet-like graphs.
The design is built around a name-dependent routing scheme which uses distance on a spanning tree as a metric for greedy routing, and a kademlia-like distributed hash table to facilitate lookups of metric space routing information from static cryptographically generated identifiers.
This approach can find routes on any network, as it reduces to spanning tree routing in the worst case, but is observed to be particularly efficient on internet-like graphs.
In an effort to mitigate many forms of attacks, the routing scheme only uses information which is either cryptographically verifiable or based on observed local network state.
The implementation is distributed and runs on dynamic graphs, though this implementation may not converge quickly enough to be practical on networks with high node mobility.
This document attempts to give a rough overview of how some of the key parts of the protocol are implemented, as well as an explanation of why a few subtle points are handled the way they are.

## Addressing

Addresses in Yggdrasil are derived from a truncated version of a `NodeID`.
The `NodeID` itself is a sha512sum of a node's permanent public Curve25519 key.
Each node's IPv6 address is then assigned from the lower half of the `fd00::/8` prefix using the following approach:

1. Begin with `0xfd` as the first byte of the address.
2. Count the number of leading `1` bits in the NodeID.
3. Set the second byte of the address to the number of leading `1` bits, subject to the constraint that this is still in the lower half of the address range (it is unlikely that a node will have 128 or more leading `1` bits in a sha512sum hash, for the foreseeable future).
4. Append the NodeID to the remaining bits of the address, truncating the leading `1` bits and the first `0` bit, to a total address size of 128 bits.

When connecting to a node, the IP address is unpacked into the known bits of the NodeID and a matching bitmask to track which bits are significant.
A node is only communicated with if its `NodeID` matches its public key and the known `NodeID` bits from the address.

It is important to note that only `NodeID` is used internally for routing, so the addressing scheme could in theory be changed without breaking compatibility with intermediate routers.
This may become useful if the IPv6 address range ever needs to be changed, or if a new addressing format that allows for more significant bits is ever implemented by the OS.

### Cryptography

Public key encryption is done using the `golang.org/x/crypto/nacl/box`, which uses Curve25519, XSalsa20, and Poly1305 for key exchange, encryption, and authentication.
Permanent keys are used only for protocol traffic, with random nonces generated on a per-packet basis using `crypto/rand` from Go's standard library.
Ephemeral session keys are generated for encapsulated IPv6 traffic, using the same set of primitives, with random initial nonces that are subsequently incremented.
A list of recently received session nonces is kept (as a bitmask) and checked to reject duplicated packets, in an effort to block duplicate packets and replay attacks.

A separate private key is generated and used for signing with Ed25519, which is used by the name-dependent routing layer to secure construction of the spanning tree, with a TreeID hash of a node's public Ed key being used to select the highest TreeID as the root of the tree.

### Prefixes

Recall that each node's address is in the lower half of the address range, I.e. `fd00::/9`. A `/64` prefix is made available to each node under `fd80::/9`, where the remaining bits of the prefix match the node's address under `fd00::/9`.
A node may optionally advertise a prefix on their local area network, which allows unsupported or legacy devices with IPv6 support to connect to the network.
Note that there are 64 fewer bits of `NodeID` available to check in each address from a routing prefix, so it makes sense to brute force a `NodeID` with more significant bits in the address if this approach is to be used.
Running `genkeys.go` will do this by default.

## Name-independent routing

A distributed hash table is used to facilitate the lookup of a node's name-dependent routing `coords` from a `NodeID`.
A kademlia-like peer structure and xor metric are used in the DHT layout, but only peering info is used--there is no key:value store.
In contrast with standard kademlia, instead of using iterative parallel lookups, a recursive lookup strategy is used.
This is an intentional design decision to make the DHT more fragile--the intent is for DHT inconsistencies to lead to lookup failures, because of concerns that the iterative parallel approach may hide DHT bugs.

In particular, the DHT is bootstrapped off of a node's one-hop neighbors, and I've observed that this causes a standard kademlia implementation to diverge in the general case.
To get around this, buckets are updated more aggressively, and the least recently pinged node from each bucket is flushed to make room for new nodes as soon as a response is heard from them.
This appears to fix the bootstrapping issues on all networks where they had been observed in testing, but recursive lookups are kept for the time being to continue monitoring the issue.
However, recursive lookups require fewer round trips, so they are expected to be lower latency.
As such, even if a switch to iterative parallel lookups was made, the recursive lookup functionality may be kept and used optimistically to minimize handshake time in stable networks.

Other than these differences, the DHT is more-or-less what you might expect from a kad implementation.

## Name-dependent routing

A spanning tree is constructed and used for name-dependent routing.
The basic idea is to use the distance between nodes *on the tree* as a distance metric, and then perform greedy routing in that metric space.
As the tree is constructed from a subset of the real links in the network, this distance metric (unlike the DHT's xor metric) has some relationship with the underlying physical network.
In the worst case, greedy routing with this metric reduces to routing on the spanning tree, which should be comparable to ethernet.
However, greedy routing can use any link, provided that the node on the other end of the link is closer to the destination, so this allows the use of off-tree shortcuts, with the possibility and effectiveness of this being topology dependent.
The main assumption that Yggdrasil's performance hinges on, is that this distance metric is close to real network distance, on average, in realistic networks.

The name dependent scheme is implemented in roughly the following way:

1. Each node generates a set of Ed25519 keys for signing routing messages, with a `TreeID` defined as the sha512sum of a node's public signing key.
2. If a node doesn't know a better (higher `TreeID`) root for the tree, then it makes itself the root of its own tree.
3. Nodes periodically send announcement messages to neighbors, which specify a sequence number for that node's current locator in the tree.
4. When a node A sees an unrecognized sequence number from a neighbor B, then A asks B to send them a locator.
5. This locator is sent in the form of a path from the root, through B, and ending at A.
6. Each hop in the path includes the public signing key of the next hop, and a signature for the full path from the root to the next hop, to prevent forgery of path information (similar to S-BGP).
7. The first hop, from the root, includes a signed sequence number which must increase (implemented as a unix timestamp, for convenience), which is used to detect root timeouts and prevent replays.

The highest `TreeID` approach to root selection is just to ensure that nodes select the same root, otherwise distance calculations wouldn't work.
Root selection has a minor effect on the stretch of the paths selected by the network, but this effect was seen to be small compared to the minimum stretch, for nearly all choices of root.

The current implementation tracks how long a neighbor has been advertising a locator for the same path, and it prefers to select a parent with a stable locator and a short distance to the root (maximize uptime/distance).
When forwarding traffic, the next hop is selected taking bandwidth to the next hop and distance to the destination into account (maximize bandwidth/distance), subject to the requirement that distance must always decrease.
The bandwidth estimation isn't very good, but it correlates well enough that e.g. when a slow wifi and a fast ethernet link to the same node are available, it typically uses the ethernet link.
However, if the ethernet link comes up while the wifi link is under heavy use, then it tends to keep using the wifi link until things settle down, and only switches to ethernet after the wireless link is no longer overloaded.
A better approach to bandwidth estimation could probably switch to the new link faster.

Note that this forwarding procedure generalizes to nodes that are not one-hop neighbors, but the current implementation omits the use of more distant neighbors, as this is expected to be a minor optimization (it would add per-link control traffic to pass path-vector-like information about a subset of the network, which is a lot of overhead compared to the current setup).

## Other implementation details

In case you hadn't noticed, this implementation is written in Go.
That decision was made because the designer and initial author (@Arceliar) felt like learning a new language when the implementation was started, and the Go language seemed like an OK choice for prototyping a network application.
While Go's GC pauses are small, they do exist, so this implementation probably isn't suited to applications that require very low latency and jitter.

Aside from that, an effort was made to write each part of it to be as "bad" (i.e. fragile) as could be managed while still being technically correct.
That's a decision made for debugging purposes: the intent is to make any bugs as obvious as possible, so they can more easily be found and fixed in a small or simulated network.

This implementation runs as an overlay network on top of regular IPv4 or IPv6 traffic.
It uses link-local IPv6 multicast traffic to automatically connect to devices on the same network, but it can also be fed a list of address:port pairs to connect to.
This can be used to e.g. set up two local networks and bridge them over the internet.

## Performance

This section compares Yggdrasil with the results in [arxiv:0708.2309](https://arxiv.org/abs/0708.2309) (specifically table 1) from tests on the 9204-node [skitter](https://www.caida.org/tools/measurement/skitter/) network maps from [caida](https://www.caida.org/).

A [simplified version](misc/sim/treesim-forward.py) of this routing scheme was written (long before the Yggdrasil implementation was started), and tested for comparison with the results from the above paper.
This version includes only the name-dependent part of the routing scheme, but the overhead of the name-independent portion is easy enough to check with the full implementation.
In summary:

1. Multiplicative stretch is approximately 1.08 with Yggdrasil, using unweighted links undirected links, as in the paper.
2. A modified version can get this as low as 1.01, but it depends on knowing the degree of each one-hop neighbor, which it is not obviously possible to cryptographically secure, and it requires using source routing to find a path from A to B and from B to A, and then have both nodes use whichever path was observed to be shorter.
3. In either case, approximately 6 routing table entries are needed, on average, for the name-dependent routing scheme, where each node needs one routing table entry per one-hop neighbor.
4. Approximately 30 DHT entries are needed to facilitate name-independent routing.
This requires a lookup and caches the results, so old information needs to time out to work on dynamic networks.
The schemes it's being compared to only work on static networks, where a similar approach would be fine, so this seems like a reasonably fair comparison.
The stretch of that initial lookup can be *very* high, but it's only for a couple of round trips to look up keys and then do the ephemeral key exchange, so this may be an acceptable tradeoff (it's probably more expensive than a DNS lookup, but is similar in principle and effect).
5. Both the name-dependent and name-independent routing table entries are of a size proportional to the length of the path between the root and the node, which is at most the diameter of the network after things have fully converged, but name-dependent routing table entries tend to be much larger in practice due to the size of cryptographic signatures (64 bytes for a signature + 32 for the signing key).
6. The name-dependent routing scheme only sends messages about one-hop neighbors on the link between those neighbors, so if you measure things by per *link* overhead instead of per *node*, then this doesn't seem so bad to me.
7. The name-independent routing scheme scales like a DHT running as an overlay on top of the router-level topology, so the per-link and per-node overhead are going to be topology dependent.
This hasn't been studied in a lot of detail, but for realistic topologies, where yggdrasil routing seems to approximate shortest path routing, academic research has shown that shortest path routing does not lead to congestion.

The designer (@Arceliar) believes that the main reason Yggdrasil performs so well is because it stores information about all one-hop neighbors.
Consider that, if Yggdrasil did not maintain state about all one-hop neighbors, but the protocol still had the ability to forward to all of them through some mechanism (i.e. source routing), then the OS still needs a way to forward traffic to them.
In most cases, this would require some form of per-neighbor state to be stored by the OS, either because there's one dedicated interface per peer or because there are entries in an arp/NDP table to reach multiple devices over a shared switch.
So while compact routing schemes have nice theoretical limits, which do not require even as much state as one entry per one-hop neighbor, that property does not seem realistic if the implementation is running at the router level (as opposed to the AS level).
As such, keeping one entry per neighbor may be reasonable, especially if nodes with a high degree have proportionally more resources available to them, but it is possible that something may have been overlooked in the design.

## Disclaimer

This is a draft version of documentation for a work-in-progress protocol.
The design and implementation should be considered pre-alpha, with any and all aspects subject to change in light of ongoing R&D.
It is possible that this document and the code base may fall out of sync with eachother.
Some details that are known to be likely to change, packet formats in particular, have been omitted.
