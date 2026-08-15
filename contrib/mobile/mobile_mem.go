package mobile

import "runtime/debug"

func setMemLimitIfPossible() {
	debug.SetMemoryLimit(1024 * 1024 * 40)
}
