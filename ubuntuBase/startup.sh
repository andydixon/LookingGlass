#!/bin/bash
# Set VNC password
mkdir -p /home/docker/.vnc
echo "$PASSWORD" | vncpasswd -f > /home/docker/.vnc/passwd
chmod 600 /home/docker/.vnc/passwd
chown -R docker:docker /home/docker/.vnc

exec "$@"
