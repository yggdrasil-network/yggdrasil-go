package cmd_line_env

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/hjson/hjson-go"
	"golang.org/x/text/encoding/unicode"

	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
)

type CmdLineEnv struct {
	Args []string
	Endpoint, Server string
	Injson, Verbose, Ver bool
}

func New() CmdLineEnv {
	var cmdLineEnv CmdLineEnv
	cmdLineEnv.Endpoint = defaults.GetDefaults().DefaultAdminListen
	return cmdLineEnv
}

func (cmdLineEnv *CmdLineEnv)ParseFlagsAndArgs() {
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
		fmt.Println("  - ", os.Args[0], "-v getSelf")
		fmt.Println("  - ", os.Args[0], "setTunTap name=auto mtu=1500 tap_mode=false")
		fmt.Println("  - ", os.Args[0], "-endpoint=tcp://localhost:9001 getDHT")
		fmt.Println("  - ", os.Args[0], "-endpoint=unix:///var/run/ygg.sock getDHT")
	}

	server := flag.String("endpoint", cmdLineEnv.Endpoint, "Admin socket endpoint")
	injson := flag.Bool("json", false, "Output in JSON format (as opposed to pretty-print)")
	verbose := flag.Bool("v", false, "Verbose output (includes public keys)")
	ver := flag.Bool("version", false, "Prints the version of this build")

	flag.Parse()

	cmdLineEnv.Args = flag.Args()
	cmdLineEnv.Server = *server
	cmdLineEnv.Injson = *injson
	cmdLineEnv.Verbose = *verbose
	cmdLineEnv.Ver = *ver
}

func (cmdLineEnv *CmdLineEnv)SetEndpoint(logger *log.Logger) {
	if cmdLineEnv.Server == cmdLineEnv.Endpoint {
		if config, err := ioutil.ReadFile(defaults.GetDefaults().DefaultConfigFile); err == nil {
			if bytes.Equal(config[0:2], []byte{0xFF, 0xFE}) ||
				bytes.Equal(config[0:2], []byte{0xFE, 0xFF}) {
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
			if ep, ok := dat["AdminListen"].(string); ok && (ep != "none" && ep != "") {
				cmdLineEnv.Endpoint = ep
				logger.Println("Found platform default config file", defaults.GetDefaults().DefaultConfigFile)
				logger.Println("Using endpoint", cmdLineEnv.Endpoint, "from AdminListen")
			} else {
				logger.Println("Configuration file doesn't contain appropriate AdminListen option")
				logger.Println("Falling back to platform default", defaults.GetDefaults().DefaultAdminListen)
			}
		} else {
			logger.Println("Can't open config file from default location", defaults.GetDefaults().DefaultConfigFile)
			logger.Println("Falling back to platform default", defaults.GetDefaults().DefaultAdminListen)
		}
	} else {
		cmdLineEnv.Endpoint = cmdLineEnv.Server
		logger.Println("Using endpoint", cmdLineEnv.Endpoint, "from command line")
	}
}
