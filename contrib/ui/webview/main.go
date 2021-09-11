package main

import (
	"github.com/webview/webview"
	"path/filepath"
	"io/ioutil"
	"net/url"
	"runtime"
	"strings"
	"os/exec"
	"log"
	"os"
	"fmt"	
)

func main() {
	debug := true
	w := webview.New(debug)
	defer w.Destroy()
	w.SetTitle("RiV-mesh")
	w.SetSize(450, 410, webview.HintNone)
	path, err := filepath.Abs(filepath.Dir(os.Args[0]))
    if err != nil {
            log.Fatal(err)
    }
    log.Println(path)
	w.Bind("onLoad", func() {
			log.Println("page loaded")
			go run(w)
	})
	dat, err := ioutil.ReadFile(path+"/index.html")
	w.Navigate("data:text/html,"+url.QueryEscape(string(dat)))
	w.Run()
}

func run(w webview.WebView){
	if runtime.GOOS == "windows" {
		program_path := "programfiles"
		path, exists := os.LookupEnv(program_path)
		if exists {
			fmt.Println("Program path: %s", path)
			riv_ctrl_path := fmt.Sprintf("%s\\RiV-mesh\\meshctl.exe", path)
			run_command(w, riv_ctrl_path, "getSelf")
		} else {
			fmt.Println("could not find Program Files path")
		}
	} else {
		riv_ctrl_path := fmt.Sprintf("meshctl")
		run_command(w, riv_ctrl_path, "getSelf")
	}
}

func run_command(w webview.WebView, riv_ctrl_path string, command string){
	
	cmd := exec.Command(riv_ctrl_path, command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("cmd.Run() failed with %s\n", err)
		return
	}
	lines := strings.Split(string(out), "\n")
	m := make(map[string]string)
	for i, s := range lines {
		p := strings.SplitN(s, ":", 2)
		if len(p)>1 {
			m[p[0]]=strings.TrimSpace(p[1])
			fmt.Println(i)
		}
	}
	if val, ok := m["IPv6 address"]; ok {
		//found ipv6
		fmt.Printf("IPv6: %s\n", val)
		go setFieldValue(w, "ipv6", val)
	}
	if val, ok := m["IPv6 subnet"]; ok {
		//found subnet
		fmt.Printf("Subnet: %s\n", val)
		go setFieldValue(w, "subnet", val)
	}
	
}

func setFieldValue(p webview.WebView, id string, value string) {
	p.Dispatch(func() {
		p.Eval("setFieldValue('"+id+"','"+value+"');")
	})
}
