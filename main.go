package main
/**
LookingGlass - (c) 2024-2026 Andy Dixon <lookingglass@andydixon.com>

This file is part of LookingGlass.

LookingGlass is free software: you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

Foobar is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for more details.

You should have received a copy of the GNU General Public License along with LookingGlass. If not, see <https://www.gnu.org/licenses/>.



**/
// A web gateway that:
// - Provides a login form
// - Starts per-user desktop containers with OverlayFS persistence
// - Proxies noVNC traffic via a single HTTP port
// - Supports ephemeral guest sessions
// - Cleans up idle sessions automatically

import (
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/ini.v1"
)

// Session holds information about a running user desktop.
type Session struct {
	Username      string    // The user this session belongs to
	ContainerName string    // The Docker container name
	OverlayDir    string    // Overlay base path (/srv/overlays/<user>)
	Port          int       // Random port bound for noVNC
	LastActive    time.Time // Timestamp for last activity
	Ephemeral     bool      // Whether this session is guest/ephemeral
}

var (
	userConfDir   = "./users"        // Directory containing <username>.conf
	templatesDir  = "./templates"    // Directory with HTML templates
	baseOverlay   = "/srv/overlays/base" // Extracted base rootfs
	sessions      = make(map[string]Session)
	sessionsMu    sync.Mutex
	sessionExpiry = 10 * time.Minute // Idle timeout
)

func main() {
	// HTTP routes
	http.HandleFunc("/", loginForm)
	http.HandleFunc("/login", login)
	http.HandleFunc("/session/", session)
	http.HandleFunc("/logout/", logout)
	http.HandleFunc("/ping/", ping)
	http.HandleFunc("/proxy/", proxyHandler)

	// Background cleanup goroutine
	go cleanupLoop()

	log.Println("Gateway running on :8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

// renderTemplate loads an HTML template and renders it.
func renderTemplate(w http.ResponseWriter, name string, data any) {
	tmplPath := filepath.Join(templatesDir, name)
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), 500)
		return
	}
	tmpl.Execute(w, data)
}

// loginForm shows the login page.
func loginForm(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "login.html", nil)
}

// login authenticates a user, mounts overlayfs, and starts a desktop container.
func login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", 400)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	confPath := filepath.Join(userConfDir, username+".conf")
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		http.Error(w, "Invalid user", 401)
		return
	}

	cfg, err := ini.Load(confPath)
	if err != nil {
		http.Error(w, "Config error", 500)
		return
	}
	if cfg.Section("user").Key("password").String() != password {
		http.Error(w, "Invalid credentials", 401)
		return
	}
	overlaySetting := cfg.Section("user").Key("overlay").String()

	// Choose overlay directory
	overlayDir := ""
	ephemeral := false
	if overlaySetting == "ephemeral" {
		// Temporary overlay for guest mode
		overlayDir = filepath.Join("/srv/overlays", "guest-"+randSeq(6))
		ephemeral = true
	} else {
		overlayDir = overlaySetting
	}

	upper := filepath.Join(overlayDir, "upper")
	work := filepath.Join(overlayDir, "work")
	merged := filepath.Join(overlayDir, "merged")

	// Ensure overlay dirs exist
	for _, d := range []string{upper, work, merged} {
		if err := os.MkdirAll(d, 0755); err != nil {
			http.Error(w, "Failed to create overlay dirs", 500)
			return
		}
	}

	// Mount OverlayFS: lowerdir=base, upperdir=user, workdir=user, merged=mountpoint
	cmd := exec.Command("mount", "-t", "overlay", "overlay",
		"-o", fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", baseOverlay, upper, work),
		merged)
	if err := cmd.Run(); err != nil {
		http.Error(w, "Failed to mount overlay: "+err.Error(), 500)
		return
	}

	// Build docker run command
	sessionID := randSeq(8)
	port := randomPort()
	containerName := "desktop-" + username + "-" + sessionID

	args := []string{
		"run", "-d", "--rm", "--privileged",
		"-p", fmt.Sprintf("%d:8080", port),
		"--name", containerName,
		"-v", merged + ":/:rshared",
		"ubuntu-xfce-novnc",
	}

	cmd = exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		// Unmount overlay if docker run fails
		exec.Command("umount", "-l", merged).Run()
		http.Error(w, "Failed to start container: "+err.Error(), 500)
		return
	}

	// Save session
	sessionsMu.Lock()
	sessions[sessionID] = Session{
		Username:      username,
		ContainerName: containerName,
		OverlayDir:    overlayDir,
		Port:          port,
		LastActive:    time.Now(),
		Ephemeral:     ephemeral,
	}
	sessionsMu.Unlock()

	// Redirect user to session page
	http.Redirect(w, r, "/session/"+sessionID, 302)
}

// session serves the HTML wrapper page for the VNC session.
func session(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")

	sessionsMu.Lock()
	s, ok := sessions[sessionID]
	if ok {
		s.LastActive = time.Now()
		sessions[sessionID] = s
	}
	sessionsMu.Unlock()

	if !ok {
		http.Error(w, "Session not found", 404)
		return
	}

	renderTemplate(w, "session.html", map[string]any{
		"SessionID": sessionID,
	})
}

// proxyHandler forwards requests into the noVNC server inside the container.
func proxyHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/proxy/"), "/", 2)
	if len(parts) < 2 {
		http.Error(w, "Bad proxy path", 400)
		return
	}
	sessionID, rest := parts[0], parts[1]

	sessionsMu.Lock()
	s, ok := sessions[sessionID]
	if ok {
		s.LastActive = time.Now()
		sessions[sessionID] = s
	}
	sessionsMu.Unlock()
	if !ok {
		http.Error(w, "Session not found", 404)
		return
	}

	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", s.Port))
	proxy := httputil.NewSingleHostReverseProxy(target)
	r.URL.Path = "/" + rest
	r.Host = target.Host
	proxy.ServeHTTP(w, r)
}

// ping updates session activity timestamp (called by JS heartbeat).
func ping(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/ping/")
	sessionsMu.Lock()
	if s, ok := sessions[sessionID]; ok {
		s.LastActive = time.Now()
		sessions[sessionID] = s
	}
	sessionsMu.Unlock()
	w.WriteHeader(200)
}

// logout stops a session explicitly.
func logout(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/logout/")
	stopSession(sessionID)
	http.Redirect(w, r, "/", 302)
}

// cleanupLoop checks sessions every minute and kills idle ones.
func cleanupLoop() {
	for {
		time.Sleep(1 * time.Minute)
		sessionsMu.Lock()
		for id, s := range sessions {
			if time.Since(s.LastActive) > sessionExpiry {
				log.Printf("Session %s idle > %v, killing...", id, sessionExpiry)
				stopSession(id)
			}
		}
		sessionsMu.Unlock()
	}
}

// stopSession kills the container, unmounts overlay, and cleans up.
func stopSession(sessionID string) {
	sessionsMu.Lock()
	if s, ok := sessions[sessionID]; ok {
		// Kill container
		exec.Command("docker", "rm", "-f", s.ContainerName).Run()

		// Unmount overlay
		merged := filepath.Join(s.OverlayDir, "merged")
		exec.Command("umount", "-l", merged).Run()

		// If guest mode, remove dirs
		if s.Ephemeral {
			os.RemoveAll(s.OverlayDir)
		}

		delete(sessions, sessionID)
	}
	sessionsMu.Unlock()
}

// --- Utility functions ---

var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

// randSeq returns a random alphanumeric string.
func randSeq(n int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// randomPort returns a random TCP port in range 10000–15000.
func randomPort() int {
	return 10000 + rand.Intn(5000)
}
