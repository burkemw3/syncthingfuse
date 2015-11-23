package model

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand"
	"os"

	"github.com/boltdb/bolt"
	"github.com/burkemw3/syncthingfuse/lib/config"
	"github.com/burkemw3/syncthingfuse/lib/fileblockcache"
	"github.com/burkemw3/syncthingfuse/lib/filetreecache"
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

	blockCaches   map[string]*fileblockcache.FileBlockCache
	treeCaches    map[string]*filetreecache.FileTreeCache
	folderDevices map[string][]protocol.DeviceID
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
		folderDevices: make(map[string][]protocol.DeviceID),
		fmut:          sync.NewRWMutex(),
	}

	for _, folderCfg := range m.cfg.Folders() {
		folder := folderCfg.ID

		fbc, err := fileblockcache.NewFileBlockCache(m.cfg, db, folderCfg)
		if err != nil {
			l.Warnln("Skipping folder", folder, "because fileblockcache init failed:", err)
		}
		m.blockCaches[folder] = fbc
		m.treeCaches[folder] = filetreecache.NewFileTreeCache(folderCfg, db, folder)

		m.folderDevices[folder] = make([]protocol.DeviceID, len(folderCfg.Devices))
		for i, device := range folderCfg.Devices {
			m.folderDevices[folder][i] = device.DeviceID
		}
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
	folders := make([]string, 0, len(m.treeCaches))
	for k := range m.treeCaches {
		folders = append(folders, k)
	}
	m.fmut.RUnlock()
	return folders
}

func (m *Model) HasFolder(folder string) bool {
	result := false
	m.fmut.RLock()
	if _, ok := m.treeCaches[folder]; ok {
		result = true
	}
	m.fmut.RUnlock()
	return result
}

func (m *Model) GetEntry(folder string, path string) (protocol.FileInfo, bool) {
	m.fmut.RLock()
	defer m.fmut.RUnlock()

	return m.treeCaches[folder].GetEntry(path)
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
func (m *Model) copyBlockToData(folder string, filepath string, readStart int64, readEnd int64, blockStart int64, blockEnd int64, block protocol.BlockInfo, data []byte, messages chan bool) {
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
	devices, _ := m.treeCaches[folder].GetEntryDevices(filepath)
	for _, deviceIndex := range rand.Perm(len(devices)) {
		deviceWithFile := devices[deviceIndex]
		conn, ok := m.protoConn[deviceWithFile]
		if !ok { // not connected to device
			continue
		}

		if debug {
			l.Debugln("Trying to fetch block at offset", offset, "for", folder, filepath, "from device", deviceWithFile)
		}

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

	m.fmut.Lock()
	defer m.fmut.Unlock()

	if false == m.isFolderSharedWithDevice(folder, deviceID) {
		if debug {
			l.Debugln("model:", deviceID, "not shared with folder", folder, "so ignoring")
		}
		return
	}

	m.updateIndex(deviceID, folder, files)
}

// An index update was received from the peer device
func (m *Model) IndexUpdate(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo, flags uint32, options []protocol.Option) {
	if debug {
		l.Debugln("model: receiving index from device", deviceID, "for folder", folder)
	}

	m.fmut.Lock()
	defer m.fmut.Unlock()

	if false == m.isFolderSharedWithDevice(folder, deviceID) {
		if debug {
			l.Debugln("model:", deviceID, "not shared with folder", folder, "so ignoring")
		}
		return
	}

	m.updateIndex(deviceID, folder, files)
}

// required fmut read (or better) lock before entry
func (m *Model) isFolderSharedWithDevice(folder string, deviceID protocol.DeviceID) bool {
	for _, device := range m.folderDevices[folder] {
		if device.Equals(deviceID) {
			return true
		}
	}
	return false
}

// requires write lock on model.fmut before entry
func (m *Model) updateIndex(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo) {
	treeCache, ok := m.treeCaches[folder]
	if !ok {
		if debug {
			l.Debugln("folder", folder, "from", deviceID.String(), "not configured, skipping")
		}
		return
	}

	for _, file := range files {
		entry, existsInLocalModel := treeCache.GetEntry(file.Name)

		var globalToLocal protocol.Ordering
		if existsInLocalModel {
			globalToLocal = file.Version.Compare(entry.Version)
		}

		if debug {
			l.Debugln("updating entry for", file.Name, "from", deviceID.Short(), existsInLocalModel, globalToLocal)
		}

		// remove if necessary
		if existsInLocalModel && (globalToLocal == protocol.Greater || (file.Version.Concurrent(entry.Version) && file.WinsConflict(entry))) {
			if debug {
				l.Debugln("remove entry for", file.Name, "from", deviceID.Short())
			}

			treeCache.RemoveEntry(file.Name)
		}

		// add if necessary
		if !existsInLocalModel || (globalToLocal == protocol.Greater || (file.Version.Concurrent(entry.Version) && file.WinsConflict(entry))) || (globalToLocal == protocol.Equal) {
			if file.IsDeleted() {
				if debug {
					l.Debugln("peer", deviceID.Short(), "has deleted file, doing nothing", file.Name)
				}
				continue
			}
			if file.IsInvalid() {
				if debug {
					l.Debugln("peer", deviceID.Short(), "has invalid file, doing nothing", file.Name)
				}
				continue
			}
			if file.IsSymlink() {
				if debug {
					l.Debugln("peer", deviceID.Short(), "has symlink, doing nothing", file.Name)
				}
				continue
			}

			if debug && file.IsDirectory() {
				l.Debugln("add directory", file.Name, "from", deviceID.Short())
			} else if debug {
				l.Debugln("add file", file.Name, "from", deviceID.Short())
			}

			treeCache.AddEntry(file, deviceID)
		}
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
}
