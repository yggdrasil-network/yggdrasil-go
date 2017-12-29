package yggdrasil

// The linux platform specific tun parts
// It depends on iproute2 being installed to set things on the tun device

import "fmt"
import "os/exec"
import "strings"

func (tun *tunDevice) setupAddress(addr string) error {
  // Set address
  cmd := exec.Command("ip", "-f", "inet6",
                      "addr", "add", addr,
                      "dev", tun.iface.Name())
  tun.core.log.Printf("ip command: %v", strings.Join(cmd.Args, " "))
  output, err := cmd.CombinedOutput()
  if err != nil {
    tun.core.log.Printf("Linux ip failed: %v.", err)
    tun.core.log.Println(string(output))
    return err
  }
  // Set MTU and bring device up
  cmd = exec.Command("ip", "link", "set",
                     "dev", tun.iface.Name(),
                     "mtu", fmt.Sprintf("%d", tun.mtu),
                     "up")
  tun.core.log.Printf("ip command: %v", strings.Join(cmd.Args, " "))
  output, err = cmd.CombinedOutput()
  if err != nil {
    tun.core.log.Printf("Linux ip failed: %v.", err)
    tun.core.log.Println(string(output))
    return err
  }
  return nil
}

