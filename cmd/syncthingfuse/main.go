package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"os"
	"path"
	"time"

	"github.com/boltdb/bolt"
	"github.com/burkemw3/syncthingfuse/lib/config"
	"github.com/burkemw3/syncthingfuse/lib/model"
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
	usage      = "syncthingfuse [options]"
	extraUsage = `
The default configuration directory is:

  %s

`
)

// The discovery results are sorted by their source priority.
const (
	ipv6LocalDiscoveryPriority = iota
	ipv4LocalDiscoveryPriority
	globalDiscoveryPriority
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
			l.Warnln("Error creating mount point", cfg.Raw().MountPoint, err)
			l.Warnln("Sometimes, SyncthingFUSE doesn't shut down and unmount cleanly,")
			l.Warnln("If you don't know of any other file systems you have mounted at")
			l.Warnln("the mount point, try running the command below to unmount, then")
			l.Warnln("start SyncthingFUSE again.")
			l.Warnln("    umount", cfg.Raw().MountPoint)
			l.Fatalln("Cannot create missing mount point")
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

	lans, _ := osutil.GetLans()

	// Start discovery
	cachedDiscovery := discover.NewCachingMux()
	mainSvc.Add(cachedDiscovery)

	// Start connection management
	connectionsService := connections.NewService(cfg.AsStCfg(myID), myID, m, tlsCfg, cachedDiscovery, bepProtocolName, tlsDefaultCommonName, lans)
	mainSvc.Add(connectionsService)

	if cfg.Raw().Options.GlobalAnnounceEnabled {
		for _, srv := range cfg.Raw().Options.GlobalAnnounceServers {
			l.Infoln("Using discovery server", srv)
			gd, err := discover.NewGlobal(srv, cert, connectionsService)
			if err != nil {
				l.Warnln("Global discovery:", err)
				continue
			}

			// Each global discovery server gets its results cached for five
			// minutes, and is not asked again for a minute when it's returned
			// unsuccessfully.
			cachedDiscovery.Add(gd, 5*time.Minute, time.Minute, globalDiscoveryPriority)
		}
	}

	if cfg.Raw().Options.LocalAnnounceEnabled {
		// v4 broadcasts
		bcd, err := discover.NewLocal(myID, fmt.Sprintf(":%d", cfg.Raw().Options.LocalAnnouncePort), connectionsService)
		if err != nil {
			l.Warnln("IPv4 local discovery:", err)
		} else {
			cachedDiscovery.Add(bcd, 0, 0, ipv4LocalDiscoveryPriority)
		}
		// v6 multicasts
		mcd, err := discover.NewLocal(myID, cfg.Raw().Options.LocalAnnounceMCAddr, connectionsService)
		if err != nil {
			l.Warnln("IPv6 local discovery:", err)
		} else {
			cachedDiscovery.Add(mcd, 0, 0, ipv6LocalDiscoveryPriority)
		}
	}

	if cfg.Raw().GUI.Enabled {
		api, err := newAPISvc(myID, cfg, m)
		if err != nil {
			l.Fatalln("Cannot start GUI:", err)
		}
		mainSvc.Add(api)
	}

	l.Infoln("Started ...")

	MountFuse(cfg.Raw().MountPoint, m, mainSvc) // TODO handle fight between FUSE and Syncthing Service

	l.Okln("Exiting")

	return
}

func openDatabase(cfg *config.Wrapper) *bolt.DB {
	databasePath := path.Join(path.Dir(cfg.ConfigPath()), "boltdb")
	database, _ := bolt.Open(databasePath, 0600, nil) // TODO check error
	return database
}
