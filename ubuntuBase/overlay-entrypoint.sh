#!/bin/bash
# Entrypoint for containers that use OverlayFS as a root substitute.
# This script chroots into /mnt/overlay (if mounted) and starts supervisord there.

OVERLAY=/mnt/overlay

if [ -d "$OVERLAY" ]; then
  echo "[*] Switching to overlay root at $OVERLAY"
  # Copy startup files into overlay if missing
  if [ ! -f "$OVERLAY/startup.sh" ]; then
    cp /startup.sh "$OVERLAY/startup.sh"
    chmod +x "$OVERLAY/startup.sh"
  fi
  if [ ! -f "$OVERLAY/etc/supervisor/conf.d/supervisord.conf" ]; then
    mkdir -p "$OVERLAY/etc/supervisor/conf.d"
    cp /etc/supervisor/conf.d/supervisord.conf "$OVERLAY/etc/supervisor/conf.d/"
  fi

mkdir -p "$OVERLAY/tmp/.X11-unix"
chmod 1777 "$OVERLAY/tmp" "$OVERLAY/tmp/.X11-unix"


  # Enter overlay and launch supervisord (manages Xfce, x11vnc, novnc)
  exec chroot "$OVERLAY" /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
else
  echo "[!] Overlay root not found, running fallback supervisord"
  exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
fi
