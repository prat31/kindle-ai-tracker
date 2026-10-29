package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"sync"
	"time"
)

const logKeep = 24 * time.Hour

// logMu is held while a log line is written and while pruneLog rewrites the file, so no
// line is lost between reading the log and writing back what's kept. While ai-dash runs
// it is the only thing writing to the log (start.sh waits for it).
var logMu sync.Mutex

// logWriter is the log package's output: stderr, which start.sh points at dash.log.
type logWriter struct{ w io.Writer }

func (l logWriter) Write(p []byte) (int, error) {
	logMu.Lock()
	defer logMu.Unlock()
	return l.w.Write(p)
}

// pruneLog drops everything in the log before the first line stamped within keep of now.
// start.sh holds the file open in append mode, so it's cut down in place rather than
// replaced: the shell's later lines still land at the end of the same file.
func pruneLog(path string, keep time.Duration, now time.Time) error {
	logMu.Lock()
	defer logMu.Unlock()
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
	// Write what's kept over the start, then cut off the rest, so the file is never
	// left empty if we're interrupted.
	if _, err := f.WriteAt(b[cut:], 0); err != nil {
		return err
	}
	return f.Truncate(int64(len(b) - cut))
}
