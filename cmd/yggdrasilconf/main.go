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
	"strconv"
	"strings"

	"github.com/hjson/hjson-go"
	"golang.org/x/text/encoding/unicode"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

type nodeConfig = config.NodeConfig

func main() {
	useconffile := flag.String("useconffile", "/etc/yggdrasil.conf", "update config at specified file path")
	usejson := flag.Bool("json", false, "write out new config as JSON instead of HJSON")
	flag.Parse()
	flags := flag.Args()
	if len(flags) == 0 {
		fmt.Println("No arguments given")
		os.Exit(1)
	}
	action := flags[0]
	switch strings.ToLower(flags[0]) {
	case "get":
	case "set":
	case "add":
	case "remove":
		action = strings.ToLower(flags[0])
	default:
		fmt.Println("Unknown action", flags[0])
		os.Exit(1)
	}
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
	item := reflect.ValueOf(&cfg).Elem()
	for index, arg := range flags {
		switch index {
		case 0:
			continue
		case len(flags) - 2:
			fallthrough
		case len(flags) - 1:
			if action != "get" {
				continue
			}
			fallthrough
		default:
			switch item.Kind() {
			case reflect.Map:
				for _, key := range item.MapKeys() {
					if key.String() == arg {
						item = item.MapIndex(key)
					}
				}
			case reflect.Struct:
				item = item.FieldByName(arg)
			}
			if !item.IsValid() {
				os.Exit(1)
			}
		}
	}
	switch action {
	case "get":
		var bs []byte
		if *usejson {
			if bs, err = json.Marshal(item.Interface()); err == nil {
				fmt.Println(string(bs))
			}
		} else {
			if bs, err = hjson.Marshal(item.Interface()); err == nil {
				fmt.Println(string(bs))
			}
		}
		if err != nil {
			panic(err)
		}
		return
	case "set":
		name := flags[len(flags)-2:][0]
		value := flags[len(flags)-1:][0]
		switch item.Kind() {
		case reflect.Struct:
			field := item.FieldByName(name)
			if !field.IsValid() {
				break
			}
			switch field.Kind() {
			case reflect.String:
				field.SetString(value)
			case reflect.Bool:
				field.SetBool(value == "true")
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				if int, ierr := strconv.ParseInt(value, 10, 64); ierr == nil {
					field.SetInt(int)
				}
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				if uint, uerr := strconv.ParseUint(value, 10, 64); uerr == nil {
					field.SetUint(uint)
				}
			}
		case reflect.Map:
			intf := item.Interface().(map[string]interface{})
			intf[name] = value
		}
	}
	var bs []byte
	if *usejson {
		if bs, err = json.Marshal(cfg); err == nil {
			fmt.Println(string(bs))
		}
	} else {
		if bs, err = hjson.Marshal(cfg); err == nil {
			fmt.Println(string(bs))
		}
	}
}
