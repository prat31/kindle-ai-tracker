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
# (both put back on exit) and suspends the Kindle between updates.
# Exiting undoes all of that in a second or two. Fully stopping lab126_gui and starting it
# again (what this script used to do) can leave FW 5.19 on a blank white screen.

CODE_DIR=$(cd "$(dirname "$0")" && pwd)
DIR=/mnt/us/ai-dashboard
mkdir -p "$DIR"
LOG="$DIR/dash.log"
PIDFILE=/tmp/ai-dash.pid
FBDUMP=/var/tmp/ai-dash-fb.dump

if [ -f "$LOG" ] && [ "$(wc -c <"$LOG")" -gt 1000000 ]; then
    mv "$LOG" "$LOG.old"
fi
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

lipc-set-prop com.lab126.powerd preventScreenSaver 1 # ai-dash suspends the Kindle itself
lipc-set-prop com.lab126.cmd wirelessEnable 1

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

cd "$DIR" || exit 1
crashes=0
while :; do
    "$CODE_DIR/ai-dash" -dir "$DIR" run
    rc=$?
    echo "ai-dash exited rc=$rc at $(date)"
    [ "$rc" -eq 0 ] && break
    crashes=$((crashes + 1))
    if [ "$crashes" -ge 5 ]; then
        /mnt/us/libkh/bin/fbink -q -c -f -m -y 20 "AI dashboard keeps crashing - see ai-dashboard/dash.log"
        sleep 10
        break
    fi
    sleep 10
done

echo "restoring the Kindle UI"
[ -n "$FRONTLIGHT" ] && lipc-set-prop com.lab126.powerd flIntensity "$FRONTLIGHT"
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
