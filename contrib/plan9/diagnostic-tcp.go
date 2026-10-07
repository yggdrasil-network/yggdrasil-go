// ygg-diagnostic-tcp checks TCP and, optionally, HTTP over a Yggdrasil address.
// It uses only the Go standard library so it can be cross-compiled for Plan 9.
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 3 && len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: ygg-diagnostic-tcp YGG_IPV6 PORT [http]")
		os.Exit(2)
	}
	mode := "tcp"
	if len(os.Args) == 4 {
		mode = os.Args[3]
	}
	if mode != "tcp" && mode != "http" {
		fmt.Fprintln(os.Stderr, "mode must be tcp or http")
		os.Exit(2)
	}
	ip := net.ParseIP(os.Args[1])
	if ip == nil || ip.To4() != nil {
		fmt.Fprintln(os.Stderr, "YGG_IPV6 must be a numeric IPv6 address")
		os.Exit(2)
	}
	addr := net.JoinHostPort(ip.String(), os.Args[2])
	failures := 0
	for attempt := 1; attempt <= 5; attempt++ {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, 4*time.Second)
		if err != nil {
			fmt.Printf("TCP_FAIL attempt=%d elapsed=%s error=%v\n", attempt, time.Since(start).Round(time.Millisecond), err)
			failures++
			time.Sleep(300 * time.Millisecond)
			continue
		}
		fmt.Printf("TCP_OK attempt=%d elapsed=%s\n", attempt, time.Since(start).Round(time.Millisecond))
		if mode == "http" {
			conn.SetDeadline(time.Now().Add(4 * time.Second))
			_, err = fmt.Fprintf(conn, "GET / HTTP/1.0\r\nHost: [%s]\r\n\r\n", ip.String())
			if err == nil {
				var line string
				line, err = bufio.NewReader(conn).ReadString('\n')
				if err == nil && strings.HasPrefix(line, "HTTP/") {
					fmt.Printf("HTTP_OK attempt=%d status=%s\n", attempt, strings.TrimSpace(line))
				} else if err == nil {
					fmt.Printf("HTTP_FAIL attempt=%d unexpected_response=%q\n", attempt, strings.TrimSpace(line))
					failures++
				}
			}
			if err != nil {
				fmt.Printf("HTTP_FAIL attempt=%d error=%v\n", attempt, err)
				failures++
			}
		}
		conn.Close()
		time.Sleep(300 * time.Millisecond)
	}
	if failures != 0 {
		fmt.Printf("RESULT failures=%d\n", failures)
		os.Exit(1)
	}
	fmt.Println("RESULT failures=0")
}
