package main

import "flag"

type CmdLineEnv struct {
	genconf       bool
	useconf       bool
	useconffile   string
	normaliseconf bool
	confjson      bool
	autoconf      bool
	ver           bool
	logto         string
	getaddr       bool
	getsnet       bool
	loglevel      string
}

func (cmdLineEnv *CmdLineEnv) parseFlagsAndArgs() {
	genconf := flag.Bool("genconf", false, "print a new config to stdout")
	useconf := flag.Bool("useconf", false, "read HJSON/JSON config from stdin")
	useconffile := flag.String("useconffile", "", "read HJSON/JSON config from specified file path")
	normaliseconf := flag.Bool("normaliseconf", false, "use in combination with either -useconf or -useconffile, outputs your configuration normalised")
	confjson := flag.Bool("json", false, "print configuration from -genconf or -normaliseconf as JSON instead of HJSON")
	autoconf := flag.Bool("autoconf", false, "automatic mode (dynamic IP, peer with IPv6 neighbors)")
	ver := flag.Bool("version", false, "prints the version of this build")
	logto := flag.String("logto", "stdout", "file path to log to, \"syslog\" or \"stdout\"")
	getaddr := flag.Bool("address", false, "returns the IPv6 address as derived from the supplied configuration")
	getsnet := flag.Bool("subnet", false, "returns the IPv6 subnet as derived from the supplied configuration")
	loglevel := flag.String("loglevel", "info", "loglevel to enable")

	flag.Parse()

	cmdLineEnv.genconf       = *genconf
	cmdLineEnv.useconf       = *useconf
	cmdLineEnv.useconffile   = *useconffile
	cmdLineEnv.normaliseconf = *normaliseconf
	cmdLineEnv.confjson      = *confjson
	cmdLineEnv.autoconf      = *autoconf
	cmdLineEnv.ver           = *ver
	cmdLineEnv.logto         = *logto
	cmdLineEnv.getaddr       = *getaddr
	cmdLineEnv.getsnet       = *getsnet
	cmdLineEnv.loglevel      = *loglevel
}
