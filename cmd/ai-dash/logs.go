package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"time"
)

const logKeep = 24 * time.Hour

// pruneLog drops everything in the log before the first line stamped within keep of now.
// start.sh holds the file open in append mode, so it's cut down in place rather than
// replaced: the shell's later lines still land at the end of the same file.
func pruneLog(path string, keep time.Duration, now time.Time) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	cut, since := 0, now.Add(-keep)
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		// Lines from the log package start with "2006/01/02 15:04:05"; the shell's don't,
		// and go with whichever stamped lines come before them.
		if len(line) >= 19 {
			if t, err := time.ParseInLocation("2006/01/02 15:04:05", string(line[:19]), time.Local); err == nil && !t.Before(since) {
				break
			}
		}
		cut += len(line) + 1
	}
	if cut == 0 {
		return nil
	}
	cut = min(cut, len(b))
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err = f.WriteAt(b[cut:], 0)
	return err
}
