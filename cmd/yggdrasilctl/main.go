package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
)

type admin_info map[string]interface{}

func main() {
	server := flag.String("endpoint", defaults.GetDefaults().DefaultAdminListen, "Admin socket endpoint")
	injson := flag.Bool("json", false, "Output in JSON format")
	verbose := flag.Bool("v", false, "Verbose output (includes public keys)")
	flag.Parse()
	args := flag.Args()

	if len(args) == 0 {
		fmt.Println("usage:", os.Args[0], "[-endpoint=proto://server] [-v] [-json] command [key=value] [...]")
		fmt.Println("example:", os.Args[0], "getPeers")
		fmt.Println("example:", os.Args[0], "setTunTap name=auto mtu=1500 tap_mode=false")
		fmt.Println("example:", os.Args[0], "-endpoint=tcp://localhost:9001 getDHT")
		fmt.Println("example:", os.Args[0], "-endpoint=unix:///var/run/ygg.sock getDHT")
		return
	}

	var conn net.Conn
	u, err := url.Parse(*server)
	if err == nil {
		switch strings.ToLower(u.Scheme) {
		case "unix":
			conn, err = net.Dial("unix", (*server)[7:])
		case "tcp":
			conn, err = net.Dial("tcp", u.Host)
		default:
			err = errors.New("protocol not supported")
		}
	} else {
		conn, err = net.Dial("tcp", *server)
	}
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	send := make(admin_info)
	recv := make(admin_info)

	for c, a := range args {
		if c == 0 {
			send["request"] = a
			continue
		}
		tokens := strings.Split(a, "=")
		if i, err := strconv.Atoi(tokens[1]); err == nil {
			send[tokens[0]] = i
		} else {
			switch strings.ToLower(tokens[1]) {
			case "true":
				send[tokens[0]] = true
			case "false":
				send[tokens[0]] = false
			default:
				send[tokens[0]] = tokens[1]
			}
		}
	}

	if err := encoder.Encode(&send); err != nil {
		panic(err)
	}
	if err := decoder.Decode(&recv); err == nil {
		if recv["status"] == "error" {
			if err, ok := recv["error"]; ok {
				fmt.Println("Error:", err)
			} else {
				fmt.Println("Unspecified error occured")
			}
			os.Exit(1)
		}
		if _, ok := recv["request"]; !ok {
			fmt.Println("Missing request in response (malformed response?)")
			return
		}
		if _, ok := recv["response"]; !ok {
			fmt.Println("Missing response body (malformed response?)")
			return
		}
		req := recv["request"].(map[string]interface{})
		res := recv["response"].(map[string]interface{})

		if *injson {
			if json, err := json.MarshalIndent(res, "", "  "); err == nil {
				fmt.Println(string(json))
			}
			os.Exit(0)
		}

		switch strings.ToLower(req["request"].(string)) {
		case "dot":
			fmt.Println(res["dot"])
		case "list", "getpeers", "getswitchpeers", "getdht", "getsessions", "dhtping":
			maxWidths := make(map[string]int)
			var keyOrder []string
			keysOrdered := false

			for _, tlv := range res {
				for slk, slv := range tlv.(map[string]interface{}) {
					if !keysOrdered {
						for k := range slv.(map[string]interface{}) {
							if !*verbose {
								if k == "box_pub_key" || k == "box_sig_key" {
									continue
								}
							}
							keyOrder = append(keyOrder, fmt.Sprint(k))
						}
						sort.Strings(keyOrder)
						keysOrdered = true
					}
					for k, v := range slv.(map[string]interface{}) {
						if len(fmt.Sprint(slk)) > maxWidths["key"] {
							maxWidths["key"] = len(fmt.Sprint(slk))
						}
						if len(fmt.Sprint(v)) > maxWidths[k] {
							maxWidths[k] = len(fmt.Sprint(v))
							if maxWidths[k] < len(k) {
								maxWidths[k] = len(k)
							}
						}
					}
				}

				if len(keyOrder) > 0 {
					fmt.Printf("%-"+fmt.Sprint(maxWidths["key"])+"s  ", "")
					for _, v := range keyOrder {
						fmt.Printf("%-"+fmt.Sprint(maxWidths[v])+"s  ", v)
					}
					fmt.Println()
				}

				for slk, slv := range tlv.(map[string]interface{}) {
					fmt.Printf("%-"+fmt.Sprint(maxWidths["key"])+"s  ", slk)
					for _, k := range keyOrder {
						preformatted := slv.(map[string]interface{})[k]
						var formatted string
						switch k {
						case "bytes_sent", "bytes_recvd":
							formatted = fmt.Sprintf("%d", uint(preformatted.(float64)))
						case "uptime", "last_seen":
							seconds := uint(preformatted.(float64)) % 60
							minutes := uint(preformatted.(float64)/60) % 60
							hours := uint(preformatted.(float64) / 60 / 60)
							formatted = fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
						default:
							formatted = fmt.Sprint(preformatted)
						}
						fmt.Printf("%-"+fmt.Sprint(maxWidths[k])+"s  ", formatted)
					}
					fmt.Println()
				}
			}
		case "gettuntap", "settuntap":
			for k, v := range res {
				fmt.Println("Interface name:", k)
				if mtu, ok := v.(map[string]interface{})["mtu"].(float64); ok {
					fmt.Println("Interface MTU:", mtu)
				}
				if tap_mode, ok := v.(map[string]interface{})["tap_mode"].(bool); ok {
					fmt.Println("TAP mode:", tap_mode)
				}
			}
		case "getself":
			for k, v := range res["self"].(map[string]interface{}) {
				if buildname, ok := v.(map[string]interface{})["build_name"].(string); ok {
					fmt.Println("Build name:", buildname)
				}
				if buildversion, ok := v.(map[string]interface{})["build_version"].(string); ok {
					fmt.Println("Build version:", buildversion)
				}
				fmt.Println("IPv6 address:", k)
				if subnet, ok := v.(map[string]interface{})["subnet"].(string); ok {
					fmt.Println("IPv6 subnet:", subnet)
				}
				if coords, ok := v.(map[string]interface{})["coords"].(string); ok {
					fmt.Println("Coords:", coords)
				}
				if *verbose {
					if boxPubKey, ok := v.(map[string]interface{})["box_pub_key"].(string); ok {
						fmt.Println("Public encryption key:", boxPubKey)
					}
					if boxSigKey, ok := v.(map[string]interface{})["box_sig_key"].(string); ok {
						fmt.Println("Public signing key:", boxSigKey)
					}
				}
			}
		case "getswitchqueues":
			maximumqueuesize := float64(4194304)
			portqueues := make(map[float64]float64)
			portqueuesize := make(map[float64]float64)
			portqueuepackets := make(map[float64]float64)
			v := res["switchqueues"].(map[string]interface{})
			if queuecount, ok := v["queues_count"].(float64); ok {
				fmt.Printf("Active queue count: %d queues\n", uint(queuecount))
			}
			if queuesize, ok := v["queues_size"].(float64); ok {
				fmt.Printf("Active queue size: %d bytes\n", uint(queuesize))
			}
			if highestqueuecount, ok := v["highest_queues_count"].(float64); ok {
				fmt.Printf("Highest queue count: %d queues\n", uint(highestqueuecount))
			}
			if highestqueuesize, ok := v["highest_queues_size"].(float64); ok {
				fmt.Printf("Highest queue size: %d bytes\n", uint(highestqueuesize))
			}
			if m, ok := v["maximum_queues_size"].(float64); ok {
				maximumqueuesize = m
				fmt.Printf("Maximum queue size: %d bytes\n", uint(maximumqueuesize))
			}
			if queues, ok := v["queues"].([]interface{}); ok {
				if len(queues) != 0 {
					fmt.Println("Active queues:")
					for _, v := range queues {
						queueport := v.(map[string]interface{})["queue_port"].(float64)
						queuesize := v.(map[string]interface{})["queue_size"].(float64)
						queuepackets := v.(map[string]interface{})["queue_packets"].(float64)
						queueid := v.(map[string]interface{})["queue_id"].(string)
						portqueues[queueport] += 1
						portqueuesize[queueport] += queuesize
						portqueuepackets[queueport] += queuepackets
						queuesizepercent := (100 / maximumqueuesize) * queuesize
						fmt.Printf("- Switch port %d, Stream ID: %v, size: %d bytes (%d%% full), %d packets\n",
							uint(queueport), []byte(queueid), uint(queuesize),
							uint(queuesizepercent), uint(queuepackets))
					}
				}
			}
			if len(portqueuesize) > 0 && len(portqueuepackets) > 0 {
				fmt.Println("Aggregated statistics by switchport:")
				for k, v := range portqueuesize {
					queuesizepercent := (100 / (portqueues[k] * maximumqueuesize)) * v
					fmt.Printf("- Switch port %d, size: %d bytes (%d%% full), %d packets\n",
						uint(k), uint(v), uint(queuesizepercent), uint(portqueuepackets[k]))
				}
			}
		case "addpeer", "removepeer", "addallowedencryptionpublickey", "removeallowedencryptionpublickey", "addsourcesubnet", "addroute", "removesourcesubnet", "removeroute":
			if _, ok := res["added"]; ok {
				for _, v := range res["added"].([]interface{}) {
					fmt.Println("Added:", fmt.Sprint(v))
				}
			}
			if _, ok := res["not_added"]; ok {
				for _, v := range res["not_added"].([]interface{}) {
					fmt.Println("Not added:", fmt.Sprint(v))
				}
			}
			if _, ok := res["removed"]; ok {
				for _, v := range res["removed"].([]interface{}) {
					fmt.Println("Removed:", fmt.Sprint(v))
				}
			}
			if _, ok := res["not_removed"]; ok {
				for _, v := range res["not_removed"].([]interface{}) {
					fmt.Println("Not removed:", fmt.Sprint(v))
				}
			}
		case "getallowedencryptionpublickeys":
			if _, ok := res["allowed_box_pubs"]; !ok {
				fmt.Println("All connections are allowed")
			} else if res["allowed_box_pubs"] == nil {
				fmt.Println("All connections are allowed")
			} else {
				fmt.Println("Connections are allowed only from the following public box keys:")
				for _, v := range res["allowed_box_pubs"].([]interface{}) {
					fmt.Println("-", v)
				}
			}
		case "getmulticastinterfaces":
			if _, ok := res["multicast_interfaces"]; !ok {
				fmt.Println("No multicast interfaces found")
			} else if res["multicast_interfaces"] == nil {
				fmt.Println("No multicast interfaces found")
			} else {
				fmt.Println("Multicast peer discovery is active on:")
				for _, v := range res["multicast_interfaces"].([]interface{}) {
					fmt.Println("-", v)
				}
			}
		case "getsourcesubnets":
			if _, ok := res["source_subnets"]; !ok {
				fmt.Println("No source subnets found")
			} else if res["source_subnets"] == nil {
				fmt.Println("No source subnets found")
			} else {
				fmt.Println("Source subnets:")
				for _, v := range res["source_subnets"].([]interface{}) {
					fmt.Println("-", v)
				}
			}
		case "getroutes":
			if _, ok := res["routes"]; !ok {
				fmt.Println("No routes found")
			} else if res["routes"] == nil {
				fmt.Println("No routes found")
			} else {
				fmt.Println("Routes:")
				for _, v := range res["routes"].([]interface{}) {
					fmt.Println("-", v)
				}
			}
		default:
			if json, err := json.MarshalIndent(recv["response"], "", "  "); err == nil {
				fmt.Println(string(json))
			}
		}
	}

	if v, ok := recv["status"]; ok && v == "error" {
		os.Exit(1)
	}
	os.Exit(0)
}
