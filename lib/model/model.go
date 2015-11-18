package model

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand"
	"os"

	"github.com/boltdb/bolt"
	"github.com/burkemw3/syncthing-fuse/lib/config"
	"github.com/burkemw3/syncthing-fuse/lib/fileblockcache"
	"github.com/burkemw3/syncthing-fuse/lib/filetreecache"
	"github.com/cznic/mathutil"
	stmodel "github.com/syncthing/syncthing/lib/model"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/sync"
)

type Model struct {
	cfg *config.Wrapper
	db  *bolt.DB

	protoConn map[protocol.DeviceID]stmodel.Connection
	pmut      sync.RWMutex // protects protoConn and rawConn. must not be acquired before fmut

	devicesByFile map[string]map[string][]protocol.DeviceID
	filesByDevice map[string]map[protocol.DeviceID][]string
	blockCaches   map[string]*fileblockcache.FileBlockCache
	treeCaches    map[string]*filetreecache.FileTreeCache
	fmut          sync.RWMutex // protects file information. must not be acquired after pmut
}

func NewModel(cfg *config.Wrapper, db *bolt.DB) *Model {
	m := &Model{
		cfg: cfg,
		db:  db,

		protoConn: make(map[protocol.DeviceID]stmodel.Connection),
		pmut:      sync.NewRWMutex(),

		blockCaches:   make(map[string]*fileblockcache.FileBlockCache),
		treeCaches:    make(map[string]*filetreecache.FileTreeCache),
		devicesByFile: make(map[string]map[string][]protocol.DeviceID),
		filesByDevice: make(map[string]map[protocol.DeviceID][]string),
		fmut:          sync.NewRWMutex(),
	}

	for _, folderCfg := range m.cfg.Folders() {
		folder := folderCfg.ID

		fbc, err := fileblockcache.NewFileBlockCache(m.cfg, db, folderCfg)
		if err != nil {
			l.Warnln("Skipping folder", folder, "because fileblockcache init failed:", err)
		}
		m.blockCaches[folder] = fbc
		m.treeCaches[folder] = filetreecache.NewFileTreeCache(m.cfg, db, folder)

		m.devicesByFile[folder] = make(map[string][]protocol.DeviceID)
		m.filesByDevice[folder] = make(map[protocol.DeviceID][]string)
	}

	m.removeUnconfiguredFolders()

	return m
}

func (m *Model) removeUnconfiguredFolders() {
	m.db.Update(func(tx *bolt.Tx) error {
		deletedFolders := make([]string, 0)

		tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			folderName := string(name)
			if _, ok := m.blockCaches[folderName]; ok {
				return nil
			}

			// folder no longer in configuration, clean it out!

			if debug {
				l.Debugln("cleaning up deleted folder", folderName)
			}

			diskCacheFolder := fileblockcache.GetDiskCacheBasePath(m.cfg, folderName)
			err := os.RemoveAll(diskCacheFolder)
			if err != nil {
				l.Warnln("Cannot cleanup deleted folder", folderName, err)
			}

			deletedFolders = append(deletedFolders, folderName)

			return nil
		})

		for _, deletedFolder := range deletedFolders {
			err := tx.DeleteBucket([]byte(deletedFolder))
			if err != nil {
				l.Warnln("Cannot cleanup deleted folder's bucket", deletedFolder, err)
			}
		}

		return nil
	})
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

func (m *Model) GetFolders() []string {
	m.fmut.RLock()
	folders := make([]string, 0, len(m.devicesByFile))
	for k := range m.devicesByFile {
		folders = append(folders, k)
	}
	m.fmut.RUnlock()
	return folders
}

func (m *Model) HasFolder(folder string) bool {
	result := false
	m.fmut.RLock()
	if _, ok := m.devicesByFile[folder]; ok {
		result = true
	}
	m.fmut.RUnlock()
	return result
}

func (m *Model) GetEntry(folder string, path string) protocol.FileInfo {
	m.fmut.RLock()

	entry, _ := m.treeCaches[folder].GetEntry(path)

	m.fmut.RUnlock()

	return entry
}

func (m *Model) GetFileData(folder string, filepath string, readStart int64, readSize int) ([]byte, error) {
	if debug {
		l.Debugln("Read for", folder, filepath, readStart, readSize)
	}

	// can probably make lock acquisition less contentious here
	m.fmut.Lock()
	defer m.fmut.Unlock()
	m.pmut.RLock()
	defer m.pmut.RUnlock()

	entry, found := m.treeCaches[folder].GetEntry(filepath)
	if false == found {
		l.Warnln("File not found", folder, filepath)
		return []byte(""), protocol.ErrNoSuchFile
	}

	data := make([]byte, readSize)
	readEnd := readStart + int64(readSize)

	blocksExpected := 0
	messages := make(chan bool)

	// create workers for pulling
	for i, block := range entry.Blocks {
		blockStart := int64(i * protocol.BlockSize)
		blockEnd := blockStart + int64(block.Size)

		if blockEnd > readStart && blockStart < readEnd { // need this block
			blocksExpected++
			go m.copyBlockToData(folder, filepath, readStart, readEnd, blockStart, blockEnd, block, data, messages)
		}
	}

	// wait for workers to finish
	for i := 0; i < blocksExpected; i++ {
		result := <-messages
		if !result {
			return []byte(""), errors.New("a required block was not successfully retrieved")
		}
	}

	return data, nil
}

// requires fmut and pmut read locks (or better) before entry
func (m *Model) copyBlockToData(folder string, filepath string, readStart int64, readEnd int64, blockStart int64, blockEnd int64, block protocol.BlockInfo, data []byte, messages chan bool) { // TODO add channel
	blockData, blockFound := m.blockCaches[folder].GetCachedBlockData(block.Hash)

	if false == blockFound {
		requestedData, requestError := m.pullBlock(folder, filepath, blockStart, block)

		if requestError != nil {
			l.Warnln("Can't get block at offset", blockStart, "from any devices for", folder, filepath, requestError)
			messages <- false
			return
		}

		blockData = requestedData

		// Add block to cache
		m.blockCaches[folder].AddCachedFileData(block, blockData)
	} else if debug {
		l.Debugln("Found block at offset", blockStart, "for", folder, filepath)
	}

	for j := mathutil.MaxInt64(readStart, blockStart); j < readEnd && j < blockEnd; j++ {
		outputItr := j - readStart
		inputItr := j - blockStart

		data[outputItr] = blockData[inputItr]
	}

	messages <- true
}

// requires fmut and pmut read locks (or better) before entry
func (m *Model) pullBlock(folder string, filepath string, offset int64, block protocol.BlockInfo) ([]byte, error) {
	if debug {
		l.Debugln("Fetching block at offset", offset, "size", block.Size, "for", folder, filepath)
	}

	flags := uint32(0)

	// Get block from a device
	devices := m.devicesByFile[folder][filepath]
	for _, deviceIndex := range rand.Perm(len(devices)) {
		deviceWithFile := devices[deviceIndex]
		if debug {
			l.Debugln("Trying to fetch block at offset", offset, "for", folder, filepath, "from device", deviceWithFile)
		}
		conn := m.protoConn[deviceWithFile]
		requestedData, requestError := conn.Request(folder, filepath, offset, int(block.Size), block.Hash, flags, []protocol.Option{})
		if requestError == nil {
			// check hash
			actualHash := sha256.Sum256(requestedData)
			if bytes.Equal(actualHash[:], block.Hash) {
				return requestedData, nil
			} else if debug {
				l.Debugln("Hash mismatch expected", block.Hash, "received", actualHash)
			}
		}
		if debug {
			l.Debugln("Error fetching block at offset", offset, "from device", deviceWithFile, ":", requestError)
		}
	}

	return []byte(""), errors.New("can't get block from any devices")
}

func (m *Model) GetChildren(folder string, path string) []protocol.FileInfo {
	m.fmut.RLock()

	// TODO assert is directory?

	entries := m.treeCaches[folder].GetChildren(path)
	result := make([]protocol.FileInfo, len(entries))
	for i, childPath := range entries {
		result[i], _ = m.treeCaches[folder].GetEntry(childPath)
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

	treeCache, ok := m.treeCaches[folder]
	if !ok {
		if debug {
			l.Debugln("folder", folder, "from", deviceID.String(), "not configured, skipping")
		}
		m.fmut.Unlock()
		return
	}

	for _, file := range files {
		entry, existsInLocalModel := treeCache.GetEntry(file.Name)

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

	m.treeCaches[folder].AddEntry(file)

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
	entries := m.treeCaches[folder].GetChildren(filePath)
	for _, childPath := range entries {
		m.removeEntryFromLocalModel(folder, childPath)
	}

	if debug {
		_, ok := m.treeCaches[folder].GetEntry(filePath)
		if ok {
			l.Debugln("file exists in local model, so removing", filePath)
		}
	}

	// remove file entry
	m.treeCaches[folder].RemoveEntry(filePath)

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
