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
	stconfig "github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/connections"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/osutil"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay"
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

	cachedDiscovery, relaySvc := setupConnections(cfg.AsStCfg(myID), tlsCfg, cert, mainSvc)

	lans, _ := osutil.GetLans()

	connectionSvc := connections.NewConnectionSvc(cfg.AsStCfg(myID), myID, m, tlsCfg, cachedDiscovery, relaySvc, bepProtocolName, tlsDefaultCommonName, lans)
	mainSvc.Add(connectionSvc)

	if cfg.Raw().GUI.Enabled {
		api, err := newAPISvc(myID, cfg, m)
		if err != nil {
			l.Fatalln("Cannot start GUI:", err)
		}
		mainSvc.Add(api)
	}

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

// The discovery results are sorted by their source priority.
const (
	ipv6LocalDiscoveryPriority = iota
	ipv4LocalDiscoveryPriority
	globalDiscoveryPriority
)

func setupConnections(cfg *stconfig.Wrapper, tlsCfg *tls.Config, cert tls.Certificate, mainSvc *suture.Supervisor) (*discover.CachingMux, *relay.Svc) {
	opts := cfg.Raw().Options

	var relaySvc *relay.Svc
	if opts.RelaysEnabled && (opts.GlobalAnnEnabled || opts.RelayWithoutGlobalAnn) {
		relaySvc = relay.NewSvc(cfg, tlsCfg)
		mainSvc.Add(relaySvc)
	}

	cachedDiscovery := discover.NewCachingMux()
	mainSvc.Add(cachedDiscovery)

	listenAddressList := newAddressLister(cfg)

	if opts.GlobalAnnEnabled {
		for _, srv := range cfg.GlobalDiscoveryServers() {
			l.Infoln("Using discovery server", srv)
			gd, err := discover.NewGlobal(srv, cert, listenAddressList, relaySvc)
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

	if opts.LocalAnnEnabled {
		// v4 broadcasts
		bcd, err := discover.NewLocal(myID, fmt.Sprintf(":%d", opts.LocalAnnPort), listenAddressList, relaySvc)
		if err != nil {
			l.Warnln("IPv4 local discovery:", err)
		} else {
			cachedDiscovery.Add(bcd, 0, 0, ipv4LocalDiscoveryPriority)
		}
		// v6 multicasts
		mcd, err := discover.NewLocal(myID, opts.LocalAnnMCAddr, listenAddressList, relaySvc)
		if err != nil {
			l.Warnln("IPv6 local discovery:", err)
		} else {
			cachedDiscovery.Add(mcd, 0, 0, ipv6LocalDiscoveryPriority)
		}
	}

	return cachedDiscovery, relaySvc
}
