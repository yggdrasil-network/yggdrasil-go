//go:build !android && !ios && !darwin
// +build !android,!ios,!darwin

package mobile

import "fmt"

type MobileLogger struct {
}

func (nsl MobileLogger) Write(p []byte) (n int, err error) {
	fmt.Print(string(p))
	return len(p), nil
}
