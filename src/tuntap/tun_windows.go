package tuntap

import (
	"bytes"
	"errors"
	"log"
	"net"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	wgtun "golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

// This is to catch Windows platforms

// Configures the TUN adapter with the correct IPv6 address and MTU.
func (tun *TunAdapter) setup(ifname string, addr string, mtu int) error {
	var err error
	err = doAsSystem(func() {
		iface, err := wgtun.CreateTUN(ifname, mtu)
		if err != nil {
			panic(err)
		}
		tun.iface = iface

		if err := tun.setupAddress(addr); err != nil {
			tun.log.Errorln("Failed to set up TUN address:", err)
		}
		if err := tun.setupMTU(getSupportedMTU(mtu)); err != nil {
			tun.log.Errorln("Failed to set up TUN MTU:", err)
		}

		if mtu, err = iface.MTU(); err == nil {
			tun.mtu = mtu
		}
	})
	return err
}

// Sets the MTU of the TAP adapter.
func (tun *TunAdapter) setupMTU(mtu int) error {
	if tun.iface == nil || tun.Name() == "" {
		return errors.New("Can't configure MTU as TUN adapter is not present")
	}
	if intf, ok := tun.iface.(*wgtun.NativeTun); ok {
		luid := winipcfg.LUID(intf.LUID())
		ipfamily, err := luid.IPInterface(windows.AF_INET6)
		if err != nil {
			return err
		}

		ipfamily.NLMTU = uint32(mtu)
		intf.ForceMTU(int(ipfamily.NLMTU))
		ipfamily.UseAutomaticMetric = false
		ipfamily.Metric = 0
		ipfamily.DadTransmits = 0
		ipfamily.RouterDiscoveryBehavior = winipcfg.RouterDiscoveryDisabled

		if err := ipfamily.Set(); err != nil {
			return err
		}
	}

	return nil
}

// Sets the IPv6 address of the TAP adapter.
func (tun *TunAdapter) setupAddress(addr string) error {
	if tun.iface == nil || tun.Name() == "" {
		return errors.New("Can't configure IPv6 address as TUN adapter is not present")
	}
	if intf, ok := tun.iface.(*wgtun.NativeTun); ok {
		if ipaddr, ipnet, err := net.ParseCIDR(addr); err == nil {
			luid := winipcfg.LUID(intf.LUID())
			addresses := append([]net.IPNet{}, net.IPNet{
				IP:   ipaddr,
				Mask: ipnet.Mask,
			})

			err := luid.SetIPAddressesForFamily(windows.AF_INET6, addresses)
			if err == windows.ERROR_OBJECT_ALREADY_EXISTS {
				cleanupAddressesOnDisconnectedInterfaces(windows.AF_INET6, addresses)
				err = luid.SetIPAddressesForFamily(windows.AF_INET6, addresses)
			}
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		return errors.New("unable to get NativeTUN")
	}
	return nil
}

/*
 * doAsSystem
 * SPDX-License-Identifier: LGPL-3.0
 * Copyright (C) 2017-2019 Jason A. Donenfeld <Jason@zx2c4.com>. All Rights Reserved.
 */
func doAsSystem(f func()) error {
	runtime.LockOSThread()
	defer func() {
		windows.RevertToSelf()
		runtime.UnlockOSThread()
	}()
	privileges := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges: [1]windows.LUIDAndAttributes{
			{
				Attributes: windows.SE_PRIVILEGE_ENABLED,
			},
		},
	}
	err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr("SeDebugPrivilege"), &privileges.Privileges[0].Luid)
	if err != nil {
		return err
	}
	err = windows.ImpersonateSelf(windows.SecurityImpersonation)
	if err != nil {
		return err
	}
	var threadToken windows.Token
	err = windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_ADJUST_PRIVILEGES, false, &threadToken)
	if err != nil {
		return err
	}
	defer threadToken.Close()
	err = windows.AdjustTokenPrivileges(threadToken, false, &privileges, uint32(unsafe.Sizeof(privileges)), nil, nil)
	if err != nil {
		return err
	}

	processes, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(processes)

	processEntry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	pid := uint32(0)
	for err = windows.Process32First(processes, &processEntry); err == nil; err = windows.Process32Next(processes, &processEntry) {
		if strings.ToLower(windows.UTF16ToString(processEntry.ExeFile[:])) == "winlogon.exe" {
			pid = processEntry.ProcessID
			break
		}
	}
	if pid == 0 {
		return errors.New("unable to find winlogon.exe process")
	}

	winlogonProcess, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(winlogonProcess)
	var winlogonToken windows.Token
	err = windows.OpenProcessToken(winlogonProcess, windows.TOKEN_IMPERSONATE|windows.TOKEN_DUPLICATE, &winlogonToken)
	if err != nil {
		return err
	}
	defer winlogonToken.Close()
	var duplicatedToken windows.Token
	err = windows.DuplicateTokenEx(winlogonToken, 0, nil, windows.SecurityImpersonation, windows.TokenImpersonation, &duplicatedToken)
	if err != nil {
		return err
	}
	defer duplicatedToken.Close()
	err = windows.SetThreadToken(nil, duplicatedToken)
	if err != nil {
		return err
	}
	f()
	return nil
}

/*
 * cleanupAddressesOnDisconnectedInterfaces
 * SPDX-License-Identifier: MIT
 * Copyright (C) 2019 WireGuard LLC. All Rights Reserved.
 */
func cleanupAddressesOnDisconnectedInterfaces(family winipcfg.AddressFamily, addresses []net.IPNet) {
	if len(addresses) == 0 {
		return
	}
	includedInAddresses := func(a net.IPNet) bool {
		// TODO: this makes the whole algorithm O(n^2). But we can't stick net.IPNet in a Go hashmap. Bummer!
		for _, addr := range addresses {
			ip := addr.IP
			if ip4 := ip.To4(); ip4 != nil {
				ip = ip4
			}
			mA, _ := addr.Mask.Size()
			mB, _ := a.Mask.Size()
			if bytes.Equal(ip, a.IP) && mA == mB {
				return true
			}
		}
		return false
	}
	interfaces, err := winipcfg.GetAdaptersAddresses(family, winipcfg.GAAFlagDefault)
	if err != nil {
		return
	}
	for _, iface := range interfaces {
		if iface.OperStatus == winipcfg.IfOperStatusUp {
			continue
		}
		for address := iface.FirstUnicastAddress; address != nil; address = address.Next {
			ip := address.Address.IP()
			ipnet := net.IPNet{IP: ip, Mask: net.CIDRMask(int(address.OnLinkPrefixLength), 8*len(ip))}
			if includedInAddresses(ipnet) {
				log.Printf("Cleaning up stale address %s from interface ‘%s’", ipnet.String(), iface.FriendlyName())
				iface.LUID.DeleteIPAddress(ipnet)
			}
		}
	}
}
