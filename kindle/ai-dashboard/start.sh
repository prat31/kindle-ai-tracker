#!/bin/sh
# Runs on the Kindle, started by the library scriptlet, the KUAL menu or `kpm launch`.
#
# The program lives next to this script: /mnt/us/ai-dashboard for a manual install, or
# KPM's package folder. Settings, logins and history always live in /mnt/us/ai-dashboard
# so they survive KPM upgrades.
#
# The Kindle UI keeps running underneath the dashboard; we only freeze it, the way
# KOReader does by default on current firmware:
#   - pillow (status bar & overlays) disabled, the statusbar job stopped
#   - the window manager (awesome) SIGSTOPped so nothing gets redrawn over us
#   - ai-dash grabs the touchscreen so taps don't reach the library underneath
# To save battery the CPU runs in powersave mode and ai-dash turns the front light off
# (both put back on exit), and puts the Kindle to sleep with Wi-Fi off whenever it isn't
# being used, waking it with an alarm for each update (Wi-Fi is put back on exit too).
# Exiting undoes all of that in a second or two, also when this script is killed (except
# with SIGKILL), so the Kindle never stays unable to sleep by itself. Fully stopping lab126_gui and starting it
# again (what this script used to do) can leave FW 5.19 on a blank white screen.

CODE_DIR=$(cd "$(dirname "$0")" && pwd)
DIR=/mnt/us/ai-dashboard
mkdir -p "$DIR"
LOG="$DIR/dash.log"
PIDFILE=/tmp/ai-dash.pid
FBDUMP=/var/tmp/ai-dash-fb.dump

rm -f "$LOG.old" # ai-dash keeps the log to the last 24 hours; older versions rotated it
exec >>"$LOG" 2>&1
echo "=== start $(date)"

if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    echo "already running"
    exit 0
fi
echo $$ >"$PIDFILE"
echo "running from $CODE_DIR"

if [ ! -f "$DIR/config.json" ] && [ -f "$CODE_DIR/config.example.json" ]; then
    cp "$CODE_DIR/config.example.json" "$DIR/config.json"
fi

# Let the library finish closing the scriptlet before we freeze the UI.
sleep 1

# What powerd offers, written once, to look for settings that save more battery.
if [ ! -s "$DIR/powerd-props.txt" ] && command -v lipc-probe >/dev/null 2>&1; then
    lipc-probe -v com.lab126.powerd >"$DIR/powerd-props.txt" 2>&1
fi

lipc-set-prop com.lab126.powerd preventScreenSaver 1 # ai-dash suspends the Kindle itself
WIRELESS=$(lipc-get-prop com.lab126.cmd wirelessEnable 2>/dev/null)
lipc-set-prop com.lab126.cmd wirelessEnable 1 # ai-dash turns it off while the Kindle sleeps

FRONTLIGHT=$(lipc-get-prop com.lab126.powerd flIntensity 2>/dev/null)
GOVERNOR=/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor
OLD_GOVERNOR=$(cat "$GOVERNOR" 2>/dev/null)
if [ -n "$OLD_GOVERNOR" ] && grep -q powersave "${GOVERNOR%/*}/scaling_available_governors" 2>/dev/null; then
    echo powersave >"$GOVERNOR"
fi

cat /dev/fb0 >"$FBDUMP" 2>/dev/null
lipc-set-prop com.lab126.pillow disableEnablePillow disable
STATUSBAR_STOPPED=no
if [ -f /etc/upstart/statusbar.conf ] && stop statusbar; then
    STATUSBAR_STOPPED=yes
fi
killall -STOP awesome

restore() {
    echo "restoring the Kindle UI"
    [ -n "$FRONTLIGHT" ] && lipc-set-prop com.lab126.powerd flIntensity "$FRONTLIGHT"
    lipc-set-prop com.lab126.cmd wirelessEnable "${WIRELESS:-1}"
    [ -n "$OLD_GOVERNOR" ] && echo "$OLD_GOVERNOR" >"$GOVERNOR"
    killall -CONT awesome
    [ "$STATUSBAR_STOPPED" = yes ] && start statusbar
    if [ -f "$FBDUMP" ]; then
        cat "$FBDUMP" >/dev/fb0
        rm -f "$FBDUMP"
    fi
    lipc-set-prop com.lab126.pillow disableEnablePillow enable
    lipc-set-prop com.lab126.appmgrd start app://com.lab126.booklet.home
    lipc-set-prop com.lab126.powerd preventScreenSaver 0
    rm -f "$PIDFILE"
    echo "=== stopped $(date)"
}

CHILD=
killed() {
    echo "killed; stopping ai-dash"
    if [ -n "$CHILD" ]; then
        kill "$CHILD" 2>/dev/null
        wait "$CHILD" 2>/dev/null
    fi
    restore
    exit 1
}
trap killed TERM INT HUP

cd "$DIR" || exit 1
crashes=0
while :; do
    started=$(date +%s)
    # In the background, so a signal reaches the trap without waiting for ai-dash to end.
    "$CODE_DIR/ai-dash" -dir "$DIR" run &
    CHILD=$!
    wait "$CHILD"
    rc=$?
    CHILD=
    echo "ai-dash exited rc=$rc at $(date)"
    [ "$rc" -eq 0 ] && break
    # Give up only on crashes in quick succession, not a few spread over weeks.
    [ $(($(date +%s) - started)) -ge 600 ] && crashes=0
    crashes=$((crashes + 1))
    if [ "$crashes" -ge 5 ]; then
        /mnt/us/libkh/bin/fbink -q -c -f -m -y 20 "AI dashboard keeps crashing - see ai-dashboard/dash.log"
        sleep 10
        break
    fi
    sleep 10
done

restore
