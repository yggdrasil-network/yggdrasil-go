package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
	"github.com/yggdrasil-network/yggdrasil-go/src/tun"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

func main() {
	// makes sure we can use defer and still return an error code to the OS
	os.Exit(run())
}

func run() int {
	logbuffer := &bytes.Buffer{}
	logger := log.New(logbuffer, "", log.Flags())

	defer func() int {
		if r := recover(); r != nil {
			logger.Println("Fatal error:", r)
			fmt.Print(logbuffer)
			return 1
		}
		return 0
	}()

	cmdLineEnv := newCmdLineEnv()
	cmdLineEnv.parseFlagsAndArgs()

	if cmdLineEnv.ver {
		fmt.Println("Build name:", version.BuildName())
		fmt.Println("Build version:", version.BuildVersion())
		fmt.Println("To get the version number of the running Yggdrasil node, run", os.Args[0], "getSelf")
		return 0
	}

	if len(cmdLineEnv.args) == 0 {
		flag.Usage()
		return 0
	}

	cmdLineEnv.setEndpoint(logger)

	var conn net.Conn
	u, err := url.Parse(cmdLineEnv.endpoint)
	if err == nil {
		switch strings.ToLower(u.Scheme) {
		case "unix":
			logger.Println("Connecting to UNIX socket", cmdLineEnv.endpoint[7:])
			conn, err = net.Dial("unix", cmdLineEnv.endpoint[7:])
		case "tcp":
			logger.Println("Connecting to TCP socket", u.Host)
			conn, err = net.Dial("tcp", u.Host)
		default:
			logger.Println("Unknown protocol or malformed address - check your endpoint")
			err = errors.New("protocol not supported")
		}
	} else {
		logger.Println("Connecting to TCP socket", u.Host)
		conn, err = net.Dial("tcp", cmdLineEnv.endpoint)
	}
	if err != nil {
		panic(err)
	}

	logger.Println("Connected")
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	send := &admin.AdminSocketRequest{}
	recv := &admin.AdminSocketResponse{}
	args := map[string]string{}
	for c, a := range cmdLineEnv.args {
		if c == 0 {
			if strings.HasPrefix(a, "-") {
				logger.Printf("Ignoring flag %s as it should be specified before other parameters\n", a)
				continue
			}
			logger.Printf("Sending request: %v\n", a)
			send.Name = a
			continue
		}
		tokens := strings.SplitN(a, "=", 2)
		switch {
		case len(tokens) == 1:
			logger.Println("Ignoring invalid argument:", a)
		default:
			args[tokens[0]] = tokens[1]
		}
	}
	if send.Arguments, err = json.Marshal(args); err != nil {
		panic(err)
	}
	if err := encoder.Encode(&send); err != nil {
		panic(err)
	}
	logger.Printf("Request sent")
	if err := decoder.Decode(&recv); err != nil {
		panic(err)
	}
	if recv.Status == "error" {
		if err := recv.Error; err != "" {
			fmt.Println("Admin socket returned an error:", err)
		} else {
			fmt.Println("Admin socket returned an error but didn't specify any error text")
		}
		return 1
	}
	if cmdLineEnv.injson {
		if json, err := json.MarshalIndent(recv.Response, "", "  "); err == nil {
			fmt.Println(string(json))
		}
		return 0
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetAutoFormatHeaders(false)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetTablePadding("\t") // pad with tabs
	table.SetNoWhiteSpace(true)
	table.SetAutoWrapText(false)

	switch strings.ToLower(send.Name) {
	case "list":
		var resp admin.ListResponse
		if err := json.Unmarshal(recv.Response, &resp); err != nil {
			panic(err)
		}
		table.SetHeader([]string{"Command", "Arguments", "Description"})
		for _, entry := range resp.List {
			for i := range entry.Fields {
				entry.Fields[i] = entry.Fields[i] + "=..."
			}
			table.Append([]string{entry.Command, strings.Join(entry.Fields, ", "), entry.Description})
		}
		table.Render()

	case "getself":
		var resp admin.GetSelfResponse
		if err := json.Unmarshal(recv.Response, &resp); err != nil {
			panic(err)
		}
		table.Append([]string{"Build name:", resp.BuildName})
		table.Append([]string{"Build version:", resp.BuildVersion})
		table.Append([]string{"IPv6 address:", resp.IPAddress})
		table.Append([]string{"IPv6 subnet:", resp.Subnet})
		table.Append([]string{"Routing table size:", fmt.Sprintf("%d", resp.RoutingEntries)})
		table.Append([]string{"Public key:", resp.PublicKey})
		table.Render()

	case "getpeers":
		var resp admin.GetPeersResponse
		if err := json.Unmarshal(recv.Response, &resp); err != nil {
			panic(err)
		}
		table.SetHeader([]string{"URI", "State", "Dir", "IP Address", "Uptime", "RX", "TX", "Pr", "Last Error"})
		for _, peer := range resp.Peers {
			state, lasterr, dir := "Up", "-", "Out"
			if !peer.Up {
				state, lasterr = "Down", fmt.Sprintf("%s ago: %s", peer.LastErrorTime.Round(time.Second), peer.LastError)
			}
			if peer.Inbound {
				dir = "In"
			}
			uristring := peer.URI
			if uri, err := url.Parse(peer.URI); err == nil {
				uri.RawQuery = ""
				uristring = uri.String()
			}
			table.Append([]string{
				uristring,
				state,
				dir,
				peer.IPAddress,
				(time.Duration(peer.Uptime) * time.Second).String(),
				peer.RXBytes.String(),
				peer.TXBytes.String(),
				fmt.Sprintf("%d", peer.Priority),
				lasterr,
			})
		}
		table.Render()

	case "gettree":
		var resp admin.GetTreeResponse
		if err := json.Unmarshal(recv.Response, &resp); err != nil {
			panic(err)
		}
		//table.SetHeader([]string{"Public Key", "IP Address", "Port", "Rest"})
		table.SetHeader([]string{"Public Key", "IP Address", "Parent", "Sequence"})
		for _, tree := range resp.Tree {
			table.Append([]string{
				tree.PublicKey,
				tree.IPAddress,
				tree.Parent,
				fmt.Sprintf("%d", tree.Sequence),
				//fmt.Sprintf("%d", dht.Port),
				//fmt.Sprintf("%d", dht.Rest),
			})
		}
		table.Render()

	case "getpaths":
		var resp admin.GetPathsResponse
		if err := json.Unmarshal(recv.Response, &resp); err != nil {
			panic(err)
		}
		table.SetHeader([]string{"Public Key", "IP Address", "Path", "Seq"})
		for _, p := range resp.Paths {
			table.Append([]string{
				p.PublicKey,
				p.IPAddress,
				fmt.Sprintf("%v", p.Path),
				fmt.Sprintf("%d", p.Sequence),
			})
		}
		table.Render()

	case "getsessions":
		var resp admin.GetSessionsResponse
		if err := json.Unmarshal(recv.Response, &resp); err != nil {
			panic(err)
		}
		table.SetHeader([]string{"Public Key", "IP Address", "Uptime", "RX", "TX"})
		for _, p := range resp.Sessions {
			table.Append([]string{
				p.PublicKey,
				p.IPAddress,
				(time.Duration(p.Uptime) * time.Second).String(),
				p.RXBytes.String(),
				p.TXBytes.String(),
			})
		}
		table.Render()

	case "getnodeinfo":
		var resp core.GetNodeInfoResponse
		if err := json.Unmarshal(recv.Response, &resp); err != nil {
			panic(err)
		}
		for _, v := range resp {
			fmt.Println(string(v))
			break
		}

	case "getmulticastinterfaces":
		var resp multicast.GetMulticastInterfacesResponse
		if err := json.Unmarshal(recv.Response, &resp); err != nil {
			panic(err)
		}
		table.SetHeader([]string{"Interface"})
		for _, p := range resp.Interfaces {
			table.Append([]string{p})
		}
		table.Render()

	case "gettun":
		var resp tun.GetTUNResponse
		if err := json.Unmarshal(recv.Response, &resp); err != nil {
			panic(err)
		}
		table.Append([]string{"TUN enabled:", fmt.Sprintf("%#v", resp.Enabled)})
		if resp.Enabled {
			table.Append([]string{"Interface name:", resp.Name})
			table.Append([]string{"Interface MTU:", fmt.Sprintf("%d", resp.MTU)})
		}
		table.Render()

	case "addpeer", "removepeer":

	default:
		fmt.Println(string(recv.Response))
	}

	return 0
}
