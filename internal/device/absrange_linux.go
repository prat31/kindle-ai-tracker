package device

import (
	"os"
	"syscall"
	"unsafe"
)

// absRange asks the kernel for an axis' range (EVIOCGABS).
func absRange(f *os.File, code int) (min, max int32, ok bool) {
	var info [6]int32 // struct input_absinfo: value, minimum, maximum, fuzz, flat, resolution
	req := uintptr(2<<30 | uintptr(unsafe.Sizeof(info))<<16 | 'E'<<8 | uintptr(0x40+code))
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(&info[0])))
	if errno != 0 || info[2] <= info[1] {
		return 0, 0, false
	}
	return info[1], info[2], true
}

// grab takes the input device for ourselves (EVIOCGRAB) so the frozen Kindle UI
// underneath doesn't also act on taps. The kernel releases it when we exit.
func grab(f *os.File) error {
	req := uintptr(1<<30 | 4<<16 | 'E'<<8 | 0x90)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, 1); errno != 0 {
		return errno
	}
	return nil
}
