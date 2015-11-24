package model

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	b64 "encoding/base64"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/boltdb/bolt"
	"github.com/burkemw3/syncthingfuse/lib/config"
	"github.com/burkemw3/syncthingfuse/lib/fileblockcache"
	"github.com/burkemw3/syncthingfuse/lib/filetreecache"
	"github.com/cznic/mathutil"
	human "github.com/dustin/go-humanize"
	stmodel "github.com/syncthing/syncthing/lib/model"
	"github.com/syncthing/syncthing/lib/protocol"
	stsync "github.com/syncthing/syncthing/lib/sync"
)

type Model struct {
	cfg         *config.Wrapper
	db          *bolt.DB
	pinnedFiles map[string][]string // read-only after initialization

	blockCaches   map[string]*fileblockcache.FileBlockCache
	treeCaches    map[string]*filetreecache.FileTreeCache
	folderDevices map[string][]protocol.DeviceID
	pulls         map[string]map[string]*blockPullStatus
	fmut          stsync.RWMutex // protects file information. must not be acquired after pmut

	pinnedList list.List
	lmut       *sync.Cond // protects pull list. must not be acquired before fmut, nor after pmut

	protoConn map[protocol.DeviceID]stmodel.Connection
	pmut      stsync.RWMutex // protects protoConn and rawConn. must not be acquired before fmut
}

func NewModel(cfg *config.Wrapper, db *bolt.DB) *Model {
	var lmutex sync.Mutex
	m := &Model{
		cfg:         cfg,
		db:          db,
		pinnedFiles: make(map[string][]string),

		blockCaches:   make(map[string]*fileblockcache.FileBlockCache),
		treeCaches:    make(map[string]*filetreecache.FileTreeCache),
		folderDevices: make(map[string][]protocol.DeviceID),
		pulls:         make(map[string]map[string]*blockPullStatus),
		fmut:          stsync.NewRWMutex(),

		lmut: sync.NewCond(&lmutex),

		protoConn: make(map[protocol.DeviceID]stmodel.Connection),
		pmut:      stsync.NewRWMutex(),
	}

	for _, folderCfg := range m.cfg.Folders() {
		folder := folderCfg.ID

		fbc, err := fileblockcache.NewFileBlockCache(m.cfg, db, folderCfg)
		if err != nil {
			l.Warnln("Skipping folder", folder, "because fileblockcache init failed:", err)
			continue
		}
		m.blockCaches[folder] = fbc
		m.treeCaches[folder] = filetreecache.NewFileTreeCache(folderCfg, db, folder)

		m.folderDevices[folder] = make([]protocol.DeviceID, len(folderCfg.Devices))
		for i, device := range folderCfg.Devices {
			m.folderDevices[folder][i] = device.DeviceID
		}

		m.pulls[folder] = make(map[string]*blockPullStatus)

		m.pinnedFiles[folder] = make([]string, len(folderCfg.PinnedFiles))
		copy(m.pinnedFiles[folder], folderCfg.PinnedFiles)
		sort.Strings(m.pinnedFiles[folder])
		m.unpinUnnecessaryBlocks(folder)
	}

	m.removeUnconfiguredFolders()

	for i := 0; i < 4; i++ {
		go m.backgroundPinnerRoutine()
	}

	return m
}

func (m *Model) unpinUnnecessaryBlocks(folder string) {
	candidates := list.New()
	first, _ := m.treeCaches[folder].GetEntry("")
	candidates.PushBack(first)

	for candidates.Len() > 0 {
		el := candidates.Front()
		candidates.Remove(el)
		entry, _ := el.Value.(protocol.FileInfo)

		if false == m.isFilePinned(folder, entry.Name) {
			for _, block := range entry.Blocks {
				m.blockCaches[folder].UnpinBlock(block.Hash)
			}
		}

		if entry.IsDirectory() {
			children := m.treeCaches[folder].GetChildren(entry.Name)
			for _, child := range children {
				candidates.PushBack(child)
			}
		}
	}
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

	m.fmut.RLock()
	defer m.fmut.RUnlock()
	m.pmut.Lock()
	defer m.pmut.Unlock()

	if _, ok := m.protoConn[deviceID]; ok {
		panic("add existing device")
	}
	m.protoConn[deviceID] = conn

	conn.Start()

	/* build and send cluster config */
	cm := protocol.ClusterConfigMessage{
		DeviceName:    m.cfg.MyDeviceConfiguration().Name,
		ClientName:    "SyncthingFUSE",
		ClientVersion: "0.0.0",
		Options:       []protocol.Option{},
	}

	for folderName, devices := range m.folderDevices {
		found := false
		for _, device := range devices {
			if device == deviceID {
				found = true
				break
			}
		}
		if false == found {
			continue
		}

		cr := protocol.Folder{
			ID: folderName,
		}
		for _, device := range devices {
			// DeviceID is a value type, but with an underlying array. Copy it
			// so we don't grab aliases to the same array later on in device[:]
			device := device
			deviceCfg := m.cfg.Devices()[device]
			cn := protocol.Device{
				ID:          device[:],
				Name:        deviceCfg.Name,
				Addresses:   deviceCfg.Addresses,
				Compression: uint32(deviceCfg.Compression),
				CertName:    deviceCfg.CertName,
				Flags:       protocol.FlagShareTrusted,
			}
			cr.Devices = append(cr.Devices, cn)
		}

		cm.Folders = append(cm.Folders, cr)
	}

	conn.ClusterConfig(cm)
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

func (m *Model) GetPathsMatchingPrefix(folderID string, pathPrefix string) []string {
	m.fmut.RLock()
	defer m.fmut.RUnlock()

	if ftc, ok := m.treeCaches[folderID]; ok {
		return ftc.GetPathsMatchingPrefix(pathPrefix)
	}

	l.Debugln("no tree cache for", folderID)

	return make([]string, 0)
}

func (m *Model) GetEntry(folder string, path string) (protocol.FileInfo, bool) {
	m.fmut.RLock()
	defer m.fmut.RUnlock()

	return m.treeCaches[folder].GetEntry(path)
}

func (m *Model) GetFileData(folder string, filepath string, readStart int64, readSize int) ([]byte, error) {
	start := time.Now()

	m.fmut.Lock()
	if debug {
		flet := time.Now()
		dur := flet.Sub(start).Seconds()
		l.Debugln("Read for", folder, filepath, readStart, readSize, "Lock took", dur)
	}
	m.pmut.RLock()

	entry, found := m.treeCaches[folder].GetEntry(filepath)
	if false == found {
		l.Warnln("File not found", folder, filepath)
		return []byte(""), protocol.ErrNoSuchFile
	}

	data := make([]byte, readSize)
	readEnd := readStart + int64(readSize)
	pendingBlocks := make([]pendingBlockRead, 0)
	fbc := m.blockCaches[folder]

	m.pmut.RLock()
	defer m.pmut.RUnlock()

	// create workers for pulling
	for i, block := range entry.Blocks {
		blockStart := int64(i * protocol.BlockSize)
		blockEnd := blockStart + int64(block.Size)

		if blockEnd > readStart {
			if blockStart < readEnd {
				// need this block
				blockData, found := fbc.GetCachedBlockData(block.Hash)
				if found {
					copyBlockData(blockData, readStart, blockStart, readEnd, blockEnd, data)
				} else {
					// pull block
					pendingBlock := pendingBlockRead{
						readStart:       readStart,
						blockStart:      blockStart,
						readEnd:         readEnd,
						blockEnd:        blockEnd,
						blockPullStatus: m.getOrCreatePullStatus("Fetch", folder, filepath, block, blockStart, assigned),
					}
					pendingBlocks = append(pendingBlocks, pendingBlock)
				}
			} else if blockStart < readEnd+protocol.BlockSize {
				if false == fbc.HasCachedBlockData(block.Hash) && false == fbc.HasPinnedBlock(block.Hash) {
					// prefetch this block
					m.getOrCreatePullStatus("Prefetch", folder, filepath, block, blockStart, assigned)
				}
			}
		}
	}

	m.fmut.Unlock()
	m.pmut.RUnlock()

	// wait for needed blocks
	for _, pendingBlock := range pendingBlocks {
		pendingBlock.blockPullStatus.cv.L.Lock()
		for done != pendingBlock.blockPullStatus.state {
			pendingBlock.blockPullStatus.cv.Wait()
		}
		pendingBlock.blockPullStatus.cv.L.Unlock()
		pendingBlock.blockPullStatus.mutex.RLock()
		if pendingBlock.blockPullStatus.error != nil {
			return []byte(""), pendingBlock.blockPullStatus.error
		}
		copyBlockData(pendingBlock.blockPullStatus.data, pendingBlock.readStart, pendingBlock.blockStart, pendingBlock.readEnd, pendingBlock.blockEnd, data)
		pendingBlock.blockPullStatus.mutex.RUnlock()
	}

	if debug {
		end := time.Now()
		fullDur := end.Sub(start).Seconds()
		l.Debugln("Read for", folder, filepath, readStart, readSize, "completed", fullDur)
	}

	return data, nil
}

func copyBlockData(blockData []byte, readStart int64, blockStart int64, readEnd int64, blockEnd int64, data []byte) {
	for j := mathutil.MaxInt64(readStart, blockStart); j < readEnd && j < blockEnd; j++ {
		outputItr := j - readStart
		inputItr := j - blockStart

		data[outputItr] = blockData[inputItr]
	}
}

type pendingBlockRead struct {
	readStart       int64
	blockStart      int64
	readEnd         int64
	blockEnd        int64
	blockPullStatus *blockPullStatus
}

type blockPullState int

const (
	queued blockPullState = iota
	assigned
	done
)

type blockPullStatus struct {
	comment string
	folder  string
	file    string
	block   protocol.BlockInfo
	offset  int64
	state   blockPullState
	data    []byte
	error   error
	mutex   *sync.RWMutex
	cv      *sync.Cond // protects this data structure. cannot be acquired before any global locks (e.g. fmut)
}

// requires fmut write lock and pmut read lock (or better) before entry
func (m *Model) getOrCreatePullStatus(comment string, folder string, file string, block protocol.BlockInfo, offset int64, state blockPullState) *blockPullStatus {
	hash := b64.URLEncoding.EncodeToString(block.Hash)

	pullStatus, ok := m.pulls[folder][hash]
	if ok {
		return pullStatus
	}

	var mutex sync.RWMutex
	pullStatus = &blockPullStatus{
		comment: comment,
		folder:  folder,
		file:    file,
		block:   block,
		offset:  offset,
		state:   state,
		mutex:   &mutex,
		cv:      sync.NewCond(&mutex),
	}

	m.pulls[folder][hash] = pullStatus

	if assigned == state {
		go m.pullBlock(pullStatus, true)
	}

	return pullStatus
}

func (m *Model) backgroundPinnerRoutine() {
	var status *blockPullStatus

	for {
		m.lmut.L.Lock()
		for 0 == m.pinnedList.Len() {
			m.lmut.Wait()
		}
		el := m.pinnedList.Front()
		m.pinnedList.Remove(el)
		status, _ = el.Value.(*blockPullStatus)
		m.lmut.L.Unlock()

		m.fmut.Lock()
		status.mutex.RLock()
		if m.isBlockStillNeeded(status) {
			if m.blockCaches[status.folder].HasCachedBlockData(status.block.Hash) {
				m.blockCaches[status.folder].PinExistingBlock(status.block)
			} else {
				m.fmut.Unlock()
				status.mutex.RUnlock()

				m.pullBlock(status, false)

				m.fmut.Lock()
				status.mutex.RLock()
				if m.isBlockStillNeeded(status) {
					m.blockCaches[status.folder].PinNewBlock(status.block, status.data)
				}
			}
		}
		m.fmut.Unlock()
		status.mutex.RUnlock()
	}
}

// requires read locks or better on fmut and status.cv.L
func (m *Model) isBlockStillNeeded(status *blockPullStatus) bool {
	entry, found := m.treeCaches[status.folder].GetEntry(status.file)
	if false == found {
		return false
	}

	for i, block := range entry.Blocks {
		blockStart := int64(i * protocol.BlockSize)
		if blockStart == status.offset && bytes.Equal(block.Hash, status.block.Hash) {
			return true
		}
	}

	return false
}

func (m *Model) pullBlock(status *blockPullStatus, addToCache bool) {
	m.fmut.RLock()
	m.pmut.RLock()
	status.cv.L.Lock()

	requestError := errors.New("can't get block from any devices")

	if done != status.state {
		devices, _ := m.treeCaches[status.folder].GetEntryDevices(status.file)
		conns := make([]stmodel.Connection, 0)
		for _, deviceIndex := range rand.Perm(len(devices)) {
			deviceWithFile := devices[deviceIndex]
			if conn, ok := m.protoConn[deviceWithFile]; ok {
				conns = append(conns, conn)
			}
		}
		m.fmut.RUnlock()
		m.pmut.RUnlock()

		if debug {
			l.Debugln(status.comment, "block at offset", status.offset, "size", status.block.Size, "for", status.folder, status.file)
		}

		flags := uint32(0)
		var requestedData []byte

		for _, conn := range conns {
			if debug {
				l.Debugln("Trying to fetch block at offset", status.offset, "for", status.folder, status.file, "from device", conn.ID().String()[:5])
			}

			requestedData, requestError = conn.Request(status.folder, status.file, status.offset, int(status.block.Size), status.block.Hash, flags, []protocol.Option{})
			if requestError == nil {
				// check hash
				actualHash := sha256.Sum256(requestedData)
				if bytes.Equal(actualHash[:], status.block.Hash) {
					break
				} else {
					requestError = errors.New(fmt.Sprint("Hash mismatch expected", status.block.Hash, "received", actualHash))
				}
			}
		}

		status.state = done
		status.error = requestError
		status.data = requestedData

		status.cv.Broadcast()
	} else {
		m.fmut.RUnlock()
		m.pmut.RUnlock()
	}

	status.cv.L.Unlock()

	m.fmut.Lock()
	status.mutex.RLock()
	hash := b64.URLEncoding.EncodeToString(status.block.Hash)
	if requestError == nil && addToCache {
		m.blockCaches[status.folder].AddCachedFileData(status.block, status.data)
	}
	delete(m.pulls[status.folder], hash)
	m.fmut.Unlock()
	status.mutex.RUnlock()
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

// required fmut read (or better) lock before entry
func (m *Model) isFolderSharedWithDevice(folder string, deviceID protocol.DeviceID) bool {
	for _, device := range m.folderDevices[folder] {
		if device.Equals(deviceID) {
			return true
		}
	}
	return false
}

func (m *Model) isFilePinned(folder string, filename string) bool {
	pins := m.pinnedFiles[folder]
	i := sort.SearchStrings(pins, filename)

	return i < len(pins) && pins[i] == filename
}

// An index was received from the peer device
func (m *Model) Index(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo, flags uint32, options []protocol.Option) {
	if debug {
		l.Debugln("model: receiving index from device", deviceID.String()[:5], "for folder", folder)
	}

	m.fmut.Lock()
	defer m.fmut.Unlock()
	m.lmut.L.Lock()
	defer m.lmut.L.Unlock()

	if false == m.isFolderSharedWithDevice(folder, deviceID) {
		if debug {
			l.Debugln("model:", deviceID.String()[:5], "not shared with folder", folder, "so ignoring")
		}
		return
	}

	m.updateIndex(deviceID, folder, files)
}

// An index update was received from the peer device
func (m *Model) IndexUpdate(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo, flags uint32, options []protocol.Option) {
	if debug {
		l.Debugln("model: receiving index update from device", deviceID.String()[:5], "for folder", folder)
	}

	m.fmut.Lock()
	defer m.fmut.Unlock()
	m.lmut.L.Lock()
	defer m.lmut.L.Unlock()

	if false == m.isFolderSharedWithDevice(folder, deviceID) {
		if debug {
			l.Debugln("model:", deviceID.String()[:5], "not shared with folder", folder, "so ignoring")
		}
		return
	}

	m.updateIndex(deviceID, folder, files)
}

// requires write locks on fmut and lmut before entry
func (m *Model) updateIndex(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo) {
	treeCache, ok := m.treeCaches[folder]
	if !ok {
		if debug {
			l.Debugln("folder", folder, "from", deviceID.String()[:5], "tree not configured, skipping")
		}
		return
	}
	fbc, ok := m.blockCaches[folder]
	if !ok {
		if debug {
			l.Debugln("folder", folder, "from", deviceID.String()[:5], "block not configured, skipping")
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
			l.Debugln("updating entry for", file.Name, "from", deviceID.String()[:5], existsInLocalModel, globalToLocal)
		}

		// remove if necessary
		if existsInLocalModel && (globalToLocal == protocol.Greater || (file.Version.Concurrent(entry.Version) && file.WinsConflict(entry))) {
			if debug {
				l.Debugln("remove entry for", file.Name, "from", deviceID.String()[:5])
			}

			treeCache.RemoveEntry(file.Name)

			if m.isFilePinned(folder, file.Name) {
				for _, block := range entry.Blocks {
					fbc.UnpinBlock(block.Hash)
				}
			}
		}

		// add if necessary
		if !existsInLocalModel || (globalToLocal == protocol.Greater || (file.Version.Concurrent(entry.Version) && file.WinsConflict(entry))) || (globalToLocal == protocol.Equal) {
			if file.IsDeleted() {
				if debug {
					l.Debugln("peer", deviceID.String()[:5], "has deleted file, doing nothing", file.Name)
				}
				continue
			}
			if file.IsInvalid() {
				if debug {
					l.Debugln("peer", deviceID.String()[:5], "has invalid file, doing nothing", file.Name)
				}
				continue
			}
			if file.IsSymlink() {
				if debug {
					l.Debugln("peer", deviceID.String()[:5], "has symlink, doing nothing", file.Name)
				}
				continue
			}

			if debug && file.IsDirectory() {
				l.Debugln("add directory", file.Name, "from", deviceID.String()[:5])
			} else if debug {
				l.Debugln("add file", file.Name, "from", deviceID.String()[:5])
			}

			treeCache.AddEntry(file, deviceID)

			// trigger pull on unsatisfied blocks for pinned files
			if m.isFilePinned(folder, file.Name) {
				for i, block := range file.Blocks {
					if false == fbc.HasPinnedBlock(block.Hash) {
						blockStart := int64(i * protocol.BlockSize)
						status := m.getOrCreatePullStatus("Pin fetch", folder, file.Name, block, blockStart, queued)
						m.pinnedList.PushBack(status)
					}
				}
			}
		}
	}

	m.lmut.Broadcast()
}

// A request was made by the peer device
func (m *Model) Request(deviceID protocol.DeviceID, folder string, name string, offset int64, hash []byte, flags uint32, options []protocol.Option, buf []byte) error {
	return protocol.ErrNoSuchFile
}

// A cluster configuration message was received
func (m *Model) ClusterConfig(deviceID protocol.DeviceID, config protocol.ClusterConfigMessage) {
	if debug {
		l.Debugln("model: receiving cluster config from device", deviceID.String()[:5])
	}

	device, ok := m.cfg.Devices()[deviceID]
	if ok && device.Name == "" {
		device.Name = config.DeviceName
		m.cfg.SetDevice(device)
		m.cfg.Save()
	}
}

// The peer device closed the connection
func (m *Model) Close(deviceID protocol.DeviceID, err error) {
	m.pmut.Lock()
	delete(m.protoConn, deviceID)
	m.pmut.Unlock()
}

func (m *Model) GetPinsStatusByFolder() map[string]string {
	result := make(map[string]string)

	m.fmut.RLock()
	defer m.fmut.RUnlock()

	for fldr, files := range m.pinnedFiles {
		pendingBytes := uint64(0)
		pendingFileCount := 0
		pinnedBytes := uint64(0)
		pinnedFileCount := 0
		fbc := m.blockCaches[fldr]
		tc := m.treeCaches[fldr]

		for _, file := range files {
			pending := false

			fileEntry, _ := tc.GetEntry(file)
			for _, block := range fileEntry.Blocks {
				if false == fbc.HasPinnedBlock(block.Hash) {
					pending = true
					pendingBytes += uint64(block.Size)
				} else {
					pinnedBytes += uint64(block.Size)
				}
			}

			if pending {
				pendingFileCount += 1
			} else {
				pinnedFileCount += 1
			}
		}

		if pendingFileCount > 0 {
			pendingByteComment := human.Bytes(pendingBytes)
			fileLabel := "files"
			if pendingFileCount == 1 {
				fileLabel = "file"
			}
			result[fldr] = fmt.Sprintf("%d %s (%s) pending", pendingFileCount, fileLabel, pendingByteComment)
		} else {
			if pinnedFileCount > 0 {
				pinnedByteComment := human.Bytes(pinnedBytes)
				fileLabel := "files"
				if pinnedFileCount == 1 {
					fileLabel = "file"
				}
				result[fldr] = fmt.Sprintf("%d %s (%s) pinned", pinnedFileCount, fileLabel, pinnedByteComment)
			}
		}
	}

	return result
}

type ConnectionInfo struct {
	DeviceID string
	Address  string
}

func (m *Model) GetConnections() []ConnectionInfo {
	m.pmut.RLock()
	defer m.pmut.RUnlock()

	connections := make([]ConnectionInfo, 0)
	for _, conn := range m.protoConn {
		ci := ConnectionInfo{
			DeviceID: conn.ID().String(),
			Address:  conn.RemoteAddr().String(),
		}
		connections = append(connections, ci)
	}

	return connections
}
