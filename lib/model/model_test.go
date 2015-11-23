package model

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/boltdb/bolt"
	"github.com/burkemw3/syncthingfuse/lib/config"
	stconfig "github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/protocol"
)

var (
	deviceAlice, _ = protocol.DeviceIDFromString("FFR6LPZ-7K4PTTV-UXQSMUU-CPQ5YWH-OEDFIIQ-JUG777G-2YQXXR5-YD6AWQR")
	deviceBob, _   = protocol.DeviceIDFromString("GYRZZQB-IRNPV4Z-T7TC52W-EQYJ3TT-FDQW6MW-DFLMU42-SSSU6EM-FBK2VAY")
	deviceCarol, _ = protocol.DeviceIDFromString("LGFPDIT-7SKNNJL-VJZA4FC-7QNCRKA-CE753K7-2BW5QDK-2FOZ7FR-FEP57QJ")
)

func TestModelSingleIndex(t *testing.T) {
	// init
	dir, _ := ioutil.TempDir("", "stf-mt")
	defer os.RemoveAll(dir)
	cfg, database, folder := setup(deviceAlice, dir, deviceBob)

	// Arrange
	model := NewModel(cfg, database)
	flags := uint32(0)
	options := []protocol.Option{}

	files := []protocol.FileInfo{
		protocol.FileInfo{Name: "file1"},
		protocol.FileInfo{Name: "file2"},
		protocol.FileInfo{Name: "dir1", Flags: protocol.FlagDirectory},
		protocol.FileInfo{Name: "dir1/dirfile1"},
		protocol.FileInfo{Name: "dir1/dirfile2"},
	}

	// Act
	model.Index(deviceBob, folder, files, flags, options)

	// Assert
	children := model.GetChildren(folder, ".")
	assertContainsChild(t, children, "file2", 0)
	assertContainsChild(t, children, "file2", 0)
	assertContainsChild(t, children, "dir1", protocol.FlagDirectory)
	if len(children) != 3 {
		t.Error("expected 3 children, but got", len(children))
	}

	children = model.GetChildren(folder, "dir1")
	assertContainsChild(t, children, "dir1/dirfile1", 0)
	assertContainsChild(t, children, "dir1/dirfile2", 0)
	if len(children) != 2 {
		t.Error("expected 2 children, but got", len(children))
	}

	assertEntry(t, model, folder, "file1", 0)
	assertEntry(t, model, folder, "file2", 0)
	assertEntry(t, model, folder, "dir1", protocol.FlagDirectory)
	assertEntry(t, model, folder, "dir1/dirfile1", 0)
	assertEntry(t, model, folder, "dir1/dirfile2", 0)
}

func TestIndexFromUnsharedPeerIgnored(t *testing.T) {
	// init
	dir, _ := ioutil.TempDir("", "stf-mt")
	defer os.RemoveAll(dir)
	cfg, database, folder := setup(deviceAlice, dir, deviceBob)

	// Arrange
	model := NewModel(cfg, database)
	flags := uint32(0)
	options := []protocol.Option{}

	files := []protocol.FileInfo{
		protocol.FileInfo{Name: "file1"},
	}

	// Act
	model.Index(deviceCarol, folder, files, flags, options)

	// Assert
	children := model.GetChildren(folder, ".")
	if len(children) != 0 {
		t.Error("expected 0 children, but got", len(children))
	}

	_, found := model.GetEntry(folder, files[0].Name)
	if found {
		t.Error("expected unfound file, but found", files[0].Name)
	}
}

func TestPeerRemovedRestart(t *testing.T) {
	// init
	dir, _ := ioutil.TempDir("", "stf-mt")
	defer os.RemoveAll(dir)
	cfg, database, folder := setup(deviceAlice, dir, deviceBob, deviceCarol)

	// Arrange
	model := NewModel(cfg, database)
	flags := uint32(0)
	options := []protocol.Option{}

	files := []protocol.FileInfo{
		protocol.FileInfo{Name: "file1"},
	}
	model.Index(deviceBob, folder, files, flags, options)

	files = []protocol.FileInfo{
		protocol.FileInfo{Name: "file2"},
	}
	model.Index(deviceCarol, folder, files, flags, options)

	// Act
	cfg.Raw().Folders[0].Devices = []stconfig.FolderDeviceConfiguration{
		stconfig.FolderDeviceConfiguration{DeviceID: deviceCarol},
	}
	model = NewModel(cfg, database)

	// Assert
	children := model.GetChildren(folder, ".")
	assertContainsChild(t, children, "file2", 0)
	if len(children) != 1 {
		t.Error("expected 1 children, but got", len(children))
	}

	assertEntry(t, model, folder, "file2", 0)
}

func TestModelIndexWithRestart(t *testing.T) {
	// init
	dir, _ := ioutil.TempDir("", "stf-mt")
	defer os.RemoveAll(dir)
	cfg, database, folder := setup(deviceAlice, dir, deviceBob)

	// Arrange
	model := NewModel(cfg, database)
	flags := uint32(0)
	options := []protocol.Option{}

	files := []protocol.FileInfo{
		protocol.FileInfo{Name: "file1"},
		protocol.FileInfo{Name: "file2"},
		protocol.FileInfo{Name: "dir1", Flags: protocol.FlagDirectory},
		protocol.FileInfo{Name: "dir1/dirfile1"},
		protocol.FileInfo{Name: "dir1/dirfile2"},
	}

	model.Index(deviceBob, folder, files, flags, options)

	// Act (restart db and model)
	databasePath := database.Path()
	database.Close()
	database, _ = bolt.Open(databasePath, 0600, nil)
	model = NewModel(cfg, database)

	// Assert
	children := model.GetChildren(folder, ".")
	assertContainsChild(t, children, "file2", 0)
	assertContainsChild(t, children, "file2", 0)
	assertContainsChild(t, children, "dir1", protocol.FlagDirectory)
	if len(children) != 3 {
		t.Error("expected 3 children, but got", len(children))
	}

	children = model.GetChildren(folder, "dir1")
	assertContainsChild(t, children, "dir1/dirfile1", 0)
	assertContainsChild(t, children, "dir1/dirfile2", 0)
	if len(children) != 2 {
		t.Error("expected 2 children, but got", len(children))
	}

	assertEntry(t, model, folder, "file1", 0)
	assertEntry(t, model, folder, "file2", 0)
	assertEntry(t, model, folder, "dir1", protocol.FlagDirectory)
	assertEntry(t, model, folder, "dir1/dirfile1", 0)
	assertEntry(t, model, folder, "dir1/dirfile2", 0)
}

func TestModelSingleIndexUpdate(t *testing.T) {
	// init
	dir, _ := ioutil.TempDir("", "stf-mt")
	defer os.RemoveAll(dir)
	cfg, database, folder := setup(deviceAlice, dir, deviceBob)

	// Arrange
	model := NewModel(cfg, database)

	flags := uint32(0)
	options := []protocol.Option{}

	version := protocol.Vector{protocol.Counter{1, 0}}

	files := []protocol.FileInfo{
		protocol.FileInfo{Name: "unchangedFile", Version: version},
		protocol.FileInfo{Name: "file2dir", Version: version},
		protocol.FileInfo{Name: "removedFile", Version: version},
		protocol.FileInfo{Name: "dir1", Flags: protocol.FlagDirectory, Version: version},
		protocol.FileInfo{Name: "dir1/dirfile1", Version: version},
		protocol.FileInfo{Name: "dir1/dirfile2", Version: version},
		protocol.FileInfo{Name: "dir2file", Flags: protocol.FlagDirectory, Version: version},
		protocol.FileInfo{Name: "dir2file/file1", Version: version},
		protocol.FileInfo{Name: "dir2file/file2", Version: version},
		protocol.FileInfo{Name: "file2symlink", Version: version},
	}
	model.Index(deviceBob, folder, files, flags, options)

	// Act
	version = protocol.Vector{protocol.Counter{1, 1}}
	files = []protocol.FileInfo{
		protocol.FileInfo{Name: "file2dir", Flags: protocol.FlagDirectory, Version: version},
		protocol.FileInfo{Name: "removedFile", Flags: protocol.FlagDeleted, Version: version},
		protocol.FileInfo{Name: "dir2file", Version: version},
		protocol.FileInfo{Name: "dir2file/file1", Flags: protocol.FlagDeleted, Version: version},
		protocol.FileInfo{Name: "file2symlink", Flags: protocol.FlagSymlink, Version: version},
	}
	model.IndexUpdate(deviceBob, folder, files, flags, options)

	// Assert
	children := model.GetChildren(folder, ".")
	assertContainsChild(t, children, "unchangedFile", 0)
	assertContainsChild(t, children, "file2dir", protocol.FlagDirectory)
	assertContainsChild(t, children, "dir1", protocol.FlagDirectory)
	assertContainsChild(t, children, "dir2file", 0)
	if len(children) != 4 {
		t.Error("expected 4 children, but got", len(children))
	}

	children = model.GetChildren(folder, "dir1")
	assertContainsChild(t, children, "dir1/dirfile1", 0)
	assertContainsChild(t, children, "dir1/dirfile2", 0)
	if len(children) != 2 {
		t.Error("expected 2 children, but got", len(children))
	}

	assertEntry(t, model, folder, "unchangedFile", 0)
	assertEntry(t, model, folder, "file2dir", protocol.FlagDirectory)
	assertEntry(t, model, folder, "dir1", protocol.FlagDirectory)
	assertEntry(t, model, folder, "dir1/dirfile1", 0)
	assertEntry(t, model, folder, "dir1/dirfile2", 0)
	assertEntry(t, model, folder, "dir2file", 0)
}

func assertContainsChild(t *testing.T, children []protocol.FileInfo, name string, flags uint32) {
	for _, child := range children {
		if child.Name == name && child.Flags == flags {
			return
		}
	}

	t.Error("Missing file", name)
}

func assertEntry(t *testing.T, model *Model, folder string, name string, flags uint32) {
	entry, found := model.GetEntry(folder, name)

	if false == found {
		t.Error("file expected, but not found:", name)
		return
	}

	if entry.Name == name && entry.Flags == flags {
		return
	}

	t.Error("incorrect entry for file", name)
}

func setup(deviceID protocol.DeviceID, dir string, peers ...protocol.DeviceID) (*config.Wrapper, *bolt.DB, string) {
	configFile, _ := ioutil.TempFile(dir, "config")
	realCfg := config.New(deviceID)
	cfg := config.Wrap(configFile.Name(), realCfg)

	databasePath := path.Join(path.Dir(cfg.ConfigPath()), "boltdb")
	database, _ := bolt.Open(databasePath, 0600, nil)

	folder := "syncthingfusetest"
	folderCfg := config.FolderConfiguration{
		ID:        folder,
		CacheSize: "1MiB",
		Devices:   make([]stconfig.FolderDeviceConfiguration, len(peers)),
	}
	for i, peer := range peers {
		folderCfg.Devices[i] = stconfig.FolderDeviceConfiguration{DeviceID: peer}
	}
	cfg.SetFolder(folderCfg)

	return cfg, database, folder
}
