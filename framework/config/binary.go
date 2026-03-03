/*
Maddy Mail Server - Composable all-in-one email server.
Copyright © 2019-2020 Max Mazurov <fox.cpp@disroot.org>, Maddy Mail Server contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

// Package config provides runtime path resolution for Madmail binaries.
//
// # Camouflage / Stealth Deployment
//
// In restricted network environments (e.g. Iran, Russia), server operators may
// need to disguise the Madmail service so that automated government scans of
// running processes, systemd units, and /etc/ directories do not reveal that a
// mail server is running.
//
// This is achieved by renaming the binary before installation:
//
//	cp maddy /usr/local/bin/sysmond   # looks like a system monitor daemon
//	sudo ./sysmond install --simple --ip 1.2.3.4
//
// All paths, usernames, and systemd unit names are then derived from the
// binary name at runtime — so they all consistently show "sysmond":
//
//	ps aux          → sysmond --config /etc/sysmond/sysmond.conf run ...
//	systemctl       → sysmond.service     (not madmail.service)
//	/etc/           → /etc/sysmond/       (not /etc/maddy/)
//	/var/lib/       → /var/lib/sysmond/   (not /var/lib/maddy/)
//	user account    → sysmond             (not maddy)
//
// Alternatively, use the --binary-name flag during install without renaming:
//
//	sudo ./maddy install --simple --ip 1.2.3.4 --binary-name sysmond
//
// See install --help for full options.
package config

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	binaryNameOnce sync.Once
	binaryName     string
)

// BinaryName returns the name of the running executable (without directory or
// extension). Symlinks are resolved so that "sysmond -> maddy" returns "sysmond".
// Falls back to "maddy" on error.
//
// This is the foundation of the camouflage system: every derived path
// (config file, state dir, service name, username) is based on this value.
func BinaryName() string {
	binaryNameOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			binaryName = "maddy"
			return
		}
		// Follow symlinks so "sysmond -> maddy" resolves to the real binary.
		real, err := filepath.EvalSymlinks(exe)
		if err != nil {
			real = exe
		}
		name := filepath.Base(real)
		if name == "" || name == "." {
			binaryName = "maddy"
			return
		}
		binaryName = name
	})
	return binaryName
}

// ServiceName returns the systemd service name derived from the binary name.
// A binary named "sysmond" produces "sysmond.service" — not "maddy.service".
func ServiceName() string {
	return BinaryName() + ".service"
}

// ServiceNameAt returns a systemd instance service name variant.
// A binary named "sysmond" with suffix "@" produces "sysmond@.service".
func ServiceNameAt(suffix string) string {
	return BinaryName() + suffix + ".service"
}

// DefaultConfigDir returns "/etc/<binaryname>".
// For a disguised binary "sysmond" this is "/etc/sysmond".
func DefaultConfigDir() string {
	return "/etc/" + BinaryName()
}

// DefaultStateDirPath returns "/var/lib/<binaryname>".
// For a disguised binary "sysmond" this is "/var/lib/sysmond".
func DefaultStateDirPath() string {
	return "/var/lib/" + BinaryName()
}

// DefaultRuntimeDirPath returns "/run/<binaryname>".
func DefaultRuntimeDirPath() string {
	return "/run/" + BinaryName()
}

// DefaultLibexecDirPath returns "/usr/lib/<binaryname>".
func DefaultLibexecDirPath() string {
	return "/usr/lib/" + BinaryName()
}

// EffectiveConfigDir is set by the root package's init() to return the
// compile-time ConfigDirectory value (which may be overridden via -X linker
// flag). It may return an empty string meaning "use binary-name-based default".
var EffectiveConfigDir func() string

// effectiveConfigDirStr resolves the config directory: compile-time override
// first, then falls back to /etc/<binaryname>.
func effectiveConfigDirStr() string {
	if EffectiveConfigDir != nil {
		if d := EffectiveConfigDir(); d != "" {
			return d
		}
	}
	return DefaultConfigDir()
}

// ConfigFile returns the absolute path to the main configuration file.
//
// Examples:
//
//	binary "maddy"   → /etc/maddy/maddy.conf
//	binary "sysmond" → /etc/sysmond/sysmond.conf   (camouflaged)
//	-X ConfigDirectory=/etc/custom + binary "sysmond" → /etc/custom/sysmond.conf
func ConfigFile() string {
	return filepath.Join(effectiveConfigDirStr(), BinaryName()+".conf")
}

// UserName returns the system user name derived from the binary name.
// A disguised binary "sysmond" will run as system user "sysmond".
func UserName() string {
	return BinaryName()
}
