package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"

	"github.com/burkemw3/syncthing-fuse/lib/model"
	"github.com/calmh/logger"
	"github.com/syncthing/protocol"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/connections"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/thejerf/suture"
)

var (
	Version     = "unknown-dev"
	LongVersion = Version
)

var (
	cfg        *config.Wrapper
	myID       protocol.DeviceID
	confDir    string
	stop       = make(chan int)
	discoverer *discover.Discoverer
	cert       tls.Certificate
	lans       []*net.IPNet
	m          *model.Model
)

const (
	bepProtocolName = "bep/1.0"
)

var l = logger.DefaultLogger

// Command line and environment options
var (
	showVersion    bool
	addDeviceId    string
	fuseMountPoint string
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
	flag.StringVar(&addDeviceId, "add-device", "", "Add a new device to the configuration, and exit (requires restart)")
	flag.StringVar(&fuseMountPoint, "fuse-mount-point", "", "Place to mount FUSE")

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

	if addDeviceId != "" {
		deviceId, _ := protocol.DeviceIDFromString(addDeviceId)
		upsertNewDeviceToConfiguration(cfg, deviceId)
		l.Infoln("Upserted ", addDeviceId, " to configuration for connection")
		return
	}

	if fuseMountPoint == "" {
		fmt.Println("fuse-mount-point is required")
		os.Exit(1)
	}

	mainSvc := suture.New("main", suture.Spec{
		Log: func(line string) {
			l.Debugln(line)
		},
	})
	mainSvc.ServeBackground()

	discoverer := startDiscovery(cfg)

	m = model.NewModel()

	connectionSvc := connections.NewConnectionSvc(cfg, myID, m, tlsCfg, tlsDefaultCommonName, nil, nil)
	connectionSvc.SetDiscoverer(discoverer)
	mainSvc.Add(connectionSvc)

	l.Infoln("Started ...")

	MountFuse(fuseMountPoint, m) // TODO handle fight between FUSE and Syncthing Service

	mainSvc.Stop()
	l.Okln("Exiting")

	return
}

func startDiscovery(cfg *config.Wrapper) *discover.Discoverer {
	opts := cfg.Options()
	disc := discover.NewDiscoverer(myID, opts.ListenAddress, nil)

	if opts.LocalAnnEnabled {
		l.Infoln("Starting local discovery announcements")
		disc.StartLocal(opts.LocalAnnPort, opts.LocalAnnMCAddr)
	}

	if opts.GlobalAnnEnabled {
		l.Infoln("Starting global discovery announcements")

		uri, err := url.Parse(opts.ListenAddress[0])
		if err != nil {
			l.Fatalf("Failed to parse listen address %s: %v", opts.ListenAddress[0], err)
		}

		addr, err := net.ResolveTCPAddr("tcp", uri.Host)
		if err != nil {
			l.Fatalln("Bad listen address:", err)
		}

		localPort := addr.Port

		disc.StartGlobal(opts.GlobalAnnServers, uint16(localPort))
	}

	return disc
}
