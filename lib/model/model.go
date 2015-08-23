package model

import (
	"fmt"

	"github.com/syncthing/protocol"
	"github.com/syncthing/syncthing/lib/sync"
	stmodel "github.com/syncthing/syncthing/lib/model"
)

type Model struct {
	protoConn map[protocol.DeviceID]stmodel.Connection
	deviceVer map[protocol.DeviceID]string
	pmut      sync.RWMutex // protects protoConn and rawConn

	folderFiles map[string]map[string]bool
	fmut        sync.RWMutex // protects file information
}

func NewModel() *Model {
	return &Model{
		protoConn: make(map[protocol.DeviceID]stmodel.Connection),
		deviceVer: make(map[protocol.DeviceID]string),
		pmut:      sync.NewRWMutex(),

		folderFiles: make(map[string]map[string]bool),
		fmut:        sync.NewRWMutex(),
	}
}

func (m *Model) AddConnection(conn stmodel.Connection) {
	deviceID := conn.ID()

	m.pmut.Lock()

	if _, ok := m.protoConn[deviceID]; ok {
		panic("add existing device")
	}
	m.protoConn[deviceID] = conn

	conn.Start()

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
	conn.ClusterConfig(cm)

	m.pmut.Unlock()
}

func (m *Model) ConnectedTo(deviceID protocol.DeviceID) bool {
	m.pmut.RLock()
	_, ok := m.protoConn[deviceID]
	m.pmut.RUnlock()
	return ok
}

func (m *Model) IsPaused(deviceID protocol.DeviceID) bool {
	return false
}

func (m *Model) GetFiles(folder string) []string {
	m.fmut.RLock()

	files := make([]string, 0)
	for filename, _ := range m.folderFiles[folder] {
		files = append(files, filename)
	}

	m.fmut.RUnlock()

	return files
}

// An index was received from the peer device
func (m *Model) Index(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo, flags uint32, options []protocol.Option) {
	if debug {
		l.Debugln("model: receiving index from device", deviceID, "for folder", folder)
	}

	m.fmut.Lock()

	_, ok := m.folderFiles[folder]
	if !ok {
		m.folderFiles[folder] = make(map[string]bool)
	}

	for _, file := range files {
		l.Debugln("model Index: peer has file", file.Name)
		m.folderFiles[folder][file.Name] = true
	}

	m.fmut.Unlock()
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
