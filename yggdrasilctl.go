package main

import "flag"
import "fmt"
import "strings"
import "net"
import "encoding/json"
import "strconv"
import "os"

type admin_info map[string]interface{}

func main() {
	server := flag.String("endpoint", "localhost:9001", "Admin socket endpoint")
	flag.Parse()
	args := flag.Args()

	conn, err := net.Dial("tcp", *server)
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
    req := recv["request"].(map[string]interface{})
    res := recv["response"].(map[string]interface{})
		switch req["request"] {
		case "dot":
			fmt.Println(res["dot"])
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
