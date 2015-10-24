package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"os"
	"path"

	"github.com/boltdb/bolt"
	"github.com/burkemw3/syncthing-fuse/lib/config"
	"github.com/burkemw3/syncthing-fuse/lib/model"
	"github.com/calmh/logger"
	"github.com/syncthing/syncthing/lib/connections"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/osutil"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/thejerf/suture"
)

var (
	Version     = "unknown-dev"
	LongVersion = Version
)

var (
	cfg     *config.Wrapper
	myID    protocol.DeviceID
	confDir string
	stop    = make(chan int)
	cert    tls.Certificate
	lans    []*net.IPNet
	m       *model.Model
)

const (
	bepProtocolName = "bep/1.0"
)

var l = logger.DefaultLogger

// Command line and environment options
var (
	showVersion bool
)

const (
	usage      = "syncthing-fuse [options]"
	extraUsage = `
The default configuration directory is:

  %s

`
)

func main() {
	flag.BoolVar(&showVersion, "version", false, "Show version")

	flag.Usage = usageFor(flag.CommandLine, usage, fmt.Sprintf(extraUsage, baseDirs["config"]))
	flag.Parse()

	if showVersion {
		fmt.Println(Version)
		return
	}

	if err := expandLocations(); err != nil {
		l.Fatalln(err)
	}

	// Ensure that our home directory exists.
	ensureDir(baseDirs["config"], 0700)

	// Ensure that that we have a certificate and key.
	tlsCfg, cert := getTlsConfig()

	// We reinitialize the predictable RNG with our device ID, to get a
	// sequence that is always the same but unique to this syncthing instance.
	predictableRandom.Seed(seedFromBytes(cert.Certificate[0]))

	myID = protocol.NewDeviceID(cert.Certificate[0])
	l.SetPrefix(fmt.Sprintf("[%s] ", myID.String()[:5]))

	l.Infoln("Started syncthingfuse v.", LongVersion)
	l.Infoln("My ID:", myID)

	cfg := getConfiguration()

	if info, err := os.Stat(cfg.Raw().MountPoint); err == nil {
		if !info.Mode().IsDir() {
			l.Fatalln("Mount point (", cfg.Raw().MountPoint, ") must be a directory, but isn't")
			os.Exit(1)
		}
	} else {
		l.Infoln("Mount point (", cfg.Raw().MountPoint, ") does not exist, creating it")
		err = os.MkdirAll(cfg.Raw().MountPoint, 0700)
		if err != nil {
			l.Fatalln("Error creating mount point", cfg.Raw().MountPoint, err)
			os.Exit(1)
		}
	}

	mainSvc := suture.New("main", suture.Spec{
		Log: func(line string) {
			l.Debugln(line)
		},
	})
	mainSvc.ServeBackground()

	database := openDatabase(cfg)

	m = model.NewModel(cfg, database)

	cachedDiscovery := startDiscovery()
	mainSvc.Add(cachedDiscovery)

	lans, _ := osutil.GetLans()

	connectionSvc := connections.NewConnectionSvc(cfg.AsStCfg(myID), myID, m, tlsCfg, cachedDiscovery, nil /* TODO relaySvc */, bepProtocolName, tlsDefaultCommonName, lans)
	mainSvc.Add(connectionSvc)

	l.Infoln("Started ...")

	MountFuse(cfg.Raw().MountPoint, m) // TODO handle fight between FUSE and Syncthing Service

	mainSvc.Stop()
	l.Okln("Exiting")

	return
}

func openDatabase(cfg *config.Wrapper) *bolt.DB {
	databasePath := path.Join(path.Dir(cfg.ConfigPath()), "boltdb")
	database, _ := bolt.Open(databasePath, 0600, nil) // TODO check error
	return database
}

func startDiscovery() *discover.CachingMux {
	cachedDiscovery := discover.NewCachingMux()

	// TODO build discovery MUX

	return cachedDiscovery
}
