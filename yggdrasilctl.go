package main

import "errors"
import "flag"
import "fmt"
import "strings"
import "net"
import "net/url"
import "sort"
import "encoding/json"
import "strconv"
import "os"

type admin_info map[string]interface{}

func main() {
	server := flag.String("endpoint", "tcp://localhost:9001", "Admin socket endpoint")
	injson := flag.Bool("json", false, "Output in JSON format")
	flag.Parse()
	args := flag.Args()

	if len(args) == 0 {
		fmt.Println("usage:", os.Args[0], "[-endpoint=proto://server] [-json] command [key=value] [...]")
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
			switch tokens[1] {
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

		switch req["request"] {
		case "dot":
			fmt.Println(res["dot"])
		case "help", "getPeers", "getSwitchPeers", "getDHT", "getSessions":
			maxWidths := make(map[string]int)
			var keyOrder []string
			keysOrdered := false

			for _, tlv := range res {
				for slk, slv := range tlv.(map[string]interface{}) {
					if !keysOrdered {
						for k := range slv.(map[string]interface{}) {
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
		case "getTunTap", "setTunTap":
			for k, v := range res {
				fmt.Println("Interface name:", k)
				if mtu, ok := v.(map[string]interface{})["mtu"].(float64); ok {
					fmt.Println("Interface MTU:", mtu)
				}
				if tap_mode, ok := v.(map[string]interface{})["tap_mode"].(bool); ok {
					fmt.Println("TAP mode:", tap_mode)
				}
			}
		case "getSelf":
			for k, v := range res["self"].(map[string]interface{}) {
				fmt.Println("IPv6 address:", k)
				if subnet, ok := v.(map[string]interface{})["subnet"].(string); ok {
					fmt.Println("IPv6 subnet:", subnet)
				}
				if coords, ok := v.(map[string]interface{})["coords"].(string); ok {
					fmt.Println("Coords:", coords)
				}
			}
		case "addPeer", "removePeer", "addAllowedEncryptionPublicKey", "removeAllowedEncryptionPublicKey":
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
		case "getAllowedEncryptionPublicKeys":
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
		case "getMulticastInterfaces":
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
