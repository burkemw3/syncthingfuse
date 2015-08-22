package main

import (
    "crypto/tls"
    "fmt"
    "io"
    "net"
    "time"

	"github.com/syncthing/protocol"
	"github.com/syncthing-fuse/lib/model"
)

func bepConnect(tlsCfg *tls.Config, myID protocol.DeviceID) {
    addr := net.JoinHostPort("192.168.2.131", "22000") // TODO use a real address
    raddr, err := net.ResolveTCPAddr("tcp", addr)
    conn, err := net.DialTCP("tcp", nil, raddr)
	if err != nil {
		l.Fatalln(err)
	}

    setTCPOptions(conn)

    tc := tls.Client(conn, tlsCfg)
	err = tc.Handshake()
	if err != nil {
	    tc.Close()
		l.Fatalln("TLS handshake:", err)
	}

    l.Infoln("Finished TLS Handshake")
    handle(tc, myID)
    tc.Close()
}

func handle(conn *tls.Conn, myID protocol.DeviceID) {
	cs := conn.ConnectionState()

	// We should have negotiated the next level protocol "bep/1.0" as part
	// of the TLS handshake. Unfortunately this can't be a hard error,
	// because there are implementations out there that don't support
	// protocol negotiation (iOS for one...).
	if !cs.NegotiatedProtocolIsMutual || cs.NegotiatedProtocol != bepProtocolName {
		l.Infof("Peer %s did not negotiate bep/1.0", conn.RemoteAddr())
	}

	// We should have received exactly one certificate from the other
	// side. If we didn't, they don't have a device ID and we drop the
	// connection.
	certs := cs.PeerCertificates
	if cl := len(certs); cl != 1 {
    	conn.Close()
		l.Fatalln("Got peer certificate list of length %d != 1 from %s; protocol error", cl, conn.RemoteAddr())
	}
	remoteCert := certs[0]
	remoteID := protocol.NewDeviceID(remoteCert.Raw)

	// The device ID should not be that of ourselves. It can happen
	// though, especially in the presence of NAT hairpinning, multiple
	// clients between the same NAT gateway, and global discovery.
	if remoteID == myID {
    	conn.Close()
		l.Fatalln("Connected to myself (%s) - should not happen", remoteID)
	}

    // TODO should not already be connected to other party (see syncthing~connections.go)

    // TODO should verify known device (see syncthing~connections.go)
    
    wr := io.Writer(conn)
	rd := io.Reader(conn)

    var model model.Model
	name := fmt.Sprintf("%s-%s", conn.LocalAddr(), conn.RemoteAddr())
	protoConn := protocol.NewConnection(remoteID, rd, wr, model, name, protocol.CompressNever) // TODO use device config compression setting

	l.Infof("Established secure connection to %s at %s", remoteID, name)

	protoConn.Start()

    /* send cluster config */
	cm := protocol.ClusterConfigMessage{
	    // TODO set these correctly
		ClientName:    "Syncthing-FUSE",
		ClientVersion: "0.0.0",
		Options: []protocol.Option{},
	}
    cr := protocol.Folder{
		ID: "default",
	}
    cm.Folders = append(cm.Folders, cr)
	protoConn.ClusterConfig(cm)

    for {
    }
}

func setTCPOptions(conn *net.TCPConn) {
	var err error
	if err = conn.SetLinger(0); err != nil {
		l.Infoln(err)
	}
	if err = conn.SetNoDelay(false); err != nil {
		l.Infoln(err)
	}
	if err = conn.SetKeepAlivePeriod(60 * time.Second); err != nil {
		l.Infoln(err)
	}
	if err = conn.SetKeepAlive(true); err != nil {
		l.Infoln(err)
	}
}

/*


func listen(conn *net.UDPConn) net.UDPAddr {
	for {
		bs := make([]byte, 65536)
		n, addr, err := conn.ReadFromUDP(bs)
		check(err)
		buffer := make([]byte, n)
		copy(buffer, bs)

		var pkt discover.Announce
		err = pkt.UnmarshalXDR(buffer)
		check(err)

		l.Debugf("discover: Received local announcement from FOOBAR for %s. Addresses: ", protocol.DeviceIDFromBytes(pkt.This.ID))
		for _, address := range pkt.This.Addresses {
		    deviceAddr := net.UDPAddr{IP: addr.IP, Port: int(address.Port)} // TODO can conversion be better?
		    l.Debugf("discover: Address %s Port %d", deviceAddr.IP.String(), deviceAddr.Port)
		    return deviceAddr
		}
	}
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
*/