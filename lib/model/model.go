package model

import (
	"fmt"
	"io"

	"github.com/syncthing/protocol"
	"github.com/syncthing/syncthing/lib/sync"
)

type Model struct {
	protoConn map[protocol.DeviceID]protocol.Connection
	rawConn   map[protocol.DeviceID]io.Closer
	deviceVer map[protocol.DeviceID]string
	pmut      sync.RWMutex // protects protoConn and rawConn
}

func NewModel() *Model {
	return &Model{
		protoConn: make(map[protocol.DeviceID]protocol.Connection),
		rawConn:   make(map[protocol.DeviceID]io.Closer),
		deviceVer: make(map[protocol.DeviceID]string),
		pmut:      sync.NewRWMutex(),
	}
}

func (m *Model) AddConnection(rawConn io.Closer, protoConn protocol.Connection) {
	deviceID := protoConn.ID()

	m.pmut.Lock()
	if _, ok := m.protoConn[deviceID]; ok {
		panic("add existing device")
	}
	m.protoConn[deviceID] = protoConn
	if _, ok := m.rawConn[deviceID]; ok {
		panic("add existing device")
	}
	m.rawConn[deviceID] = rawConn

	protoConn.Start()

	/* send cluster config */ // TODO stop hard coding this, get it from model, like syncthing?
	cm := protocol.ClusterConfigMessage{
		// TODO set these correctly
		ClientName:    "Syncthing-FUSE",
		ClientVersion: "0.0.0",
		Options:       []protocol.Option{},
	}
	cr := protocol.Folder{
		ID: "default",
	}
	cm.Folders = append(cm.Folders, cr)
	protoConn.ClusterConfig(cm)
}

func (m *Model) ConnectedTo(deviceID protocol.DeviceID) bool {
	m.pmut.RLock()
	_, ok := m.protoConn[deviceID]
	m.pmut.RUnlock()
	return ok
}

// An index was received from the peer device
func (m *Model) Index(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo, flags uint32, options []protocol.Option) {
	fmt.Println("model: receiving index from device ", deviceID, " for folder ", folder)

	for _, file := range files {
		fmt.Println("model Index: peer has file ", file.Name)
	}
}

// An index update was received from the peer device
func (m *Model) IndexUpdate(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo, flags uint32, options []protocol.Option) {
}

// A request was made by the peer device
func (m *Model) Request(deviceID protocol.DeviceID, folder string, name string, offset int64, hash []byte, flags uint32, options []protocol.Option, buf []byte) error {
	return protocol.ErrNoSuchFile
}

// A cluster configuration message was received
func (m *Model) ClusterConfig(deviceID protocol.DeviceID, config protocol.ClusterConfigMessage) {
	fmt.Println("model: receiving cluster config from device ", deviceID)
}

// The peer device closed the connection
func (m *Model) Close(deviceID protocol.DeviceID, err error) {
}
