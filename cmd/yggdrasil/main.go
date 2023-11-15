package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gologme/log"
	gsyslog "github.com/hashicorp/go-syslog"
	"github.com/hjson/hjson-go/v4"
	"github.com/kardianos/minwinsvc"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/setup"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

// The main function is responsible for configuring and starting Yggdrasil.
func main() {
	args := setup.ParseArguments()

	// Catch interrupts from the operating system to exit gracefully.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// Capture the service being stopped on Windows.
	minwinsvc.SetOnExit(cancel)

	// Create a new logger that logs output to stdout.
	var logger *log.Logger
	switch args.LogTo {
	case "stdout":
		logger = log.New(os.Stdout, "", log.Flags())

	case "syslog":
		if syslogger, err := gsyslog.NewLogger(gsyslog.LOG_NOTICE, "DAEMON", version.BuildName()); err == nil {
			logger = log.New(syslogger, "", log.Flags()&^(log.Ldate|log.Ltime))
		}

	default:
		if logfd, err := os.OpenFile(args.LogTo, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			logger = log.New(logfd, "", log.Flags())
		}
	}
	if logger == nil {
		logger = log.New(os.Stdout, "", log.Flags())
		logger.Warnln("Logging defaulting to stdout")
	}

	var cfg *config.NodeConfig
	var err error
	switch {
	case args.Version:
		fmt.Println("Build name:", version.BuildName())
		fmt.Println("Build version:", version.BuildVersion())
		return

	case args.AutoConf:
		// Use an autoconf-generated config, this will give us random keys and
		// port numbers, and will use an automatically selected TUN/TAP interface.
		cfg = config.GenerateConfig()

	case args.UseConfFile != "" || args.UseConf:
		// Read the configuration from either stdin or from the filesystem
		cfg = setup.ReadConfig(logger, args.UseConf, args.UseConfFile, args.NormaliseConf)
		// If the -normaliseconf option was specified then remarshal the above
		// configuration and print it back to stdout. This lets the user update
		// their configuration file with newly mapped names (like above) or to
		// convert from plain JSON to commented HJSON.
		if args.NormaliseConf {
			var bs []byte
			if args.ConfJSON {
				bs, err = json.MarshalIndent(cfg, "", "  ")
			} else {
				bs, err = hjson.Marshal(cfg)
			}
			if err != nil {
				panic(err)
			}
			fmt.Println(string(bs))
			return
		}

	case args.GenConf:
		// Generate a new configuration and print it to stdout.
		fmt.Printf("%s\n", config.GenerateConfigJSON(args.ConfJSON))
		return

	default:
		// No flags were provided, therefore print the list of flags to stdout.
		fmt.Println("Usage:")
		flag.PrintDefaults()
	}

	// Create a new standalone node
	n := setup.NewNode(cfg, logger)
	n.SetLogLevel(args.LogLevel)

	// Now start Yggdrasil - this starts the router, switch and other core
	// components needed for Yggdrasil to operate
	if err = n.Run(args); err != nil {
		logger.Fatalln(err)
	}

	// Setup the TUN module.
	if err = n.SetupTun(); err != nil {
		panic(err)
	}

	// Block until we are told to shut down.
	<-ctx.Done()

	// Shut down the node.
	n.Close()
}
