package main

import (
	"bytes"
	"log"
	"os"

	"github.com/RiV-chain/RiV-mesh/src/defaults"
	"github.com/hjson/hjson-go"
	"github.com/webview/webview"
	"golang.org/x/text/encoding/unicode"

	"github.com/docopt/docopt-go"
)

var usage = `Graphical interface for RiV mesh.

Usage:
  mesh-ui [<index>] [-c]
  mesh-ui -h | --help
  mesh-ui -v | --version

Options:
  <index>       Index file name [default: http://localhost:19019].
  -c --console  Show debug console window.
  -h --help     Show this screen.
  -v --version  Show version.`

var confui struct {
	IndexHtml string `docopt:"<index>"`
	Console   bool   `docopt:"-c,--console"`
}

var uiVersion = "0.0.1"

func main() {
	opts, _ := docopt.ParseArgs(usage, os.Args[1:], uiVersion)
	opts.Bind(&confui)
	if !confui.Console {
		Console(false)
	}
	debug := true
	w := webview.New(debug)
	defer w.Destroy()
	w.SetTitle("RiV-mesh")
	w.SetSize(690, 920, webview.HintFixed)

	if confui.IndexHtml == "" {
		confui.IndexHtml = getEndpoint()
	}
	if confui.IndexHtml == "" {
		confui.IndexHtml = "http://localhost:19019"
	}

	log.Printf("Opening: %v", confui.IndexHtml)
	w.Navigate(confui.IndexHtml)
	w.Run()
}

func getEndpoint() string {
	if config, err := os.ReadFile(defaults.GetDefaults().DefaultConfigFile); err == nil {
		if bytes.Equal(config[0:2], []byte{0xFF, 0xFE}) ||
			bytes.Equal(config[0:2], []byte{0xFE, 0xFF}) {
			utf := unicode.UTF16(unicode.BigEndian, unicode.UseBOM)
			decoder := utf.NewDecoder()
			config, err = decoder.Bytes(config)
			if err != nil {
				return ""
			}
		}
		var dat map[string]interface{}
		if err := hjson.Unmarshal(config, &dat); err != nil {
			return ""
		}
		if ep, ok := dat["HttpAddress"].(string); ok && (ep != "none" && ep != "") {
			return ep
		}
	}
	return ""
}
