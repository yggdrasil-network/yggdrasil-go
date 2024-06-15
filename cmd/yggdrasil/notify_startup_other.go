//go:build !linux
// +build !linux

package main

func notifyStartupCompleted() (bool, error) {
	return false, nil
}
