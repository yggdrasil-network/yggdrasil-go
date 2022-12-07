//go:build windows
// +build windows

package main

import (
	"os/exec"
	"syscall"
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
