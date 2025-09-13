# 📘 Desktop Gateway – Remote Ubuntu XFCE with OverlayFS

This project provides a **web-based Ubuntu XFCE desktop environment** which can be launched on demand in isolated Docker containers.  
Users log in through a web form, and the system spins up a dedicated container for them with **persistent storage** and **per-user OverlayFS root filesystems**.  

Guests can log in too, but their sessions are completely ephemeral and leave no trace once closed.  

The gateway is written in **Go**, proxies all traffic securely through itself, and requires only a single exposed port.  

---

## ✨ Features

- **Web-based login** – HTML form served by the Go gateway.  
- **Per-user configuration** – each user has a `.conf` file controlling their password and persistence.  
- **Persistent installs** – users can install their own software via `apt` or `snap`, and it persists across sessions.  
- **OverlayFS root** – each user’s container runs on a merged root filesystem, layered over a common base.  
- **Guest mode** – ephemeral overlays that disappear when the session ends.  
- **noVNC integration** – XFCE desktop accessible directly in a browser (no client required).  
- **Idle cleanup** – sessions auto-terminate after inactivity.  
- **Proxying** – all VNC traffic is reverse-proxied via the Go gateway, so no container ports are directly exposed.  
- **Systemd service** – runs automatically at boot and restarts on failure.  

---

## 🏗️ Project Structure

```
project/
├── main.go                 # Go gateway source code
├── templates/              # HTML templates
│   ├── login.html
│   └── session.html
├── users/                  # Per-user configs
│   ├── alice.conf
│   └── guest.conf
├── ubuntuBase/             # Base image build context
│   ├── Dockerfile
│   └── build.sh
└── README.md
```

---

## ⚙️ How It Works (Under the Bonnet)

### 1. Base Image (`ubuntuBase`)
- The **Dockerfile** builds an Ubuntu 22.04 image with:  
  - XFCE desktop  
  - x11vnc  
  - noVNC + websockify  
  - supervisord (to run everything together)  
- Once built, we **export the root filesystem** to `/srv/overlays/base` using `docker export`.  
- This exported filesystem becomes the **read-only lowerdir** for OverlayFS.

### 2. OverlayFS Per User
- Each user gets their own directories:
  ```
  /srv/overlays/<username>/
   ├── upper/   (all their writes, e.g. installed packages)
   ├── work/    (required for OverlayFS internals)
   └── merged/  (the mounted overlay, seen as / inside the container)
  ```
- At login, the gateway mounts:
  ```
  mount -t overlay overlay     -o lowerdir=/srv/overlays/base,upperdir=/srv/overlays/<user>/upper,workdir=/srv/overlays/<user>/work     /srv/overlays/<user>/merged
  ```
- Docker then runs a container with `/srv/overlays/<user>/merged` bound to `/`.  
- Any changes made (apt installs, configs, etc.) are written to the user’s `upper/`.  

### 3. Guest Mode
- If a user’s config sets `overlay = ephemeral`, the gateway creates a temporary directory under `/srv/overlays/guest-<random>`.  
- On logout, the container is killed, the overlay is unmounted, and the entire directory is deleted.  
- Nothing persists.  

### 4. Go Gateway
- Handles login, session tracking, and cleanup.  
- Proxies all `/proxy/<sessionid>/*` requests into the relevant container’s noVNC server.  
- Runs a cleanup loop every minute to kill idle sessions.  

### 5. Systemd Service
- The Go gateway runs as a managed service.  
- Ensures it starts on boot and restarts if it fails.  

---

## 🛠️ Installation

### Prerequisites
- Ubuntu 22.04 or later (tested)  
- Docker (with root privileges)  
- Go (to build the gateway)  
- `systemd` (for service management)  

### 1. Clone the Project
```bash
git clone https://example.com/desktop-gateway.git
cd desktop-gateway
```

### 2. Build the Base Image
Go into the `ubuntuBase` folder:

```bash
cd ubuntuBase
./build.sh
```

The `build.sh` does two things:
1. Builds the `ubuntu-xfce-novnc` Docker image from the Dockerfile.  
2. Exports its root filesystem into `/srv/overlays/base` for use as the OverlayFS lowerdir.  

At the end you should have:

```
/srv/overlays/base/
 ├── bin/
 ├── etc/
 ├── usr/
 └── ...
```

### 3. Build the Gateway
Back in the project root:

```bash
go build -o /usr/local/bin/desktop-gateway main.go
```

### 4. Create Directories
Ensure the overlay root exists:

```bash
sudo mkdir -p /srv/overlays/base
sudo mkdir -p /srv/desktop-gateway/{templates,users}
```

Place your HTML templates in `/srv/desktop-gateway/templates/`  
Place user configs in `/srv/desktop-gateway/users/`  

Example `users/alice.conf`:

```ini
[user]
password = secret123
overlay = /srv/overlays/alice
```

Example `users/guest.conf`:

```ini
[user]
password = guest
overlay = ephemeral
```

### 5. Configure Systemd Service
Create `/etc/systemd/system/desktop-gateway.service`:

```ini
[Unit]
Description=Desktop Gateway (noVNC with OverlayFS)
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/desktop-gateway
WorkingDirectory=/srv/desktop-gateway
Restart=on-failure
RestartSec=5
User=root
Group=root

[Install]
WantedBy=multi-user.target
```

### 6. Enable and Start
```bash
sudo systemctl daemon-reload
sudo systemctl enable desktop-gateway
sudo systemctl start desktop-gateway
```

### 7. Access
Open a browser to:

```
http://<server-ip>:8081/
```

Login as `alice / secret123` → persistent XFCE desktop.  
Login as `guest / guest` → ephemeral desktop.  

---

## 🔍 Usage Walkthrough

1. **Alice logs in**:  
   - Gateway reads `/srv/overlays/alice/`.  
   - Mounts overlay at `/srv/overlays/alice/merged`.  
   - Starts container with this as `/`.  
   - Alice sees a full Ubuntu XFCE desktop in her browser.  

2. **Alice installs Firefox**:  
   - `apt install firefox` writes into `/srv/overlays/alice/upper/`.  
   - Next login, Firefox is still there.  

3. **Guest logs in**:  
   - Gateway makes `/srv/overlays/guest-xyz123/`.  
   - Guest uses desktop, installs something.  
   - On logout, the overlay is unmounted and deleted.  
   - Nothing persists.  

---

## 🔒 Security Considerations

- Containers are run with `--privileged` to allow OverlayFS mounts.  
- Only the Go gateway port (8081) should be exposed to the outside world.  
- Recommended: put this behind **Nginx/Traefik** with HTTPS.  
- Consider filesystem quotas for `/srv/overlays` to prevent users consuming too much space.  

---

## 🚀 Future Enhancements / To-Do

- **User management** – provide an admin tool to create/delete users.  
- **Encrypted passwords** – store password hashes in configs instead of plain text.  
- **Multi-image support** – allow different users to start desktops from different base images (e.g. XFCE, MATE, KDE).  
- **Per-user settings** – resolution, idle timeout, default locale.  
- **Quotas** – limit disk usage per user overlay.  
- **TLS support** – native HTTPS inside the gateway without Nginx.  

---

## ✅ Summary

This project creates a secure, web-accessible Ubuntu desktop system where:  

- Each user has their own isolated container.  
- Installations persist thanks to OverlayFS.  
- Guests can log in without leaving data behind.  
- Everything runs under a single gateway process, managed by systemd.  
