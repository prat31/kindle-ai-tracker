#!/bin/sh
# KPM launch hook (`kpm launch ai_dashboard`), run inside the package folder. Starts the
# dashboard detached so it keeps running after kpm returns.
if command -v setsid >/dev/null 2>&1; then
    setsid /bin/sh "$(pwd)/start.sh" </dev/null >/dev/null 2>&1 &
else
    nohup /bin/sh "$(pwd)/start.sh" </dev/null >/dev/null 2>&1 &
fi
