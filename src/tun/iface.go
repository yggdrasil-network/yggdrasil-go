package tun

const TUN_OFFSET_BYTES = 80 // sizeof(virtio_net_hdr)
const TUN_MAX_VECTOR = 16

func (tun *TunAdapter) idealBatchSize() int {
	if b := tun.iface.BatchSize(); b <= TUN_MAX_VECTOR {
		return b
	}
	return TUN_MAX_VECTOR
}

func (tun *TunAdapter) read() {
	vs := tun.idealBatchSize()
	bufs := make([][]byte, vs)
	sizes := make([]int, vs)
	for i := range bufs {
		bufs[i] = make([]byte, TUN_OFFSET_BYTES+65535)
	}
	for {
		n, err := tun.iface.Read(bufs, sizes, TUN_OFFSET_BYTES)
		if err != nil {
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

func (tun *TunAdapter) write() {
	vs := tun.idealBatchSize()
	bufs := make([][]byte, vs)
	sizes := make([]int, vs)
	for i := range bufs {
		bufs[i] = make([]byte, TUN_OFFSET_BYTES+65535)
	}
	for {
		n, err := tun.rwc.ReadMany(bufs, sizes, TUN_OFFSET_BYTES)
		if err != nil {
			tun.log.Errorln("Exiting TUN writer due to core read error:", err)
			return
		}
		if !tun.isEnabled {
			continue // Nothing to do, the tun isn't enabled
		}
		if _, err = tun.iface.Write(bufs[:n], TUN_OFFSET_BYTES); err != nil {
			tun.Act(nil, func() {
				if !tun.isOpen {
					tun.log.Errorln("TUN iface write error:", err)
				}
			})
		}
	}
}
