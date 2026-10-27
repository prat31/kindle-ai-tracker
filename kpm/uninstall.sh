#!/bin/sh
# KPM uninstall hook (also run with "upgrade" before an upgrade). Removes the library
# entry and KUAL menu this package added, only if they're still ours. KPM deletes the
# package folder itself. /mnt/us/ai-dashboard (settings, logins, history) is kept;
# delete it by hand to remove your logins.

same() { [ -f "$1" ] && [ "$(md5sum <"$1")" = "$(md5sum <"$2")" ]; }

if same /mnt/us/documents/ai_dashboard.sh scriptlets/ai_dashboard.sh; then
    rm -f /mnt/us/documents/ai_dashboard.sh
fi
for f in kual/ai-dashboard/*; do
    name=$(basename "$f")
    if same "/mnt/us/extensions/ai-dashboard/$name" "$f"; then
        rm -f "/mnt/us/extensions/ai-dashboard/$name"
    fi
done
rmdir /mnt/us/extensions/ai-dashboard 2>/dev/null
exit 0
