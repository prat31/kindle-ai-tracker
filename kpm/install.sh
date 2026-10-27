#!/bin/sh
# KPM install hook. Runs inside the unpacked package folder (also after an upgrade).
# The package folder holds the program; settings, logins and history live in
# /mnt/us/ai-dashboard, which KPM never touches.

cp scriptlets/ai_dashboard.sh /mnt/us/documents/ || exit 1

# A KUAL menu entry too, if KUAL is installed.
if [ -d /mnt/us/extensions ]; then
    mkdir -p /mnt/us/extensions/ai-dashboard
    cp kual/ai-dashboard/* /mnt/us/extensions/ai-dashboard/
fi

mkdir -p /mnt/us/ai-dashboard
echo "AI Dashboard installed: open it from the library or KUAL"
