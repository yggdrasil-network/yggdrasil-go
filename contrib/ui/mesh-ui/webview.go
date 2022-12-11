package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hjson/hjson-go"
	"github.com/webview/webview"

	"github.com/RiV-chain/RiV-mesh/src/admin"
	"github.com/docopt/docopt-go"
)

var usage = `Graphical interface for RiV mesh.

Usage:
  mesh-ui [<index>] [-c]
  mesh-ui -h | --help
  mesh-ui -v | --version

Options:
  <index>       Index file name [default: index.html].
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
	/*1. Create ~/.riv-mesh folder if not existing
	 *2. Create ~/.riv-mesh/mesh.conf if not existing
	 *3. If the file exists read Peers.
	 *3.1 Invoke add peers for each record
	 */
	mesh_folder := ".riv-mesh"
	mesh_conf := "mesh.conf"
	user_home := get_user_home_path()
	mesh_settings_folder := filepath.Join(user_home, mesh_folder)
	err := os.MkdirAll(mesh_settings_folder, os.ModePerm)
	if err != nil {
		fmt.Printf("Unable to create folder: %v", err)
	}
	mesh_settings_path := filepath.Join(user_home, mesh_folder, mesh_conf)
	riv_ctrl_path = get_ctl_path()
	if _, err := os.Stat(mesh_settings_path); os.IsNotExist(err) {
		err := ioutil.WriteFile(mesh_settings_path, []byte(""), 0750)
		if err != nil {
			fmt.Printf("Unable to write file: %v", err)
		}
	} else {
		//read peers from mesh.conf
		conf, _ := ioutil.ReadFile(mesh_settings_path)
		var dat map[string]interface{}
		if err := hjson.Unmarshal(conf, &dat); err != nil {
			fmt.Printf("Unable to parse mesh.conf file: %v", err)
		} else {
			if dat["Peers"] != nil {
				peers := dat["Peers"].([]interface{})
				remove_peers()
				for _, u := range peers {
					log.Printf("Unmarshaled: %v", u.(string))
					add_peers(u.(string))
				}
			} else {
				fmt.Printf("Warning: Peers array not loaded from mesh.conf file")
			}
		}
	}

	if confui.IndexHtml == "" {
		confui.IndexHtml = "index.html"
	}
	confui.IndexHtml, err = filepath.Abs(confui.IndexHtml)
	if err != nil {
		panic(errors.New("Index file not found: " + err.Error()))
	}

	if stat, err := os.Stat(confui.IndexHtml); err != nil {
		panic(errors.New("Index file not found or permissians denied: " + err.Error()))
	} else if stat.IsDir() {
		panic(errors.New(fmt.Sprintf("Index file %v not found", confui.IndexHtml)))
	}

	w.Bind("onLoad", func() {
		log.Println("page loaded")
		go run(w)
	})
	w.Bind("savePeers", func(peer_list string) {
		//log.Println("peers saved ", peer_list)
		var peers []string
		_ = json.Unmarshal([]byte(peer_list), &peers)
		log.Printf("Unmarshaled: %v", peers)
		remove_peers()
		for _, u := range peers {
			log.Printf("Unmarshaled: %v", u)
			add_peers(u)
		}
		//add peers to ~/mesh.conf
		dat := make(map[string]interface{})
		dat["Peers"] = peers
		bs, _ := hjson.Marshal(dat)
		e := ioutil.WriteFile(mesh_settings_path, bs, 0750)
		if e != nil {
			fmt.Printf("Unable to write file: %v", e)
		}
	})
	w.Bind("ping", func(peer_list string) {
		go ping(w, peer_list)
	})
	log.Printf("Opening: %v", confui.IndexHtml)
	w.Navigate(confui.IndexHtml)
	w.Run()
}

func ping(w webview.WebView, peer_list string) {
	var peers []string
	_ = json.Unmarshal([]byte(peer_list), &peers)
	log.Printf("Unmarshaled: %v", peers)
	for _, u := range peers {
		log.Printf("Unmarshaled: %v", u)
		ping_time := check(u)
		log.Printf("ping: %d", ping_time)
		setPingValue(w, u, strconv.FormatInt(ping_time, 10))
	}
}

func check(peer string) int64 {
	u, e := url.Parse(peer)
	if e != nil {
		return -1
	}
	t := time.Now()
	_, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		return -1
	}
	d := time.Since(t)
	return d.Milliseconds()
}

func get_user_home_path() string {
	path, exists := os.LookupEnv("HOME")
	if exists {
		return path
	} else {
		return ""
	}
}

func get_ctl_path() string {
	if runtime.GOOS == "windows" {
		program_path := "programfiles"
		path, exists := os.LookupEnv(program_path)
		if exists {
			fmt.Println("Program path: %s", path)
			ctl_path := fmt.Sprintf("%s\\RiV-mesh\\meshctl.exe", path)
			return ctl_path
		} else {
			fmt.Println("could not find Program Files path")
			return ""
		}
	} else {
		ctl_path := fmt.Sprintf("/usr/local/bin/meshctl")
		return ctl_path
	}
}

func run(w webview.WebView) {
	if len(riv_ctrl_path) > 0 {
		get_self(w)
		get_peers(w)
	}
	_ = time.AfterFunc(10*time.Second, func() {
		run(w)
	})
}

func add_peers(uri string) {
	run_command_with_arg("addpeers", "uri="+uri)
}

func remove_peers() {
	run_command("removepeers")
}

func get_self(w webview.WebView) {

	res := &admin.GetSelfResponse{}
	out := run_command("getSelf")
	if err := json.Unmarshal(out, &res); err != nil {
		go setFieldValue(w, "ipv6", err.Error())
		return
	}
	//found ipv6
	fmt.Printf("IPv6: %s\n", res.IPAddress)
	go setFieldValue(w, "ipv6", res.IPAddress)
	go setFieldValue(w, "pub_key", res.PublicKey)
	go setFieldValue(w, "priv_key", res.PrivateKey)
	go setFieldValue(w, "version", fmt.Sprintf("v%v/%v", res.BuildVersion, uiVersion))
	//found subnet
	fmt.Printf("Subnet: %s\n", res.Subnet)
	go setFieldValue(w, "subnet", res.Subnet)
	out = run_command("getPeers")
	//go setFieldValue(w, "peers", string(out))
}

func get_peers(w webview.WebView) {

	res := &admin.GetPeersResponse{}
	out := run_command("getPeers")
	if err := json.Unmarshal(out, &res); err != nil {
		go setFieldValue(w, "peers", err.Error())
		return
	}

	var m []string
	for _, s := range res.Peers {
		m = append(m, s.Remote)
	}
	for k := range m {
		// Loop
		fmt.Println(k)
	}
	inner_html := strings.Join(m[:], "<br>")
	strings.Join(m[:], "<br>")
	go setFieldValue(w, "peers", inner_html)
}

func setFieldValue(p webview.WebView, id string, value string) {
	p.Dispatch(func() {
		p.Eval("setFieldValue('" + id + "','" + value + "');")
	})
}

func setPingValue(p webview.WebView, peer string, value string) {
	p.Dispatch(func() {
		p.Eval("setPingValue('" + peer + "','" + value + "');")
	})
}
