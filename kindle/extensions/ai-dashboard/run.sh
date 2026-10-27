#!/bin/sh
# KUAL "Start dashboard": wait for KUAL to close, then start the dashboard the same way
# the library scriptlet does (a manual install first, otherwise the KPM package).
(
    sleep 2
    if [ -f /mnt/us/ai-dashboard/start.sh ]; then
        exec /bin/sh /mnt/us/ai-dashboard/start.sh
    fi
    exec /var/local/kmc/bin/kpm launch ai_dashboard
) </dev/null >/dev/null 2>&1 &
