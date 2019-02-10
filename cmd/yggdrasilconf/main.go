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
	"os"
	"reflect"

	"github.com/hjson/hjson-go"
	"golang.org/x/text/encoding/unicode"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

type nodeConfig = config.NodeConfig

func main() {
	useconffile := flag.String("useconffile", "/etc/yggdrasil.conf", "update config at specified file path")
	usejson := flag.Bool("json", false, "write out new config as JSON instead of HJSON")

	var action string
	switch flag.Arg(0) {
	case "get":
	case "set":
	case "add":
	case "remove":
		action = flag.Arg(0)
	default:
		fmt.Errorf("Action must be get, set, add or remove")
	}

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
	if err = hjson.Unmarshal(config, &dat); err != nil {
		panic(err)
	}
	confJSON, err := json.Marshal(dat)
	if err != nil {
		panic(err)
	}
	json.Unmarshal(confJSON, &cfg)

	item := reflect.ValueOf(cfg)
	for index, arg := range flag.Args() {
		if *set || *add || *remove {

		}
		if item.Kind() == reflect.Map {
			for _, key := range item.MapKeys() {
				if key.String() == arg {
					item = item.MapIndex(key)
				}
			}
		} else {
			item = item.FieldByName(arg)
		}
		if !item.IsValid() {
			os.Exit(1)
			return
		}
	}
	var bs []byte
	if *usejson {
		bs, err = json.Marshal(item.Interface())
	} else {
		bs, err = hjson.Marshal(item.Interface())
	}
	if err != nil {
		panic(err)
	}
	fmt.Println(string(bs))
	os.Exit(0)

	/* else {
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
		case "setNodeInfoName":
			cfg.NodeInfo["name"] = flag.Arg(1)
		}
	}*/
	var bs []byte
	if *usejson {
		bs, err = json.Marshal(cfg)
	} else {
		bs, err = hjson.Marshal(cfg)
	}
	if err != nil {
		panic(err)
	}
	fmt.Println(string(bs))
	return
}
