package device

import (
	"bufio"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Suspend on current firmware belongs to powerd: writing /sys/power/state directly is
// refused (EBUSY). So we work the way KOReader does: "press" the power button through
// powerd, set the wake alarm when it announces readyToSuspend (the only moment it
// accepts one), and look at its state after wakeupFromSuspend to tell an alarm from
// someone pressing the button.

// PowerEvent is something powerd announced.
type PowerEvent string

const (
	GoingToScreenSaver PowerEvent = "goingToScreenSaver"
	OutOfScreenSaver   PowerEvent = "outOfScreenSaver"
	ReadyToSuspend     PowerEvent = "readyToSuspend"
	WakeupFromSuspend  PowerEvent = "wakeupFromSuspend"
)

// WatchPowerd follows powerd's events with lipc-wait-event. Off the Kindle the
// channel never fires.
func WatchPowerd() <-chan PowerEvent {
	ch := make(chan PowerEvent, 16)
	bin, err := exec.LookPath("lipc-wait-event")
	if err != nil {
		return ch
	}
	events := strings.Join([]string{string(GoingToScreenSaver), string(OutOfScreenSaver),
		string(ReadyToSuspend), string(WakeupFromSuspend)}, ",")
	go func() {
		for {
			cmd := exec.Command(bin, "-m", "com.lab126.powerd", events)
			out, err := cmd.StdoutPipe()
			if err == nil && cmd.Start() == nil {
				sc := bufio.NewScanner(out)
				for sc.Scan() {
					name, _, _ := strings.Cut(strings.TrimSpace(sc.Text()), " ")
					log.Printf("powerd: %s", sc.Text())
					ch <- PowerEvent(name)
				}
				cmd.Wait()
			}
			time.Sleep(5 * time.Second)
		}
	}()
	return ch
}

// PowerdState is powerd's state: "active", "screenSaver", "readyToSuspend",
// "suspended", or "" off the Kindle.
func PowerdState() string {
	out, err := exec.Command("lipc-get-prop", "com.lab126.powerd", "state").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// PressPowerButton makes powerd act as if the power button was pressed.
func PressPowerButton() {
	exec.Command("lipc-set-prop", "-i", "com.lab126.powerd", "powerButton", "1").Run()
}

// SetRtcWakeup schedules a wake-up; powerd only accepts it during readyToSuspend.
func SetRtcWakeup(after time.Duration) {
	secs := max(int64(after.Seconds()), 30)
	if err := exec.Command("lipc-set-prop", "-i", "com.lab126.powerd", "rtcWakeup", strconv.FormatInt(secs, 10)).Run(); err != nil {
		log.Printf("rtcWakeup: %v", err)
	}
}

// PreventScreenSaver stops (or allows) powerd from putting the Kindle to sleep by itself.
func PreventScreenSaver(on bool) {
	v := "0"
	if on {
		v = "1"
	}
	exec.Command("lipc-set-prop", "com.lab126.powerd", "preventScreenSaver", v).Run()
}

// WaitForWifi turns Wi-Fi on and waits until it's connected, or the timeout passes.
func WaitForWifi(timeout time.Duration) bool {
	WakeWifi()
	deadline := time.Now().Add(timeout)
	for {
		out, err := exec.Command("lipc-get-prop", "com.lab126.wifid", "cmState").Output()
		if err != nil {
			return WifiIP() != "" // not a Kindle
		}
		if strings.TrimSpace(string(out)) == "CONNECTED" && WifiIP() != "" {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Second)
	}
}

// SetFrontlight sets the front light brightness (0 = off).
func SetFrontlight(level int) {
	exec.Command("lipc-set-prop", "com.lab126.powerd", "flIntensity", strconv.Itoa(level)).Run()
}

// Frontlight is the current front light level, or -1 if unknown.
func Frontlight() int {
	return lipcInt("flIntensity", -1)
}

// FrontlightMax is the brightest front light level, or 0 off the Kindle.
func FrontlightMax() int {
	if _, err := exec.LookPath("lipc-get-prop"); err != nil {
		return 0
	}
	return lipcInt("flMaxIntensity", 24) // 24 on a Paperwhite 5
}

func lipcInt(prop string, fallback int) int {
	out, err := exec.Command("lipc-get-prop", "com.lab126.powerd", prop).Output()
	if err != nil {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return fallback
	}
	return n
}
