package main

import (
	"io/ioutil"

	"github.com/gologme/log"

	//"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	//"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

type simNode struct {
	core yggdrasil.Core
	id   int
}

func newNode(id int) *simNode {
	n := simNode{id: id}
	n.core.Start(config.GenerateConfig(), log.New(ioutil.Discard, "", 0))
	return &n
}
