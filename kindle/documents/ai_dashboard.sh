#!/bin/sh
# Name: AI Dashboard
# Author: kindle-ai-tracker
# Icon:
# DontUseFBInk
# Starts the dashboard whether it was installed by hand (/mnt/us/ai-dashboard) or with KPM.
# Detached, so it survives the Kindle UI being frozen.
if [ -f /mnt/us/ai-dashboard/start.sh ]; then
    if command -v setsid >/dev/null 2>&1; then
        setsid /bin/sh /mnt/us/ai-dashboard/start.sh </dev/null >/dev/null 2>&1 &
    else
        nohup /bin/sh /mnt/us/ai-dashboard/start.sh </dev/null >/dev/null 2>&1 &
    fi
else
    /var/local/kmc/bin/kpm launch ai_dashboard
fi
