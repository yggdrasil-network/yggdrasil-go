package tun

import (
	"errors"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

const TUN_OFFSET_BYTES = 80 // sizeof(virtio_net_hdr)

func (tun *TunAdapter) read() {
	vs := tun.iface.BatchSize()
	bufs := make([][]byte, vs)
	sizes := make([]int, vs)
	for i := range bufs {
		bufs[i] = make([]byte, TUN_OFFSET_BYTES+65535)
	}
	for {
		n, err := tun.iface.Read(bufs, sizes, TUN_OFFSET_BYTES)
		if err != nil {
			if errors.Is(err, wgtun.ErrTooManySegments) {
				tun.log.Debugln("TUN segments dropped: %v", err)
				continue
			}
			tun.log.Errorln("Error reading TUN:", err)
			return
		}
		for i, b := range bufs[:n] {
			if _, err := tun.rwc.Write(b[TUN_OFFSET_BYTES : TUN_OFFSET_BYTES+sizes[i]]); err != nil {
				tun.log.Debugln("Unable to send packet:", err)
			}
		}
	}
}

func (tun *TunAdapter) queue() {
	for {
		p := bufPool.Get().([]byte)[:bufPoolSize]
		n, err := tun.rwc.Read(p)
		if err != nil {
			tun.log.Errorln("Exiting TUN writer due to core read error:", err)
			return
		}
		tun.ch <- p[:n]
	}
}

func (tun *TunAdapter) write() {
	vs := cap(tun.ch)
	bufs := make([][]byte, vs)
	for i := range bufs {
		bufs[i] = make([]byte, TUN_OFFSET_BYTES+65535)
	}
	for {
		n := len(tun.ch)
		if n == 0 {
			n = 1 // Nothing queued up yet, wait for it instead
		}
		for i := 0; i < n; i++ {
			msg := <-tun.ch
			bufs[i] = append(bufs[i][:TUN_OFFSET_BYTES], msg...)
			bufPool.Put(msg) // nolint:staticcheck
		}
		if !tun.isEnabled {
			continue // Nothing to do, the tun isn't enabled
		}
		if _, err := tun.iface.Write(bufs[:n], TUN_OFFSET_BYTES); err != nil {
			tun.Act(nil, func() {
				if !tun.isOpen {
					tun.log.Errorln("TUN iface write error:", err)
				}
			})
		}
	}
}
