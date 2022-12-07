//go:build windows
// +build windows

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hjson/hjson-go"
	"github.com/webview/webview"

	"github.com/RiV-chain/RiV-mesh/src/admin"
)

var riv_ctrl_path string

func run_command(command string) []byte {
	args := []string{"-json", command}
	cmd := exec.Command(riv_ctrl_path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		//log.Fatalf("cmd.Run() failed with %s\n", err)
		return []byte(err.Error())
	}
	return out
}

func run_command_with_arg(command string, arg string) []byte {
	args := []string{"-json", command, arg}
	cmd := exec.Command(riv_ctrl_path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		//log.Fatalf("command failed: %s\n", riv_ctrl_path+" "+strings.Join(args, " "))
		return []byte(err.Error())
	}
	return out
}
