package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/hjson/hjson-go/v4"
	"golang.org/x/text/encoding/unicode"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

type CmdLineEnv struct {
	args             []string
	endpoint, server string
	injson, ver      bool
}

func newCmdLineEnv() CmdLineEnv {
	var cmdLineEnv CmdLineEnv
	cmdLineEnv.endpoint = config.GetDefaults().DefaultAdminListen
	return cmdLineEnv
}

func (cmdLineEnv *CmdLineEnv) parseFlagsAndArgs() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] command [key=value] [key=value] ...\n\n", os.Args[0])
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Please note that options must always specified BEFORE the command\non the command line or they will be ignored.")
		fmt.Println()
		fmt.Println("Commands:\n  - Use \"list\" for a list of available commands")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  - ", os.Args[0], "list")
		fmt.Println("  - ", os.Args[0], "getPeers")
		fmt.Println("  - ", os.Args[0], "setTunTap name=auto mtu=1500 tap_mode=false")
		fmt.Println("  - ", os.Args[0], "-endpoint=tcp://localhost:9001 getPeers")
		fmt.Println("  - ", os.Args[0], "-endpoint=unix:///var/run/ygg.sock getPeers")
	}

	server := flag.String("endpoint", cmdLineEnv.endpoint, "Admin socket endpoint")
	injson := flag.Bool("json", false, "Output in JSON format (as opposed to pretty-print)")
	ver := flag.Bool("version", false, "Prints the version of this build")

	flag.Parse()

	cmdLineEnv.args = flag.Args()
	cmdLineEnv.server = *server
	cmdLineEnv.injson = *injson
	cmdLineEnv.ver = *ver
}

func (cmdLineEnv *CmdLineEnv) setEndpoint(logger *log.Logger) {
	if cmdLineEnv.server == cmdLineEnv.endpoint {
		if cfg, err := os.ReadFile(config.GetDefaults().DefaultConfigFile); err == nil {
			if bytes.Equal(cfg[0:2], []byte{0xFF, 0xFE}) ||
				bytes.Equal(cfg[0:2], []byte{0xFE, 0xFF}) {
				utf := unicode.UTF16(unicode.BigEndian, unicode.UseBOM)
				decoder := utf.NewDecoder()
				cfg, err = decoder.Bytes(cfg)
				if err != nil {
					panic(err)
				}
			}
			var dat map[string]interface{}
			if err := hjson.Unmarshal(cfg, &dat); err != nil {
				panic(err)
			}
			if ep, ok := dat["AdminListen"].(string); ok && (ep != "none" && ep != "") {
				cmdLineEnv.endpoint = ep
				logger.Println("Found platform default config file", config.GetDefaults().DefaultConfigFile)
				logger.Println("Using endpoint", cmdLineEnv.endpoint, "from AdminListen")
			} else {
				logger.Println("Configuration file doesn't contain appropriate AdminListen option")
				logger.Println("Falling back to platform default", config.GetDefaults().DefaultAdminListen)
			}
		} else {
			logger.Println("Can't open config file from default location", config.GetDefaults().DefaultConfigFile)
			logger.Println("Falling back to platform default", config.GetDefaults().DefaultAdminListen)
		}
	} else {
		cmdLineEnv.endpoint = cmdLineEnv.server
		logger.Println("Using endpoint", cmdLineEnv.endpoint, "from command line")
	}
}
