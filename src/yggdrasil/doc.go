/*
Package yggdrasil implements the core functionality of the Yggdrasil Network.

Introduction

Yggdrasil is a proof-of-concept mesh network which provides end-to-end encrypted
communication between nodes in a decentralised fashion. The network is arranged
using a globally-agreed spanning tree which provides each node with a locator
(coordinates relative to the root) and a distributed hash table (DHT) mechanism
for finding other nodes.

Each node also implements a router, which is responsible for encryption of
traffic, searches and connections, and a switch, which is responsible ultimately
for forwarding traffic across the network.

While many Yggdrasil nodes in existence today are IP nodes - that is, they are
transporting IPv6 packets, like a kind of mesh VPN - it is also possible to
integrate Yggdrasil into your own applications and use it as a generic data
transport, similar to UDP.

This library is what you need to integrate and use Yggdrasil in your own
application.

Basics

In order to start an Yggdrasil node, you should start by generating node
configuration, which amongst other things, includes encryption keypairs which
are used to generate the node's identity, and supply a logger which Yggdrasil's
output will be written to.

This may look something like this:

  import (
    "os"
    "github.com/gologme/log"
    "github.com/yggdrasil-network/yggdrasil-go/src/config"
    "github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
  )

  type node struct {
    core   yggdrasil.Core
    config *config.NodeConfig
    log    *log.Logger
  }

You then can supply node configuration and a logger:

  n := node{}
  n.log = log.New(os.Stdout, "", log.Flags())
  n.config = config.GenerateConfig()

In the above example, we ask the config package to supply new configuration each
time, which results in fresh encryption keys and therefore a new identity. It is
normally preferable in most cases to persist node configuration onto the
filesystem or into some configuration store so that the node's identity does not
change each time that the program starts. Note that Yggdrasil will automatically
fill in any missing configuration items with sane defaults.

Once you have supplied a logger and some node configuration, you can then start
the node:

  n.core.Start(n.config, n.log)

Add some peers to connect to the network:

  n.core.AddPeer("tcp://some-host.net:54321", "")
  n.core.AddPeer("tcp://[2001::1:2:3]:54321", "")
  n.core.AddPeer("tcp://1.2.3.4:54321", "")

You can also ask the API for information about our node:

  n.log.Println("My node ID is", n.core.NodeID())
  n.log.Println("My public key is", n.core.EncryptionPublicKey())
  n.log.Println("My coords are", n.core.Coords())

Incoming Connections

Once your node is started, you can then listen for connections from other nodes
by asking the API for a Listener:

  listener, err := n.core.ConnListen()
  if err != nil {
    // ...
  }

The Listener has a blocking Accept function which will wait for incoming
connections from remote nodes. It will return a Conn when a connection is
received. If the node never receives any incoming connections then this function
can block forever, so be prepared for that, perhaps by listening in a separate
goroutine.

Assuming that you have defined a myConnectionHandler function to deal with
incoming connections:

  for {
    conn, err := listener.Accept()
    if err != nil {
      // ...
    }

    // We've got a new connection
    go myConnectionHandler(conn)
  }

Outgoing Connections

If you know the node ID of the remote node that you want to talk to, you can
dial an outbound connection to it. To do this, you should first ask the API for
a Dialer:

  dialer, err := n.core.ConnDialer()
  if err != nil {
    // ...
  }

You can then dial using the 16-byte node ID in hexadecimal format, for example:

  conn, err := dialer.Dial("nodeid", "24a58cfce691ec016b0f698f7be1bee983cea263781017e99ad3ef62b4ef710a45d6c1a072c5ce46131bd574b78818c9957042cafeeed13966f349e94eb771bf")
  if err != nil {
    // ...
  }

Using Connections

Conn objects are implementations of io.ReadWriteCloser, and as such, you can
Read, Write and Close them as necessary.

For example, to write to the Conn from the supplied buffer:

  buf := []byte{1, 2, 3, 4, 5}
  w, err := conn.Write(buf)
  if err != nil {
    // ...
  } else {
    // written w bytes
  }

Reading from the Conn into the supplied buffer:

  buf := make([]byte, 65535)
  r, err := conn.Read(buf)
  if err != nil {
    // ...
  } else {
    // read r bytes
  }

When you are happy that a connection is no longer required, you can discard it:

  err := conn.Close()
  if err != nil {
    // ...
  }

*/
package yggdrasil
