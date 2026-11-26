package guard

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// The kernel's process information call, which libproc wraps: `ps` reads the
// same, but is setuid, and a sandboxed session may not run it. The call
// answers for the caller's own processes under the sandbox.
const (
	sysProcInfo = 336
	// callListPIDs lists process identifiers by a criterion; callRusage reads
	// one process's resource usage.
	callListPIDs = 1
	callRusage   = 9
	// listByGroup selects the processes of one process group.
	listByGroup = 2
	// rusageV0 is the first layout of the resource usage record, which every
	// later one extends: ninety-six bytes, the physical footprint at seventy-two.
	rusageV0        = 0
	rusageSize      = 96
	footprintOffset = 72
	// pidSize is the size of one process identifier in a listing.
	pidSize = 4
	// firstListing is the room for a group's first listing; a listing that
	// fills its buffer is read again in twice the room.
	firstListing = 64
)

func (g processGroup) members() ([]int, error) {
	for room := firstListing; ; room *= 2 {
		buffer := make([]int32, room)

		// #nosec G103 -- the kernel writes process identifiers into the buffer, and no more than its size.
		written, _, errno := syscall.Syscall6(sysProcInfo, callListPIDs, listByGroup, uintptr(g.pgid), 0,
			uintptr(unsafe.Pointer(&buffer[0])), uintptr(room*pidSize))
		if errno != 0 {
			return nil, fmt.Errorf("guard: listing process group %d: %w", g.pgid, errno)
		}

		count := int(written) / pidSize
		if count == room {
			continue
		}

		pids := make([]int, 0, count)

		for _, pid := range buffer[:count] {
			if pid > 0 {
				pids = append(pids, int(pid))
			}
		}

		return pids, nil
	}
}

func (processGroup) footprint(pid int) (uint64, error) {
	var record [rusageSize]byte

	// #nosec G103 -- the kernel writes one record of the layout asked for, which the buffer holds.
	_, _, errno := syscall.Syscall6(sysProcInfo, callRusage, uintptr(pid), rusageV0, 0,
		uintptr(unsafe.Pointer(&record[0])), 0)
	if errors.Is(errno, syscall.ESRCH) {
		return 0, errGone
	}

	if errno != 0 {
		return 0, fmt.Errorf("guard: reading process %d: %w", pid, errno)
	}

	return binary.LittleEndian.Uint64(record[footprintOffset:]), nil
}
