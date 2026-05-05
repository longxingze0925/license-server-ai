package handler

import "hash/crc32"

func isMachineInRollout(machineID string, percentage int) bool {
	if percentage >= 100 {
		return true
	}
	if percentage <= 0 {
		return false
	}
	if machineID == "" {
		return true
	}
	return int(crc32.ChecksumIEEE([]byte(machineID))%100) < percentage
}
