// +build android

package mobile

import "log"

type MobileLogger struct{}

func (nsl MobileLogger) Write(p []byte) (n int, err error) {
	log.Println(string(p))
	return len(p), nil
}
