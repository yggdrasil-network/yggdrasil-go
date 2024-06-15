//go:build linux
// +build linux

package main

import (
	"net"
	"os"
)

// Notify systemd daemon when start up is completed.
// Required to ensure that dependent services are started only after TUN interface is ready.
//
// One of the following is returned:
// (false, nil) - notification not supported (i.e. `notifySocketEnv` is unset)
// (false, err) - notification supported, but failure happened (e.g. error connecting to `notifySocketEnv` or while sending data)
// (true, nil) - notification supported, data has been sent
//
// Based on `SdNotify` from [`coreos/go-systemd`](https://github.com/coreos/go-systemd/blob/7d375ecc2b092916968b5601f74cca28a8de45dd/daemon/sdnotify.go#L56)
func notifyStartupCompleted() (bool, error) {
	const (
		notifyReady     = "READY=1"
		notifySocketEnv = "NOTIFY_SOCKET"
	)

	socketAddr := &net.UnixAddr{
		Name: os.Getenv(notifySocketEnv),
		Net:  "unixgram",
	}

	// `notifySocketEnv` not set
	if socketAddr.Name == "" {
		return false, nil
	}

	os.Unsetenv(notifySocketEnv)

	conn, err := net.DialUnix(socketAddr.Net, nil, socketAddr)
	// Error connecting to `notifySocketEnv`
	if err != nil {
		return false, err
	}
	defer conn.Close()

	if _, err = conn.Write([]byte(notifyReady)); err != nil {
		return false, err
	}

	return true, nil
}
