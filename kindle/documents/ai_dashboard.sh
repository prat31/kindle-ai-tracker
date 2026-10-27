#!/bin/sh
# Name: AI Dashboard
# Author: kindle-ai-tracker
# Icon:
# DontUseFBInk
# Detach so the dashboard survives the Kindle UI being stopped.
if command -v setsid >/dev/null 2>&1; then
    setsid /bin/sh /mnt/us/ai-dashboard/start.sh </dev/null >/dev/null 2>&1 &
else
    nohup /bin/sh /mnt/us/ai-dashboard/start.sh </dev/null >/dev/null 2>&1 &
fi
