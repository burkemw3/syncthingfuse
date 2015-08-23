package main

import (
    "crypto/tls"
    "flag"
	"fmt"
	"net"
	"runtime"
	"time"

	"github.com/calmh/logger"
	"github.com/syncthing/protocol"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/discover"
)

var (
	Version     = "unknown-dev"
	LongVersion = Version
)

var (
	cfg            *config.Wrapper
	myID           protocol.DeviceID
	confDir        string
	stop           = make(chan int)
	discoverer     *discover.Discoverer
	cert           tls.Certificate
	lans           []*net.IPNet
)

const (
	bepProtocolName   = "bep/1.0"
)


var l = logger.DefaultLogger

// Command line and environment options
var (
	showVersion       bool
	generateDir       string
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

	l.Infoln(LongVersion)
	l.Infoln("My ID:", myID)

	cfg := getConfiguration()

    opts := cfg.Options()

    /* TODO don't announce anything, cuz we can't do respond to anything right now */
    opts.LocalAnnEnabled = false;
    opts.GlobalAnnEnabled = false;

	intfs, err := net.Interfaces()
	if err != nil {
		l.Debugln("discover: interfaces:", err)
		l.Infoln("Local discovery over IPv6 unavailable")
		return
	}

	//v6Intfs := 0
	for _, intf := range intfs {
		// Interface flags seem to always be 0 on Windows
		if runtime.GOOS != "windows" && (intf.Flags&net.FlagUp == 0 || intf.Flags&net.FlagMulticast == 0) {
			continue
		}
		fmt.Println("Interface: ", intf.Name)
		if intf.Name == "en1" {
/*    		mcaddr, err := net.ResolveUDPAddr("udp", "[ff32::5222]:21026")
            check(err)
            socket, err := net.ListenMulticastUDP("udp", &intf, mcaddr)
            check(err)
            fmt.Println("listening ...")
            bep_addr := listen(socket)
*/
            bepConnect(tlsCfg, myID)
		}
	}


    return

	protocol.PingTimeout = time.Duration(opts.PingTimeoutS) * time.Second
	protocol.PingIdleTime = time.Duration(opts.PingIdleTimeS) * time.Second

	addr, err := net.ResolveTCPAddr("tcp", opts.ListenAddress[0])
	if err != nil {
		l.Fatalln("Bad listen address:", err)
	}

	// Start discovery

	localPort := addr.Port
	discoverer = discovery(localPort)

	fmt.Printf("Waiting ...")
}


func discovery(extPort int) *discover.Discoverer {
	opts := cfg.Options()
	disc := discover.NewDiscoverer(myID, opts.ListenAddress)

	if opts.LocalAnnEnabled {
		l.Infoln("Starting local discovery announcements")
		disc.StartLocal(opts.LocalAnnPort, opts.LocalAnnMCAddr)
	}

	if opts.GlobalAnnEnabled {
		l.Infoln("Starting global discovery announcements")
		disc.StartGlobal(opts.GlobalAnnServers, uint16(extPort))
	}

	return disc
}