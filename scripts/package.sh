#!/bin/sh
# Builds release artifacts from build/ai-dash (run `make kindle` first):
#   build/dist/kindle-ai-dashboard-<version>.zip   unzip onto the Kindle drive (manual / KUAL)
#   build/kpm-repo/                                a KPM repository: manifest.json + .kpkg
set -eu

cd "$(dirname "$0")/.."
VERSION=$(cat VERSION)
BIN=build/ai-dash
[ -f "$BIN" ] || { echo "missing $BIN, run make kindle" >&2; exit 1; }
export COPYFILE_DISABLE=1 # no macOS ._ files in archives

ID=ai_dashboard
PLATFORM=kindlehf
DESCRIPTION="Desk display for your AI plan usage: Claude, ChatGPT, GitHub Copilot, OpenRouter and Z.ai."
MAJOR=$(echo "$VERSION" | cut -d. -f1)
MINOR=$(echo "$VERSION" | cut -d. -f2)
PATCH=$(echo "$VERSION" | cut -d. -f3)

# KUAL extension with the version filled in.
kual() {
    mkdir -p "$1"
    cp kindle/extensions/ai-dashboard/menu.json kindle/extensions/ai-dashboard/run.sh "$1/"
    sed "s|<version>VERSION</version>|<version>$VERSION</version>|" kindle/extensions/ai-dashboard/config.xml >"$1/config.xml"
}

# The library shows the scriptlet with the cover from its "# Icon:" line. It's inlined
# as a data URI, since the scriptlet can't know where the app was installed.
SCRIPTLET=build/ai_dashboard.sh
sed "s|^# Icon:.*|# Icon: data:image/png;base64,$(base64 <kindle/cover.png | tr -d '\n')|" \
    kindle/documents/ai_dashboard.sh >"$SCRIPTLET"

# --- release zip: mirrors the Kindle drive ----------------------------------
rm -rf build/dist && mkdir -p build/dist/root/ai-dashboard build/dist/root/documents
cp "$BIN" kindle/ai-dashboard/start.sh kindle/ai-dashboard/config.example.json build/dist/root/ai-dashboard/
cp "$SCRIPTLET" build/dist/root/documents/
kual build/dist/root/extensions/ai-dashboard
ZIP="build/dist/kindle-ai-dashboard-$VERSION.zip"
(cd build/dist/root && zip -qr -X "../$(basename "$ZIP")" .)
echo "wrote $ZIP"

# --- KPM package ------------------------------------------------------------
PKG=build/kpm-pkg
rm -rf "$PKG" && mkdir -p "$PKG/scriptlets"
cp "$BIN" kindle/ai-dashboard/start.sh kindle/ai-dashboard/config.example.json kpm/install.sh kpm/uninstall.sh kpm/launch.sh "$PKG/"
cp "$SCRIPTLET" "$PKG/scriptlets/"
kual "$PKG/kual/ai-dashboard"
cat >"$PKG/manifest.json" <<JSON
{
  "manifest_version": 2,
  "id": "$ID",
  "name": "AI Dashboard",
  "author": "prat31",
  "description": "$DESCRIPTION",
  "version": [$MAJOR, $MINOR, $PATCH],
  "dependencies": [],
  "supported_platforms": ["$PLATFORM"]
}
JSON

REPO=build/kpm-repo
ARTIFACT="packages/$ID/artifacts/${ID}_${VERSION}_${PLATFORM}.kpkg"
rm -rf "$REPO" && mkdir -p "$REPO/$(dirname "$ARTIFACT")"
# KPM wants the files at the root of the archive (manifest.json, not ./manifest.json).
(cd "$PKG" && tar -czf "../../$REPO/$ARTIFACT" $(ls -A))
cat >"$REPO/manifest.json" <<JSON
{
  "manifest_version": 2,
  "id": "kindle-ai-tracker",
  "name": "Kindle AI Tracker",
  "description": "AI Dashboard for jailbroken Kindles",
  "packages": {
    "$ID": {
      "name": "AI Dashboard",
      "author": "prat31",
      "description": "$DESCRIPTION",
      "artifacts": [
        {
          "url": "$ARTIFACT",
          "version": [$MAJOR, $MINOR, $PATCH],
          "dependencies": [],
          "supported_platforms": ["$PLATFORM"]
        }
      ]
    }
  }
}
JSON
echo "wrote $REPO/ ($ARTIFACT)"
