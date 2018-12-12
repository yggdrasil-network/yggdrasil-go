# Yggdasil

Note: This is a very rough early draft.

Yggdrasil is an encrypted IPv6 network running in the [`200::/7` address range](https://en.wikipedia.org/wiki/Unique_local_address).
It is an experimental/toy network, so failure is acceptable, as long as it's instructive to see how it breaks if/when everything falls apart.

IP addresses are derived from cryptographic keys, to reduce the need for public key infrastructure.
A form of locator/identifier separation (similar in goal to [LISP](https://en.wikipedia.org/wiki/Locator/Identifier_Separation_Protocol)) is used to map static identifiers (IP addresses) onto dynamic routing information (locators), using a [distributed hash table](https://en.wikipedia.org/wiki/Distributed_hash_table) (DHT).
Locators are used to approximate the distance between nodes in the network, where the approximate distance is the length of a real worst-case-scenario path through the network.
This is (arguably) easier to secure and requires less information about the network than commonly used routing schemes.

While not technically a [compact routing scheme](https://arxiv.org/abs/0708.2309), tests on real-world networks suggest that routing in this style incurs stretch comparable to the name-dependent compact routing schemes designed for static networks.
Compared to compact routing schemes, Yggdrasil appears to have smaller average routing table sizes, works on dynamic networks, and is name-independent.
It currently lacks the provable bounds of compact routing schemes, and there's a serious argument to be made that it cheats by stretching the definition of some of the above terms, but the main point to be emphasized is that there are trade-offs between different concerns when trying to route traffic, and we'd rather make every part *good* than try to make any one part *perfect*.
In that sense, Yggdrasil seems to be competitive, on what are supposedly realistic networks, with compact routing schemes.

## Addressing

Yggdrasil uses a truncated version of a `NodeID` to assign addresses.
An address is assigned from the `200::/7` prefix, according to the following:

1. Begin with `0x02` as the first byte of the address, or `0x03` if it's a `/64` prefix.
2. Count the number of leading `1` bits in the NodeID.
3. Set the second byte of the address to the number of leading `1` bits in the NodeID (8 bit unsigned integer, at most 255).
4. Append the NodeID to the remaining bits of the address, truncating the leading `1` bits and the first `0` bit, to a total address size of 128 bits.

The last bit of the first byte is used to flag if an address is for a router (`200::/8`), or part of an advertised prefix (`300::/8`), where each router owns a `/64` that matches their address (except with the eight bit set to 1 instead of 0).
This allows the prefix to be advertised to the router's LAN, so unsupported devices can still connect to the network (e.g. network printers).

The NodeID is a [sha512sum](https://en.wikipedia.org/wiki/SHA-512) of a node's public encryption key.
Addresses are checked that they match NodeID, to prevent address spoofing.
As such, while a 128 bit IPv6 address is likely too short to be considered secure by cryptographer standards, there is a significant cost in attempting to cause an address collision.
Addresses can be made more secure by brute force generating a large number of leading `1` bits in the NodeID.

When connecting to a node, the IP address is unpacked into the known bits of the NodeID and a matching bitmask to track which bits are significant.
A node is only communicated with if its `NodeID` matches its public key and the known `NodeID` bits from the address.

It is important to note that only `NodeID` is used internally for routing, so the addressing scheme could in theory be changed without breaking compatibility with intermediate routers.
This has been done once, when moving the address range from the `fd00::/8` ULA range to the reserved-but-[deprecated](https://tools.ietf.org/html/rfc4048) `200::/7` range.
Further addressing scheme changes could occur if, for example, an IPv7 format ever emerges.

### Cryptography

Public key encryption is done using the `golang.org/x/crypto/nacl/box`, which uses [Curve25519](https://en.wikipedia.org/wiki/Curve25519), [XSalsa20](https://en.wikipedia.org/wiki/Salsa20), and [Poly1305](https://en.wikipedia.org/wiki/Poly1305) for key exchange, encryption, and authentication (interoperable with [NaCl](https://en.wikipedia.org/wiki/NaCl_(software))).
Permanent keys are used only for protocol traffic, with random nonces generated on a per-packet basis using `crypto/rand` from Go's standard library.
Ephemeral session keys (for [forward secrecy](https://en.wikipedia.org/wiki/Forward_secrecy)) are generated for encapsulated IPv6 traffic, using the same set of primitives, with random initial nonces that are subsequently incremented.
A list of recently received session nonces is kept (as a bitmask) and checked to reject duplicated packets, in an effort to block duplicate packets and replay attacks.
A separate set of keys are generated and used for signing with [Ed25519](https://en.wikipedia.org/wiki/Ed25519), which is used by the routing layer to secure construction of a spanning tree.

### Prefixes

Recall that each node's address is in the lower half of the address range, I.e. `200::/8`. A `/64` prefix is made available to each node under `300::/8`, where the remaining bits of the prefix match the node's address under `200::/8`.
A node may optionally advertise a prefix on their local area network, which allows unsupported or legacy devices with IPv6 support to connect to the network.
Note that there are 64 fewer bits of `NodeID` available to check in each address from a routing prefix, so it makes sense to brute force a `NodeID` with more significant bits in the address if this approach is to be used.
Running `genkeys.go` will do this by default.

## Locators and Routing

Locators are generated using information from a spanning tree (described below).
The result is that each node has a set of [coordinates in a greedy metric space](https://en.wikipedia.org/wiki/Greedy_embedding).
These coordinates are used as a distance label.
Given the coordinates of any two nodes, it is possible to calculate the length of some real path through the network between the two nodes.

Traffic is forwarded using a [greedy routing](https://en.wikipedia.org/wiki/Small-world_routing#Greedy_routing) scheme, where each node forwards the packet to a one-hop neighbor that is closer to the destination (according to this distance metric) than the current node.
In particular, when a packet needs to be forward, a node will forward it to whatever peer is closest to the destination in the greedy [metric space](https://en.wikipedia.org/wiki/Metric_space) used by the network, provided that the peer is closer to the destination than the current node.

If no closer peers are idle, then the packet is queued in FIFO order, with separate queues per destination coords (currently, as a bit of a hack, IPv6 flow labels are embedeed after the end of the significant part of the coords, so queues distinguish between different traffic streams with the same destination).
Whenever the node finishes forwarding a packet to a peer, it checks the queues, and will forward the first packet from the queue with the maximum `<age of first packet>/<queue size in bytes>`, i.e. the bandwidth the queue is attempting to use, subject to the constraint that the peer is a valid next hop (i.e. closer to the destination than the current node).
If no non-empty queue is available, then the peer is added to the idle set, forward packets when the need arises.

This acts as a crude approximation of backpressure routing, where the remote queue sizes are assumed to be equal to the distance of a node from a destination (rather than communicating queue size information), and packets are never forwarded "backwards" through the network, but congestion on a local link is routed around when possible.
The queue selection strategy behaves similar to shortest-queue-first, in that a larger fration of available bandwith to sessions that attempt to use less bandwidth, and is loosely based on the rationale behind some proposed solutions to the [cake-cutting](https://en.wikipedia.org/wiki/Fair_cake-cutting) problem.

The queue size is limited to 4 MB. If a packet is added to a queue and the total size of all queues is larger than this threshold, then a random queue is selected (with odds proportional to relative queue sizes), and the first packet from that queue is dropped, with the process repeated until the total queue size drops below the allowed threshold.

Note that this forwarding procedure generalizes to nodes that are not one-hop neighbors, but the current implementation omits the use of more distant neighbors, as this is expected to be a minor optimization (it would add per-link control traffic to pass path-vector-like information about a subset of the network, which is a lot of overhead compared to the current setup).

### Spanning Tree

A [spanning tree](https://en.wikipedia.org/wiki/Spanning_tree) is constructed with the tree rooted at the highest TreeID, where TreeID is equal to a sha512sum of a node's public [Ed25519](https://en.wikipedia.org/wiki/Ed25519) key (used for signing).
A node sends periodic advertisement messages to each neighbor.
The advertisement contains the coords that match the path from the root through the node, plus one additional hop from the node to the neighbor being advertised to.
Each hop in this advertisement includes a matching ed25519 signature.
These signatures prevent nodes from forging arbitrary routing advertisements.

The first hop, from the root, also includes a sequence number, which must be updated periodically.
A node will blacklist the current root (keeping a record of the last sequence number observed) if the root fails to update for longer than some timeout (currently hard coded at 1 minute).
Normally, a root node will update their sequence number for frequently than this (once every 30 seconds).
Nodes are throttled to ignore updates with a new sequence number for some period after updating their most recently seen sequence number (currently this cooldown is 15 seconds).
The implementation chooses to set the sequence number equal to the unix time on the root's clock, so that a new (higher) sequence number will be selected if the root is restarted and the clock is not set back.

Other than the root node, every other node in the network must select one of its neighbors to use as their parent.
This selection is done by tracking when each neighbor first sends us a message with a new timestamp from the root, to determine the ordering of the latency of each path from the root, to each neighbor, and then to the node that's searching for a parent.
These relative latencies are tracked by, for each neighbor, keeping a score vs each other neighbor.
If a neighbor sends a message with an updated timestamp before another neighbor, then the faster neighbor's score is increased by 1.
If the neighbor sends a message slower, then the score is decreased by 2, to make sure that a node must be reliably faster (at least 2/3 of the time) to see a net score increase over time.
If a node begins to advertise new coordinates, then its score vs all other nodes is reset to 0.
A node switches to a new parent if a neighbor's score (vs the current parent) reaches some threshold, currently 240, which corresponds to about 2 hours of being a reliably faster path.
The intended outcome of this process is that stable connections from fixed infrastructure near the "core" of the network should (eventually) select parents that minimize latency from the root to themselves, while the more dynamic parts of the network, presumably more towards the edges, will try to favor reliability when selecting a parent.

The distance metric between nodes is simply the distance between the nodes if they routed on the spanning tree.
This is equal to the sum of the distance from each node to the last common ancestor of the two nodes being compared.
The locator then consists of a root's key, timestamp, and coordinates representing each hop in the path from the root to the node.
In practice, only the coords are used for routing, while the root and timestamp, along with all the per-hop signatures, are needed to securely construct the spanning tree.

## Name-independent routing

A [Chord](https://en.wikipedia.org/wiki/Chord_(peer-to-peer))-like Distributed Hash Table (DHT) is used as a distributed database that maps NodeIDs onto coordinates in the spanning tree metric space.
The DHT is Chord-like in that it uses a successor/predecessor structure to do lookups in `O(n)` time with `O(1)` entries, then augments this with some additional information, adding roughly `O(logn)` additional entries, to reduce the lookup time to something around `O(logn)`.
In the long term, the idea is to favor spending our bandwidth making sure the minimum `O(1)` part is right, to prioritize correctness, and then try to conserve bandwidth (and power) by being a bit lazy about checking the remaining `O(logn)` portion when it's not in use.

To be specific, the DHT stores the immediate successor of a node, plus the next node it manages to find which is strictly closer (by the tree hop-count metric) than all previous nodes.
The same process is repeated for predecessor nodes, and lookups walk the network in the predecessor direction, with each key being owned by its successor (to make sure defaulting to 0 for unknown bits of a `NodeID` doesn't cause us to overshoot the target during a lookup).
In addition, all of a node's one-hop neighbors are included in the DHT, since we get this information "for free", and we must include it in our DHT to ensure that the network doesn't diverge to a broken state (though I suspect that only adding parents or parent-child relationships may be sufficient -- worth trying to prove or disprove, if somebody's bored).
The DHT differs from Chord in that there are no values in the key:value store -- it only stores information about DHT peers -- and that it uses a [Kademlia](https://en.wikipedia.org/wiki/Kademlia)-inspired iterative-parallel lookup process.

To summarize the entire routing procedure, when given only a node's IP address, the goal is to find a route to the destination.
That happens through 3 steps:

1. The address is unpacked into the known bits of a NodeID and a bitmask to signal which bits of the NodeID are known (the unknown bits are ignored).
2. A DHT search is performed, which normally results in a response from the node closest in the DHT keyspace to the target `NodeID`. The response contains the node's curve25519 public key, which is checked to match the `NodeID` (and therefore the address), as well as the node's coordinates.
3. Using the keys and coords from the above step, an ephemeral key exchange occurs between the source and destination nodes. These ephemeral session keys are used to encrypt any ordinary IPv6 traffic that may be encapsulated and sent between the nodes.

From that point, the session keys and coords are cached and used to encrypt and send traffic between nodes. This is *mostly* transparent to the user: the initial DHT lookup and key exchange takes at least 2 round trips, so there's some delay before session setup completes and normal IPv6 traffic can flow. This is similar to the delay caused by a DNS lookup, although it generally takes longer, as a DHT lookup requires multiple iterations to reach the destination.

## Project Status and Plans

The current (Go) implementation is considered alpha, so compatibility with future versions is neither guaranteed nor expected.
While users are discouraged from running anything truly critical on top of it, as of writing, it seems reliable enough for day-to-day use.

As an "alpha" quality release, Yggdrasil *should* at least be able to detect incompatible versions when it sees them, and warn the users that an update may be needed.
A "beta" quality release should know enough to be compatible in the face of wire format changes, and reasonably feature complete.
A "stable" 1.0 release, if it ever happens, would probably be feature complete, with no expectation of future wire format changes, and free of known critical bugs.

Roughly speaking, there are a few obvious ways the project could turn out:

1. The developers could lose interest before it goes anywhere.
2. The project could be reasonably complete (beta or stable), but never gain a significant number of users.
3. The network may grow large enough that fundamental (non-fixable) design problems appear, which is hopefully a learning experience, but the project may die as a result.
4. The network may grow large, but never hit any design problems, in which case we need to think about either moving the important parts into other projects ([cjdns](https://github.com/cjdelisle/cjdns)) or rewriting compatible implementations that are better optimized for the target platforms (e.g. a linux kernel module).

That last one is probably impossible, because the speed of light would *eventually* become a problem, for a sufficiently large network.
If the only thing limiting network growth turns out to be the underlying physics, then that arguably counts as a win.

Also, note that some design decisions were made for ease-of-programming or ease-of-testing reasons, and likely need to be reconsidered at some point.
In particular, Yggdrasil currently uses TCP for connections with one-hop neighbors, which introduces an additional layer of buffering that can lead to increased and/or unstable latency in congested areas of the network.

