//go:build !linux

package device

import "os"

func absRange(f *os.File, code int) (min, max int32, ok bool) { return 0, 0, false }

func grab(f *os.File) error { return nil }
