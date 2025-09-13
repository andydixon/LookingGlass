# Base Ubuntu
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# Install XFCE, VNC server, noVNC, Supervisor
RUN apt-get update && apt-get install -y \
    xfce4 xfce4-goodies \
    novnc websockify \
    x11vnc xvfb xserver-xorg-video-dummy xfonts-base \
    wget curl net-tools supervisor \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -s /bin/bash docker && \
    echo "docker:docker" | chpasswd && adduser docker sudo

# Environment variables
ENV USER=docker \
    PASSWORD=docker \
    DISPLAY=:1 \
    VNC_PORT=5901 \
    NO_VNC_PORT=8080

EXPOSE 8080

# Supervisor config
COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# Startup script
COPY startup.sh /startup.sh
RUN chmod +x /startup.sh

COPY overlay-entrypoint.sh /overlay-entrypoint.sh
RUN chmod +x /overlay-entrypoint.sh
CMD ["/overlay-entrypoint.sh"]
