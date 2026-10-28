package device

import (
	"bufio"
	"log"
	"os/exec"
	"strings"
	"time"
)

// PowerEvent is a change in powerd's screensaver state.
type PowerEvent int

const (
	GoingToSleep PowerEvent = iota // powerd is showing the screensaver and will suspend
	WokeUp                         // back out of the screensaver
)

// WatchPowerd follows powerd's screensaver events with lipc-wait-event. Off the Kindle
// the channel never fires.
func WatchPowerd() <-chan PowerEvent {
	ch := make(chan PowerEvent, 4)
	bin, err := exec.LookPath("lipc-wait-event")
	if err != nil {
		return ch
	}
	go func() {
		for {
			cmd := exec.Command(bin, "-m", "com.lab126.powerd", "goingToScreenSaver,outOfScreenSaver")
			out, err := cmd.StdoutPipe()
			if err == nil && cmd.Start() == nil {
				sc := bufio.NewScanner(out)
				for sc.Scan() {
					line := sc.Text()
					log.Printf("powerd: %s", line)
					switch {
					case strings.HasPrefix(line, "goingToScreenSaver"):
						ch <- GoingToSleep
					case strings.HasPrefix(line, "outOfScreenSaver"):
						ch <- WokeUp
					}
				}
				cmd.Wait()
			}
			time.Sleep(5 * time.Second)
		}
	}()
	return ch
}

// PreventScreenSaver stops (or allows) the Kindle from going to sleep by itself.
func PreventScreenSaver(on bool) {
	v := "0"
	if on {
		v = "1"
	}
	exec.Command("lipc-set-prop", "com.lab126.powerd", "preventScreenSaver", v).Run()
}

// PressPowerButton asks powerd to act as if the power button was pressed.
func PressPowerButton() {
	exec.Command("lipc-set-prop", "com.lab126.powerd", "powerButton", "1").Run()
}
