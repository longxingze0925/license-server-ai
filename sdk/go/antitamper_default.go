//go:build !advanced_antitamper

package license

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"runtime"
)

func runtimeFunctionHash(ptr uintptr) uint64 {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%x", runtime.GOARCH, ptr)))
	return binary.LittleEndian.Uint64(h[:8])
}

func detectCommonHooks(_ uintptr) bool {
	return false
}

func detectCurrentFunctionBreakpoint() bool {
	return false
}
