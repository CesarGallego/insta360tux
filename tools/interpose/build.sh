#!/bin/sh
# Construye el interceptor y prepara una copia re-firmada ad hoc de la app en work/.
# No toca /Applications. Uso: tools/interpose/build.sh [--resign]
set -e
cd "$(dirname "$0")/../.."
mkdir -p work/logs
cc -arch arm64 -dynamiclib -O1 -o work/xulog.dylib tools/interpose/xulog.c -framework CoreFoundation -framework IOKit
cc -O1 -o work/mark tools/interpose/mark.c
cc -O1 -o work/xu tools/interpose/xu.c -I/opt/homebrew/include -L/opt/homebrew/lib -lusb-1.0
if [ "$1" = "--resign" ]; then
  APP=work/LinkController.app
  [ -d "$APP" ] || cp -R "/Applications/Insta360 Link Controller.app" "$APP"
  rm -f "$APP/Contents/embedded.provisionprofile"
  # Entitlements restringidos (application-identifier, team, system-extension) exigen
  # perfil de aprovisionamiento y AMFI mataria el proceso: solo se mantienen los no restringidos.
  cat > work/ent.plist <<'PL'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>com.apple.security.device.camera</key><true/>
<key>com.apple.security.device.audio-input</key><true/>
</dict></plist>
PL
  # Sin hardened runtime (sin --options runtime): DYLD_INSERT_LIBRARIES permitido.
  codesign --force --deep -s - --entitlements work/ent.plist "$APP"
fi
