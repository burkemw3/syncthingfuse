package model

import (
	"fmt"
	"path"

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

		entries:     make(map[string]map[string]protocol.FileInfo),
		childLookup: make(map[string]map[string][]string),
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

func (m *Model) GetEntry(folder string, path string) protocol.FileInfo {
	m.fmut.RLock()

	entry := m.entries[folder][path]

	m.fmut.RUnlock()

	return entry
}

func (m *Model) GetFileData(folder string, path string) ([]byte, error) {
	m.fmut.RLock()

	entry := m.entries[folder][path]
	if debug {
		l.Debugln("Creating data for", folder, path, "size", entry.Size())
	}
	data := make([]byte, entry.Size())

	for _, conn := range m.protoConn {
		for i, block := range entry.Blocks {
			if debug {
				l.Debugln("Fetching block", i, "size", block.Size)
			}
			byteOffset := int64(i * protocol.BlockSize)
			flags := uint32(0)
			blockData, err := conn.Request(folder, path, byteOffset, int(block.Size), block.Hash, flags, []protocol.Option{})
			if err != nil {
				return blockData, err
			}
			// TODO check hash
			if debug {
				l.Debugln("Putting data at", byteOffset)
			}
			for j, k := byteOffset, int32(0); k < block.Size; j, k = j+1, k+1 {
				data[j] = blockData[k]
			}
		}

		break // only one device for now ...
		// TODO support multiple devices
		// TODO support zero devices
	}

	m.fmut.RUnlock()

	return data, nil
}

func (m *Model) GetChildren(folder string, path string) []protocol.FileInfo {
	m.fmut.RLock()

	// TODO assert is directory?

	entries := m.childLookup[folder][path]
	result := make([]protocol.FileInfo, len(entries))
	for i, childPath := range entries {
		result[i] = m.entries[folder][childPath]
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
		m.childLookup[folder] = make(map[string][]string)
	}

	for _, file := range files {
		if file.IsDeleted() {
			if debug {
				l.Debugln("model Index: peer has deleted file", file.Name)
			}
			continue
		}
		if file.IsInvalid() {
			if debug {
				l.Debugln("model Index: peer has invalid file", file.Name)
			}
			continue
		}
		if file.IsSymlink() {
			if debug {
				l.Debugln("model Index: peer has symlink", file.Name)
			}
			continue
		}
		if debug {
			l.Debugln("model Index: peer has file/dir", file.Name)
		}

		// Add to primary lookup
		m.entries[folder][file.Name] = file

		// add to directory children lookup
		dir := path.Dir(file.Name)
		_, ok := m.childLookup[folder][dir]
		if ok {
			m.childLookup[folder][dir] = append(m.childLookup[folder][dir], file.Name)
		} else {
			children := make([]string, 1)
			children[0] = file.Name
			m.childLookup[folder][dir] = children
		}
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
