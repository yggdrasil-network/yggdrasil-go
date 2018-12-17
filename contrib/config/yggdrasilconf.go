package main

/*
This is a small utility that is designed to accompany the vyatta-yggdrasil
package. It takes a HJSON configuration file, makes changes to it based on
the command line arguments, and then spits out an updated file.
*/

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"strconv"

	"github.com/hjson/hjson-go"
	"golang.org/x/text/encoding/unicode"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

type nodeConfig = config.NodeConfig

func main() {
	useconffile := flag.String("useconffile", "/etc/yggdrasil.conf", "update config at specified file path")
	flag.Parse()
	cfg := nodeConfig{}
	var config []byte
	var err error
	config, err = ioutil.ReadFile(*useconffile)
	if err != nil {
		panic(err)
	}
	if bytes.Compare(config[0:2], []byte{0xFF, 0xFE}) == 0 ||
		bytes.Compare(config[0:2], []byte{0xFE, 0xFF}) == 0 {
		utf := unicode.UTF16(unicode.BigEndian, unicode.UseBOM)
		decoder := utf.NewDecoder()
		config, err = decoder.Bytes(config)
		if err != nil {
			panic(err)
		}
	}
	var dat map[string]interface{}
	if err := hjson.Unmarshal(config, &dat); err != nil {
		panic(err)
	}
	confJson, err := json.Marshal(dat)
	if err != nil {
		panic(err)
	}
	json.Unmarshal(confJson, &cfg)
	switch flag.Arg(0) {
	case "setMTU":
		cfg.IfMTU, err = strconv.Atoi(flag.Arg(1))
		if err != nil {
			cfg.IfMTU = 1280
		}
		if mtu, _ := strconv.Atoi(flag.Arg(1)); mtu < 1280 {
			cfg.IfMTU = 1280
		}
	case "setIfName":
		cfg.IfName = flag.Arg(1)
	case "setListen":
		cfg.Listen = flag.Arg(1)
	case "setAdminListen":
		cfg.AdminListen = flag.Arg(1)
	case "setIfTapMode":
		if flag.Arg(1) == "true" {
			cfg.IfTAPMode = true
		} else {
			cfg.IfTAPMode = false
		}
	case "addPeer":
		found := false
		for _, v := range cfg.Peers {
			if v == flag.Arg(1) {
				found = true
			}
		}
		if !found {
			cfg.Peers = append(cfg.Peers, flag.Arg(1))
		}
	case "removePeer":
		for k, v := range cfg.Peers {
			if v == flag.Arg(1) {
				cfg.Peers = append(cfg.Peers[:k], cfg.Peers[k+1:]...)
			}
		}
	}
	bs, err := hjson.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(bs))
	return
}
