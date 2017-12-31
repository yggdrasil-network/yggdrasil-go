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
This is an intentional design decision to make the DHT more fragile--I explicitly want DHT inconsistencies to lead to lookup failures, because of concerns that the iterative parallel approach may hide DHT bugs.

In particular, the DHT is bootstrapped off of a node's one-hop neighbors, and I've observed that this causes a standard kademlia implementation to diverge in the general case.
To get around this, buckets are updated more aggressively, and the least recently pinged node from each bucket is flushed to make room for new nodes as soon as a response is heard from them.
This appears to fix the bootstrapping issues on all networks where I have previously found them, but recursive lookups are kept for the time being to continue monitoring the issue.
However, recursive lookups require fewer round trips, so they are expected to be lower latency.
As such, even if a switch to iterative parallel lookups was made, the recursive lookup functionality may be kept and used optimistically to minimize handshake time in stable networks.

Other than these differences, the DHT is more-or-less what you might expect from a kad implementation.

## Name-dependent routing

A spanning tree is constructed and used for name-dependent routing.
Each node generates a set of Ed25519 keys for signing routing messages, with a `TreeID` defined as the sha512sum of a node's public signing key.

The nodes use what is essentially a version of the spanning tree protocol, but with each node advertising the full path from the root of the tree to the neighbor that the node is sending an advertisement to.
These messages are accompanied by a set of signatures, each of which includes the intended next-hop information as part of the data being signed, which are checked for validity and consistency before an advertisement is accepted.
This prevents the forgery of arbitrary routing information, along with the various bugs and route poisoning attacks that such incorrect information can lead it.
The root of the spanning tree is selected as the node with the highest `TreeID`, subject to the constrain that the node is regularly triggering updates by increasing a sequence number (taken from a unix timestamp for convenience), signing them, and sending them to each of their one-hop neighbors.
If a node fails to receive an updated route with a new timestamp from their current root, then after some period of time, the node considers the root to have timed out.
The node is blacklisted as a root until it begins sending newer timestamps, and the next highest `TreeID` that the node becomes aware of is used instead.
Through this protocol, each node of the network eventually selects the same node as root, and thereby joins the same spanning tree of the network.
The choice of which node is root appears to have only a minor effect on the performance of the routing scheme, with the only strict requirement being that all nodes select the same root, so using highest `TreeID` seems like an acceptable approach.
I dislike that it adds some semblance of a proof-of-work arms-race to become root, but as there is neither significant benefit nor obvious potential for abuse from being the root, I'm not overly concerned with it at this time.

When selecting which node to use as a parent in the tree, the only strict requirement, other than the choice of root, is that path from the root to the node does not loop.
The current implementation favors long lived short paths to the root, at equal weight.
I.e. it takes the time that the same path has been advertised and divides by the number of hops to reach the root, and selects the one-hop neighbor for which this value is largest, conditional on the path not looping.

When forwarding traffic, the only strict requirement is that the distance to the destination, as estimated by the distance on the tree between the current location and the destination, must always decrease.
This is implemented by having each sender set the value of a TTL field to match the node's distance from the destination listed on a packet, and having each receiver check that they are in fact closer to the destination than this TTL (before changing it to their own distance and sending it to the next hop).
This ensures loop-free routing.
The current implementation prioritizes a combination of estimated bandwidth to a one-hop neighbor, and the estimated total length of the path from the current node, to that one-hop neighbor (1), and then to the destination (tree distance from the neighbor to the dest).
Specifically, the current implementation forwards to the one-hop neighbor for which estimated bandwidth divided by expected path length is maximized, subject to the TTL constraints.
The current implementation's bandwidth estimates are terrible, but there seems to be enough correlation to actual bandwidth that e.g. it eventually prefers to use a fast wired link over a slow wifi link if there's multiple connections to a neighbor in a multi-home setup.

Note that this forwarding procedure generalizes to nodes that are not one-hop neighbors, but the current implementation omits the use of more distant neighbors, as this is expected to be a minor optimization (and may not even be worth the increased overhead).

## Other implementation details

In case you hadn't noticed, this implementation is written in Go.
That decision was made because I felt like learning Go, and the language seemed like an OK choice for prototyping a network application.
While Go's GC pauses are small, they do exist, so this implementation probably isn't suited to applications that require very low latency and jitter.

Aside from that, I also tried to write each part of it to be as "bad" (i.e. fragile) as I could manage while still being technically correct.
That's a decision made for debugging purposes: I want my bugs to be obvious, so I can more easily find them and fix them.

This implementation runs as an overlay network on top of regular IPv4 or IPv6 traffic.
It tries to auto-detect and peer with one-hop neighbors using link-local IPv6 multicast, but it can also be fed a list of address:port pairs to connect to.
This allows you to connect to devices that aren't on the same network, if you know their address an have a working route to them.

## Performance

This seconds compares Yggdrasil with the results in [arxiv:0708.2309](https://arxiv.org/abs/0708.2309) (specifically table 1) from tests on the 9204-node [skitter](https://www.caida.org/tools/measurement/skitter/) network maps from [caida](https://www.caida.org/).

A [simplified version](misc/sim/treesim-forward.py) of this routing scheme was written (long before the Yggdrasil implementation was started), and tested for comparison with the results from the above paper.
This version includes only the name-dependent part of the routing scheme, but the overhead of the name-independent portion is easy enough to check with the full implementation.
In summary:

1. Multiplicative stretch is approximately 1.08 with Yggdrasil, using unweighted links undirected links, as in the paper.
2. A modified version can get this as low as 1.01, but it depends on knowing the degree of each one-hop neighbor, which I can think of no way to cryptographically secure, and it requires using source routing to find a path from A to B and from B to A, and then have both nodes use whichever path was observed to be shorter.
3. In either case, approximately 6 routing table entries are needed, on average, for the name-dependent routing scheme, where each node needs one routing table entry per one-hop neighbor.
4. Approximately 30 DHT entries are needed to facilitate name-independent routing.
5. Both the name-dependent and name-independent routing table entries are of a size proportional to the length of the path between the root and the node, which is at most the diameter of the network after things have fully converged, but name-dependent routing table entries tend to be much larger in practice due to the size of cryptographic signatures (64 bytes for a signature + 32 for the signing key).
6. The name-dependent routing scheme only sends messages about one-hop neighbors on the link between those neighbors, so if you measure things by per *link* overhead instead of per *node*, then this doesn't seem so bad to me.
7. The name-independent routing scheme scales like a DHT running as an overlay on top of the router-level topology, so the per-link and per-node overhead are going to be topology dependent.
I haven't studied them in a lot of detail, but for realistic topologies, I don't see an obvious reason to think this is a problem.

I think the main reason Yggdrasil performs so well is because it stores information about all one-hop neighbors.
Consider that, if Yggdrasil did not maintain state about all one-hop neighbors, but the protocol still had the ability to forward to all of them, then I OS still needs a way to forward traffic to them.
In most cases, I think this would require some form of per-neighbor state to be stored by the OS, either because there's one dedicated interface per peer or because there are entries in an arp/NDP table to reach multiple devices over a shared switch.
So while compact routing schemes have nice theoretical limits that don't even require one entry per one-hop neighbor, I don't think current real machines can benefit from that property if the routing scheme is used at the router level.
As such, I don't think keeping one entry per neighbor is a problem, especially if nodes with a high degree have proportionally more resources available to them, but I could be overlooking something.

## Disclaimer

This is a draft version of documentation for a work-in-progress protocol.
The design and implementation should be considered pre-alpha, with any and all aspects subject to change in light of ongoing R&D.
It is possible that this document and the code base may fall out of sync with eachother.
Some details that are known to be likely to change, packet formats in particular, have been omitted.
