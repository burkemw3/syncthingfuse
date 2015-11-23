package main

import (
	"fmt"
	"net"
	"os"

	"github.com/burkemw3/syncthingfuse/lib/config"
	"github.com/syncthing/syncthing/lib/osutil"
)

func getConfiguration() *config.Wrapper {
	cfgFile := locations[locConfigFile]

	// Load the configuration file, if it exists. If it does not, create a template.
	if info, err := os.Stat(cfgFile); err == nil {
		if !info.Mode().IsRegular() {
			l.Fatalln("Config file is not a file?")
		}
		cfg, err = config.Load(cfgFile, myID)
		if err != nil {
			l.Fatalln("Configuration:", err)
		}
	} else {
		l.Infoln("No config file; starting with empty defaults")
		myName, _ := os.Hostname()
		newCfg := defaultConfig(myName)
		cfg = config.Wrap(cfgFile, newCfg)
		cfg.Save()
		l.Infof("Edit %s to taste or use the GUI\n", cfgFile)
	}

	return cfg
}

func ensureDir(dir string, mode int) {
	fi, err := os.Stat(dir)
	if os.IsNotExist(err) {
		err := osutil.MkdirAll(dir, 0700)
		if err != nil {
			l.Fatalln(err)
		}
	} else if mode >= 0 && err == nil && int(fi.Mode()&0777) != mode {
		err := os.Chmod(dir, os.FileMode(mode))
		// This can fail on crappy filesystems, nothing we can do about it.
		if err != nil {
			l.Warnln(err)
		}
	}
}

func defaultConfig(myName string) config.Configuration {
	newCfg := config.New(myID, myName)

	port, err := getFreePort("0.0.0.0", 22000)
	if err != nil {
		l.Fatalln("get free port (BEP):", err)
	}
	newCfg.Options.ListenAddress = []string{fmt.Sprintf("tcp://0.0.0.0:%d", port)}

	return newCfg
}

// getFreePort returns a free TCP port fort listening on. The ports given are
// tried in succession and the first to succeed is returned. If none succeed,
// a random high port is returned.
func getFreePort(host string, ports ...int) (int, error) {
	for _, port := range ports {
		c, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
		if err == nil {
			c.Close()
			return port, nil
		}
	}

	c, err := net.Listen("tcp", host+":0")
	if err != nil {
		return 0, err
	}
	addr := c.Addr().(*net.TCPAddr)
	c.Close()
	return addr.Port, nil
}
