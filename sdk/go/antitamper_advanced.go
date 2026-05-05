//go:build advanced_antitamper

package license

import (
	"crypto/sha256"
	"encoding/binary"
	"runtime"
	"unsafe"
)

const antitamperFunctionFingerprintBytes = 64

func runtimeFunctionHash(ptr uintptr) uint64 {
	if ptr == 0 {
		return 0
	}

	data := make([]byte, antitamperFunctionFingerprintBytes)
	for i := 0; i < len(data); i++ {
		data[i] = *(*byte)(unsafe.Pointer(ptr + uintptr(i)))
	}

	h := sha256.Sum256(data)
	return binary.LittleEndian.Uint64(h[:8])
}

func detectCommonHooks(ptr uintptr) bool {
	if ptr == 0 {
		return false
	}

	firstByte := *(*byte)(unsafe.Pointer(ptr))
	secondByte := *(*byte)(unsafe.Pointer(ptr + 1))

	if firstByte == 0xE9 || firstByte == 0xEB {
		return true
	}
	if firstByte == 0x48 && secondByte == 0xB8 {
		return true
	}
	return firstByte == 0xCC
}

func detectCurrentFunctionBreakpoint() bool {
	pc, _, _, ok := runtime.Caller(1)
	if !ok || pc == 0 {
		return false
	}
	return *(*byte)(unsafe.Pointer(uintptr(pc))) == 0xCC
}
