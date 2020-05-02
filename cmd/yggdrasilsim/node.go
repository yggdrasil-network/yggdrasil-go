package main

import (
	"io/ioutil"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

type simNode struct {
	core     yggdrasil.Core
	id       int
	nodeID   crypto.NodeID
	dialer   *yggdrasil.Dialer
	listener *yggdrasil.Listener
}

func newNode(id int) *simNode {
	n := simNode{id: id}
	n.core.Start(config.GenerateConfig(), log.New(ioutil.Discard, "", 0))
	n.nodeID = *n.core.NodeID()
	n.dialer, _ = n.core.ConnDialer()
	n.listener, _ = n.core.ConnListen()
	return &n
}
