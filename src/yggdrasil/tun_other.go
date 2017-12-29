// +build !linux

package yggdrasil

// This is to catch unsupported platforms
// If your platform supports tun devices, you could try configuring it manually

func (tun *tunDevice) setupAddress(addr string) error {
  tun.core.log.Println("Platform not supported, you must set the address of", tun.iface.Name(), "to", addr)
  return nil
}

