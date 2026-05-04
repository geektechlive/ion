#!/bin/bash
# Build IonRemote and install to a connected iPhone.
# Usage: bash commands/install.command [--device DEVICE_ID]
#
# Requires a connected iPhone (USB or Wi-Fi paired).
# Uses xcodebuild to build + install in one shot via
# `build install-on-device`.

set -euo pipefail

cd "$(dirname "$0")/.."

TEAM_ID="P6UU9VHF7D"
SCHEME="IonRemote"
PROJECT="IonRemote.xcodeproj"
BUNDLE_ID="com.sprague.ion.mobile"
CONFIGURATION="Debug"
DEVICE_ID=""

# ── Parse args ──

while [[ $# -gt 0 ]]; do
  case "$1" in
    --device)
      DEVICE_ID="$2"
      shift 2
      ;;
    --release)
      CONFIGURATION="Release"
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: bash commands/install.command [--device DEVICE_ID] [--release]"
      exit 1
      ;;
  esac
done

# ── Find connected device ──

if [[ -z "$DEVICE_ID" ]]; then
  echo "==> Detecting connected iPhone..."

  # Prefer devicectl (CoreDevice) — it reports availability and provides
  # the UUID that xcodebuild actually understands on modern Xcode.
  # Fall back to xctrace only when devicectl is unavailable.
  DEVICECTL_TMP=$(mktemp /tmp/devicectl.XXXXXX.json)
  xcrun devicectl list devices --json-output "$DEVICECTL_TMP" >/dev/null 2>&1 || true

  if [[ -s "$DEVICECTL_TMP" ]]; then
    # Find the first physical iPhone from devicectl JSON output
    DEVICE_RESULT=$(python3 -c "
import json, sys
data = json.load(open('$DEVICECTL_TMP'))
devices = data.get('result', {}).get('devices', [])
for d in devices:
    hw = d.get('hardwareProperties', {})
    conn = d.get('connectionProperties', {})
    props = d.get('deviceProperties', {})
    # Only physical iPhones
    if hw.get('reality') != 'physical':
        continue
    if hw.get('deviceType', '') != 'iPhone':
        continue
    udid = d.get('identifier', '')
    name = props.get('name', hw.get('marketingName', 'iPhone'))
    tunnel = conn.get('tunnelState', 'unavailable')
    available = 'available' if tunnel not in ['unavailable'] else 'unavailable'
    print(f'{available}|{udid}|{name}')
    break
" 2>/dev/null || true)
    rm -f "$DEVICECTL_TMP"

    if [[ -n "$DEVICE_RESULT" ]]; then
      DEV_STATE=$(echo "$DEVICE_RESULT" | cut -d'|' -f1)
      DEVICE_ID=$(echo "$DEVICE_RESULT" | cut -d'|' -f2)
      DEV_NAME=$(echo "$DEVICE_RESULT" | cut -d'|' -f3)
      echo "  Found: $DEV_NAME ($DEVICE_ID)"

      if [[ "$DEV_STATE" != "available" ]]; then
        echo
        echo "✗ Device is connected but not available (tunnel: unavailable)."
        echo
        echo "  Try these steps:"
        echo "    1. Unlock the phone and keep it unlocked"
        echo "    2. Unplug USB and plug back in"
        echo "    3. Tap 'Trust' if prompted on the phone"
        echo "    4. Open Xcode.app and wait for device preparation"
        echo "    5. Run: xcrun devicectl list devices | grep iPhone"
        echo "       Wait until it shows 'available', then retry."
        exit 1
      fi
    fi
  else
    rm -f "$DEVICECTL_TMP"
  fi

  # Fallback: xctrace (for older Xcode or when devicectl has no results)
  if [[ -z "$DEVICE_ID" ]]; then
    DEVICE_LINE=$(xcrun xctrace list devices 2>/dev/null \
      | grep -v "Simulator" \
      | grep -v "^==" \
      | grep -v "^$" \
      | grep -vE "^$(scutil --get ComputerName 2>/dev/null || hostname -s)" \
      | head -1)

    if [[ -z "$DEVICE_LINE" ]]; then
      echo "✗ No connected iPhone found."
      echo
      echo "  Connect an iPhone via USB or ensure Wi-Fi pairing is active."
      echo "  To list devices: xcrun devicectl list devices"
      exit 1
    fi

    DEVICE_ID=$(echo "$DEVICE_LINE" | grep -oE '[0-9A-Fa-f-]{20,}' | tail -1)

    if [[ -z "$DEVICE_ID" ]]; then
      echo "✗ Could not parse device ID from: $DEVICE_LINE"
      exit 1
    fi

    DEVICE_NAME=$(echo "$DEVICE_LINE" | sed 's/ (.*//')
    echo "  Found: $DEVICE_NAME ($DEVICE_ID)"
  fi
fi

DESTINATION="id=$DEVICE_ID"

# ── Build + Install ──

echo
echo "═══ Building $SCHEME ($CONFIGURATION) ═══"
echo

xcodebuild \
  -project "$PROJECT" \
  -scheme "$SCHEME" \
  -configuration "$CONFIGURATION" \
  -destination "$DESTINATION" \
  -allowProvisioningUpdates \
  DEVELOPMENT_TEAM="$TEAM_ID" \
  build 2>&1 | tail -5

BUILD_EXIT=${PIPESTATUS[0]}

if [[ $BUILD_EXIT -ne 0 ]]; then
  echo
  echo "✗ Build failed."
  exit 1
fi

echo
echo "═══ Installing to device ═══"
echo

# Find the most recently built .app in DerivedData.
# Multiple DerivedData directories may exist (e.g. from Xcode and
# command-line builds). We pick the newest by modification time to
# ensure we install the binary we just built, not a stale one.
find_newest_app() {
  find ~/Library/Developer/Xcode/DerivedData \
    -path "*/$SCHEME-*/$CONFIGURATION-iphoneos/$SCHEME.app" \
    -maxdepth 5 \
    -type d \
    2>/dev/null \
    | while read -r app_dir; do
        # Use the binary's mod-time as the sort key (epoch seconds)
        binary="$app_dir/$SCHEME"
        if [[ -f "$binary" ]]; then
          echo "$(stat -f '%m' "$binary") $app_dir"
        fi
      done \
    | sort -rn \
    | head -1 \
    | cut -d' ' -f2-
}

APP_PATH=$(find_newest_app)

if [[ -z "$APP_PATH" ]]; then
  echo "✗ Could not find built .app bundle in DerivedData."
  echo "  Expected: DerivedData/*/$CONFIGURATION-iphoneos/$SCHEME.app"
  exit 1
fi

echo "  App: $APP_PATH"

# Use ios-deploy if available (faster, launches app), otherwise devicectl
if command -v ios-deploy &>/dev/null; then
  echo "  Using ios-deploy..."
  ios-deploy --id "$DEVICE_ID" --bundle "$APP_PATH" --no-wifi 2>&1 || {
    echo "  ios-deploy failed, falling back to devicectl..."
    xcrun devicectl device install app --device "$DEVICE_ID" "$APP_PATH" 2>&1
  }
else
  echo "  Using devicectl..."
  xcrun devicectl device install app --device "$DEVICE_ID" "$APP_PATH" 2>&1
fi

echo
echo "═══ IonRemote installed ═══"
echo "  Device: $DEVICE_ID"
echo "  Config: $CONFIGURATION"
echo "  Bundle: $BUNDLE_ID"
