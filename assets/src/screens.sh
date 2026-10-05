#!/bin/sh
# Regenerates the README screenshots from a running dev panel with demo data.
#   BOT=<bot id for the console and deploys shots> sh assets/src/screens.sh
set -eu
cd "$(dirname "$0")/../.."
out=assets/screens
tmp=$(mktemp -d)
mkdir -p "$out"
shot() { LOGIN=1 PRESET="$2" node assets/src/screenshot.mjs "http://localhost:8080$1" "$tmp/$3.png" 1440 900 "${4:-3000}" >/dev/null; }

clean="localStorage.removeItem('mechon-theme');localStorage.removeItem('mechon-setup-open')"
rail="$clean;localStorage.setItem('mechon-sidebar','collapsed')"

shot / "$clean;localStorage.setItem('mechon-sidebar','open')" overview 3500
shot "/bots/$BOT" "$rail" console 26000
shot "/bots/$BOT?tab=deploys" "$rail" deploys 3500
shot "/bots?new=1" "$rail" new-bot 3500
shot /bots "$rail;localStorage.setItem('mechon-theme','light')" bots-light 3500

for f in overview console deploys new-bot bots-light; do
	python3 assets/src/frame.py "$tmp/$f.png" "$out/$f.png"
done
cp "$tmp/overview.png" assets/src/shot.png
node assets/src/render.mjs >/dev/null
rm -f assets/src/shot.png
rm -rf "$tmp"
ls -la "$out"
