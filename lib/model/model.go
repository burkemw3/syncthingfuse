package model

import (
	"bytes"
	"crypto/sha1"
	"encoding/gob"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/syncthing/syncthing/lib/config"
	stmodel "github.com/syncthing/syncthing/lib/model"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/sync"
)

type Model struct {
	cfg *config.Wrapper

	protoConn map[protocol.DeviceID]stmodel.Connection
	pmut      sync.RWMutex // protects protoConn and rawConn. must not be acquired before fmut

	entries           map[string]map[string]protocol.FileInfo
	devicesByFile     map[string]map[string][]protocol.DeviceID
	filesByDevice     map[string]map[protocol.DeviceID][]string
	childLookup       map[string]map[string][]string
	cachedFilesByPath map[string]map[string]string // st.Folder.Name to st.File.Name to local disk file name
	fmut              sync.RWMutex                 // protects file information. must not be acquired after pmut
}

func NewModel(cfg *config.Wrapper) *Model {
	return &Model{
		cfg: cfg,

		protoConn: make(map[protocol.DeviceID]stmodel.Connection),
		pmut:      sync.NewRWMutex(),

		entries:           make(map[string]map[string]protocol.FileInfo),
		devicesByFile:     make(map[string]map[string][]protocol.DeviceID),
		filesByDevice:     make(map[string]map[protocol.DeviceID][]string),
		childLookup:       make(map[string]map[string][]string),
		cachedFilesByPath: make(map[string]map[string]string),
		fmut:              sync.NewRWMutex(),
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

func (m *Model) GetFileData(folder string, filepath string) ([]byte, error) {
	m.fmut.RLock()

	entry := m.entries[folder][filepath]
	if debug {
		l.Debugln("Creating data for", folder, filepath, "size", entry.Size())
	}

	// check disk cache
	expectedDiskCacheName := m.getDiskCacheName(folder, entry)
	diskCachePath := path.Join(path.Dir(m.cfg.ConfigPath()), folder, expectedDiskCacheName)
	if foundDiskCacheName, ok := m.cachedFilesByPath[folder][filepath]; ok {
		if foundDiskCacheName == expectedDiskCacheName {
			// found a good match
			data, _ := ioutil.ReadFile(diskCachePath) // TODO check error
			m.fmut.RUnlock()
			return data, nil
		}
	}

	// didn't find, gonna have to modify cache, so drop read lock, and re-acquire write lock later
	m.fmut.RUnlock()

	data := make([]byte, entry.Size())

	for i, block := range entry.Blocks {
		// TODO fetch blocks in parallel
		if debug {
			l.Debugln("Fetching block", i, "size", block.Size)
		}
		byteOffset := int64(i * protocol.BlockSize)
		flags := uint32(0)

		m.pmut.RLock()
		var blockData []byte
		err := protocol.ErrNoSuchFile
		// TODO use the devicesByFile lookup
		for _, conn := range m.protoConn {
			blockData, err = conn.Request(folder, filepath, byteOffset, int(block.Size), block.Hash, flags, []protocol.Option{})
			if err == nil {
				break
			}
		}
		m.pmut.RUnlock()
		if err != nil {
			return blockData, err
		}

		// TODO check hash

		for j, k := byteOffset, int32(0); k < block.Size; j, k = j+1, k+1 {
			data[j] = blockData[k]
		}
	}

	m.fmut.Lock()

	if foundDiskCacheName, ok := m.cachedFilesByPath[folder][filepath]; ok {
		if foundDiskCacheName != expectedDiskCacheName {
			oldDiskCachePath := path.Join(path.Dir(m.cfg.ConfigPath()), folder, foundDiskCacheName)
			os.Remove(oldDiskCachePath)
		}
	}

	m.cachedFilesByPath[folder][filepath] = expectedDiskCacheName
	ioutil.WriteFile(diskCachePath, data, 0644)

	m.fmut.Unlock()

	return data, nil
}

func (m *Model) getDiskCacheName(folder string, file protocol.FileInfo) string {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(folder)
	enc.Encode(file.Name)
	enc.Encode(file.Version)

	h := sha1.New()
	h.Write(buf.Bytes())

	diskCacheName := fmt.Sprintf("%x", h.Sum(nil))

	return diskCacheName
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

	// supersede previous index (remove device from devicesByFile lookup)
	m.fmut.Lock()
	if filesByDevice, ok := m.filesByDevice[folder]; ok {
		if files, ok := filesByDevice[deviceID]; ok {
			for _, file := range files {
				devices := m.devicesByFile[folder][file]
				candidate := 0
				for ; candidate < len(devices); candidate = candidate + 1 {
					if devices[candidate].Equals(deviceID) {
						break
					}
				}
				if candidate < len(devices) {
					if len(devices) == 1 {
						delete(m.devicesByFile[folder], file)
					} else {
						devices[candidate] = devices[len(devices)-1]
						devices = devices[:len(devices)-1]
						m.devicesByFile[folder][file] = devices
					}
				}
			}
		}
	}
	m.fmut.Unlock()

	m.updateIndex(deviceID, folder, files)
}

// An index update was received from the peer device
func (m *Model) IndexUpdate(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo, flags uint32, options []protocol.Option) {
	if debug {
		l.Debugln("model: receiving index from device", deviceID, "for folder", folder)
	}

	m.updateIndex(deviceID, folder, files)
}

func (m *Model) updateIndex(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo) {
	m.fmut.Lock()

	_, ok := m.entries[folder]
	if !ok {
		m.entries[folder] = make(map[string]protocol.FileInfo)
		m.childLookup[folder] = make(map[string][]string)
		m.devicesByFile[folder] = make(map[string][]protocol.DeviceID)
		m.filesByDevice[folder] = make(map[protocol.DeviceID][]string)
		m.cachedFilesByPath[folder] = make(map[string]string)

		diskCacheFolder := path.Join(path.Dir(m.cfg.ConfigPath()), folder)
		os.RemoveAll(diskCacheFolder)
		os.Mkdir(diskCacheFolder, 0744)
	}

	for _, file := range files {
		entry, existsInLocalModel := m.entries[folder][file.Name]

		if !existsInLocalModel {
			if debug {
				l.Debugln("file", file.Name, "from", deviceID.String(), "does not exist in local model, trying to add")
			}
			m.addToLocalModel(deviceID, folder, file)
			continue
		}

		localToGlobal := entry.Version.Compare(file.Version)
		if localToGlobal == protocol.Equal {
			if debug {
				l.Debugln("peer", deviceID.String(), "has same version for file", file.Name, ", adding as peer.")
			}
			m.addPeerForEntry(deviceID, folder, file)
			continue
		}

		if localToGlobal == protocol.Lesser || localToGlobal == protocol.ConcurrentLesser {
			if debug {
				l.Debugln("peer", deviceID.String(), "has new version for file", file.Name, ", replacing current data.")
			}
			m.removeEntryFromLocalModel(folder, file.Name)
			m.addToLocalModel(deviceID, folder, file)
			// TODO probably want to re-fill disk cache if file small enough (10MB?) or within certain size (5%?)
			continue
		}
	}

	m.fmut.Unlock()
}

// requires write lock on model.fmut before entry
// requires file does not exist in local model
func (m *Model) addToLocalModel(deviceID protocol.DeviceID, folder string, file protocol.FileInfo) {
	if file.IsDeleted() {
		if debug {
			l.Debugln("peer", deviceID.String(), "has deleted file, doing nothing", file.Name)
		}
		return
	}
	if file.IsInvalid() {
		if debug {
			l.Debugln("peer", deviceID.String(), "has invalid file, doing nothing", file.Name)
		}
		return
	}
	if file.IsSymlink() {
		if debug {
			l.Debugln("peer", deviceID.String(), "has symlink, doing nothing", file.Name)
		}
		return
	}

	if debug && file.IsDirectory() {
		l.Debugln("peer", deviceID.String(), "has directory, adding", file.Name)
	} else if debug {
		l.Debugln("peer", deviceID.String(), "has file, adding", file.Name)
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

	m.addPeerForEntry(deviceID, folder, file)
}

// requires write lock on model.fmut before entry
func (m *Model) addPeerForEntry(deviceID protocol.DeviceID, folder string, file protocol.FileInfo) {
	peers, ok := m.devicesByFile[folder][file.Name]
	if ok {
		shouldAdd := true
		for candidate := 0; candidate < len(peers); candidate = candidate + 1 {
			if peers[candidate].Equals(deviceID) {
				shouldAdd = false
				break
			}
		}
		if shouldAdd {
			peers = append(peers, deviceID)
			m.devicesByFile[folder][file.Name] = peers
		}
	} else {
		peers = make([]protocol.DeviceID, 1)
		peers[0] = deviceID
		m.devicesByFile[folder][file.Name] = peers
	}
}

// remove any children and self from local model
// requires write lock on model.fmut before entry
func (m *Model) removeEntryFromLocalModel(folder string, filePath string) {
	entries := m.childLookup[folder][filePath]
	for _, childPath := range entries {
		m.removeEntryFromLocalModel(folder, childPath)
	}

	if debug {
		_, ok := m.entries[folder][filePath]
		if ok {
			l.Debugln("file exists in local model, so removing", filePath)
		}
	}

	// remove file entry
	delete(m.entries[folder], filePath)

	// remove files by device lookup
	for _, device := range m.devicesByFile[folder][filePath] {
		if files, ok := m.filesByDevice[folder][device]; ok {
			victim := len(files)
			for i, file := range files {
				if file == filePath {
					victim = i
					break
				}
			}
			if victim < len(files) {
				if len(files) == 1 {
					delete(m.filesByDevice[folder], device)
				} else {
					files[victim] = files[len(files)-1]
					files = files[:len(files)-1]
					m.filesByDevice[folder][device] = files
				}
			}
		}
	}

	// remove devices by file lookup
	delete(m.devicesByFile[folder], filePath)

	// remove from parent lookup
	dir := path.Dir(filePath)
	children, ok := m.childLookup[folder][dir]
	if ok {
		for candidate := 0; candidate < len(children); candidate = candidate + 1 {
			if children[candidate] == filePath {
				indexAndNewLength := len(children) - 1
				children[candidate] = children[indexAndNewLength]
				children = children[:indexAndNewLength]
				m.childLookup[folder][dir] = children
				break
			}
		}
	}

	// remove from disk cache
	if foundDiskCacheName, ok := m.cachedFilesByPath[folder][filePath]; ok {
		oldDiskCachePath := path.Join(path.Dir(m.cfg.ConfigPath()), folder, foundDiskCacheName)
		os.Remove(oldDiskCachePath)
	}
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
	m.pmut.Lock()
	delete(m.protoConn, deviceID)
	m.pmut.Unlock()

	m.fmut.Lock()

	// remove devices by file lookup
	for folder, _ := range m.filesByDevice {
		for _, file := range m.filesByDevice[folder][deviceID] {
			if devices, ok := m.devicesByFile[folder][file]; ok {
				victim := len(devices)
				for i, device := range devices {
					if device == deviceID {
						victim = i
						break
					}
				}
				if victim < len(devices) {
					if len(devices) == 1 {
						delete(m.devicesByFile[folder], file)
					} else {
						devices[victim] = devices[len(devices)-1]
						devices = devices[:len(devices)-1]
						m.devicesByFile[folder][file] = devices
					}
				}
			}
		}
	}

	// remove files by device lookup
	for _, filesByDevice := range m.filesByDevice {
		delete(filesByDevice, deviceID)
	}

	m.fmut.Unlock()
}
