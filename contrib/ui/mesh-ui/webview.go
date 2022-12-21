package main

import (
	"log"
	"os"

	"github.com/RiV-chain/RiV-mesh/src/defaults"

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
	w := New(debug)
	defer w.Destroy()
	w.SetTitle("RiV-mesh")
	w.SetSize(690, 920, HintFixed)

	if confui.IndexHtml == "" {
		confui.IndexHtml = defaults.GetHttpEndpoint("http://localhost:19019")
	}

	log.Printf("Opening: %v", confui.IndexHtml)
	w.Navigate(confui.IndexHtml)
	w.Run()
}
