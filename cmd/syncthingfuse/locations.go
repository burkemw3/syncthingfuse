package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/syncthing/syncthing/lib/osutil"
)

type locationEnum string

// Use strings as keys to make printout and serialization of the locations map
// more meaningful.
const (
	locConfigFile    locationEnum = "config"
	locCertFile                   = "certFile"
	locKeyFile                    = "keyFile"
	locHTTPSCertFile              = "httpsCertFile"
	locHTTPSKeyFile               = "httpsKeyFile"
	locDatabase                   = "database"
	locLogFile                    = "logFile"
	locCsrfTokens                 = "csrfTokens"
	locPanicLog                   = "panicLog"
	locAuditLog                   = "auditLog"
	locGUIAssets                  = "GUIAssets"
	locDefFolder                  = "defFolder"
)

// Platform dependent directories
var baseDirs = map[string]string{
	"config": defaultConfigDir(), // Overridden by -home flag
	"home":   homeDir(),          // User's home directory, *not* -home flag
}

// Use the variables from baseDirs here
var locations = map[locationEnum]string{
	locConfigFile:    "${config}/config.xml",
	locCertFile:      "${config}/cert.pem",
	locKeyFile:       "${config}/key.pem",
	locHTTPSCertFile: "${config}/https-cert.pem",
	locHTTPSKeyFile:  "${config}/https-key.pem",
	locDatabase:      "${config}/index-v0.11.0.db",
	locLogFile:       "${config}/syncthing.log", // -logfile on Windows
	locCsrfTokens:    "${config}/csrftokens.txt",
	locPanicLog:      "${config}/panic-${timestamp}.log",
	locAuditLog:      "${config}/audit-${timestamp}.log",
	locGUIAssets:     "${config}/gui",
	locDefFolder:     "${home}/Sync",
}

// expandLocations replaces the variables in the location map with actual
// directory locations.
func expandLocations() error {
	for key, dir := range locations {
		for varName, value := range baseDirs {
			dir = strings.Replace(dir, "${"+varName+"}", value, -1)
		}
		var err error
		dir, err = osutil.ExpandTilde(dir)
		if err != nil {
			return err
		}
		locations[key] = dir
	}
	return nil
}

// defaultConfigDir returns the default configuration directory, as figured
// out by various the environment variables present on each platform, or dies
// trying.
func defaultConfigDir() string {
	switch runtime.GOOS {
	case "darwin":
		dir, err := osutil.ExpandTilde("~/Library/Application Support/SyncthingFUSE")
		if err != nil {
			l.Fatalln(err)
		}
		return dir
	case "linux":
		if xdgCfg := os.Getenv("XDG_CONFIG_HOME"); xdgCfg != "" {
			return filepath.Join(xdgCfg, "syncthing")
		}
		dir, err := osutil.ExpandTilde("~/.config/syncthingfuse")
		if err != nil {
			l.Fatalln(err)
		}
		return dir

	default:
		l.Fatalln("Only OS X and Linux supported right now!")
	}

	return "nil"
}

// homeDir returns the user's home directory, or dies trying.
func homeDir() string {
	home, err := osutil.ExpandTilde("~")
	if err != nil {
		l.Fatalln(err)
	}
	return home
}
