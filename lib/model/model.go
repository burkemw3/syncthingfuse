package model

import (
	"fmt"

	"github.com/syncthing/protocol"
	stmodel "github.com/syncthing/syncthing/lib/model"
	"github.com/syncthing/syncthing/lib/sync"
)

type Model struct {
	protoConn map[protocol.DeviceID]stmodel.Connection
	deviceVer map[protocol.DeviceID]string
	pmut      sync.RWMutex // protects protoConn and rawConn

	// TODO keep devices associated with each file
	entries     map[string]map[string]protocol.FileInfo
	childLookup map[string]map[string][]string
	fmut        sync.RWMutex // protects file information
}

func NewModel() *Model {
	return &Model{
		protoConn: make(map[protocol.DeviceID]stmodel.Connection),
		deviceVer: make(map[protocol.DeviceID]string),
		pmut:      sync.NewRWMutex(),

		entries: make(map[string]map[string]protocol.FileInfo),
		fmut:    sync.NewRWMutex(),
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

func (m *Model) GetEntry(folder string, path string) protocol.FileInfo {
	m.fmut.RLock()

	entry := m.entries[folder][path]
	result := protocol.FileInfo{
		Name:  entry.Name,
		Flags: entry.Flags,
	}

	m.fmut.RUnlock()

	return result
}

func (m *Model) GetChildren(folder string, path string) []protocol.FileInfo {
	m.fmut.RLock()

	entries := m.childLookup[folder][path]
	result := make([]protocol.FileInfo, len(entries))
	for i, childPath := range entries {
		entry := m.entries[folder][childPath]
		result[i] = protocol.FileInfo{
			Name:  entry.Name,
			Flags: entry.Flags,
		}
	}

	m.fmut.RUnlock()

	return result
}

// An index was received from the peer device
func (m *Model) Index(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo, flags uint32, options []protocol.Option) {
	if debug {
		l.Debugln("model: receiving index from device", deviceID, "for folder", folder)
	}

	m.fmut.Lock()

	_, ok := m.entries[folder]
	if !ok {
		m.entries[folder] = make(map[string]protocol.FileInfo)
	}

	for _, file := range files {
		l.Debugln("model Index: peer has file", file.Name)
		m.entries[folder][file.Name] = file
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
