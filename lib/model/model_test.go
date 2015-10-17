package model

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/boltdb/bolt"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/protocol"
)

func TestModelSingleIndex(t *testing.T) {
	// init
	dir, _ := ioutil.TempDir("", "stf-mt")
	defer os.RemoveAll(dir)
	configFile, _ := ioutil.TempFile(dir, "config")
	deviceID, _ := protocol.DeviceIDFromString("FFR6LPZ-7K4PTTV-UXQSMUU-CPQ5YWH-OEDFIIQ-JUG777G-2YQXXR5-YD6AWQR")
	realCfg := config.New(deviceID)
	cfg := config.Wrap(configFile.Name(), realCfg)
	t.Logf("config path %s\n", configFile.Name())

	databasePath := path.Join(path.Dir(cfg.ConfigPath()), "boltdb")
	database, _ := bolt.Open(databasePath, 0600, nil)

	folder := "syncthingfusetest"
	folderCfg := config.FolderConfiguration{
		ID: folder,
	}
	cfg.SetFolder(folderCfg)

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
	model.Index(deviceID, folder, files, flags, options)

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

	assertEntry(t, model.GetEntry(folder, "file1"), "file1", 0)
	assertEntry(t, model.GetEntry(folder, "file2"), "file2", 0)
	assertEntry(t, model.GetEntry(folder, "dir1"), "dir1", protocol.FlagDirectory)
	assertEntry(t, model.GetEntry(folder, "dir1/dirfile1"), "dir1/dirfile1", 0)
	assertEntry(t, model.GetEntry(folder, "dir1/dirfile2"), "dir1/dirfile2", 0)
}

func TestModelIndexWithRestart(t *testing.T) {
	// init
	dir, _ := ioutil.TempDir("", "stf-mt")
	defer os.RemoveAll(dir)
	configFile, _ := ioutil.TempFile(dir, "config")
	deviceID, _ := protocol.DeviceIDFromString("FFR6LPZ-7K4PTTV-UXQSMUU-CPQ5YWH-OEDFIIQ-JUG777G-2YQXXR5-YD6AWQR")
	realCfg := config.New(deviceID)
	cfg := config.Wrap(configFile.Name(), realCfg)
	t.Logf("config path %s\n", configFile.Name())

	databasePath := path.Join(path.Dir(cfg.ConfigPath()), "boltdb")
	database, _ := bolt.Open(databasePath, 0600, nil)

	folder := "syncthingfusetest"
	folderCfg := config.FolderConfiguration{
		ID: folder,
	}
	cfg.SetFolder(folderCfg)

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

	model.Index(deviceID, folder, files, flags, options)

	// Act (restart db and model)
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

	assertEntry(t, model.GetEntry(folder, "file1"), "file1", 0)
	assertEntry(t, model.GetEntry(folder, "file2"), "file2", 0)
	assertEntry(t, model.GetEntry(folder, "dir1"), "dir1", protocol.FlagDirectory)
	assertEntry(t, model.GetEntry(folder, "dir1/dirfile1"), "dir1/dirfile1", 0)
	assertEntry(t, model.GetEntry(folder, "dir1/dirfile2"), "dir1/dirfile2", 0)
}

func TestModelSingleIndexUpdate(t *testing.T) {
	// init
	dir, _ := ioutil.TempDir("", "stf-mt")
	defer os.RemoveAll(dir)
	configFile, _ := ioutil.TempFile(dir, "config")
	deviceID, _ := protocol.DeviceIDFromString("FFR6LPZ-7K4PTTV-UXQSMUU-CPQ5YWH-OEDFIIQ-JUG777G-2YQXXR5-YD6AWQR")
	realCfg := config.New(deviceID)
	cfg := config.Wrap(configFile.Name(), realCfg)

	databasePath := path.Join(path.Dir(cfg.ConfigPath()), "boltdb")
	database, _ := bolt.Open(databasePath, 0600, nil)

	folder := "syncthingfusetest"
	folderCfg := config.FolderConfiguration{
		ID: folder,
	}
	cfg.SetFolder(folderCfg)

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
	model.Index(deviceID, folder, files, flags, options)

	// Act
	version = protocol.Vector{protocol.Counter{1, 1}}
	files = []protocol.FileInfo{
		protocol.FileInfo{Name: "file2dir", Flags: protocol.FlagDirectory, Version: version},
		protocol.FileInfo{Name: "removedFile", Flags: protocol.FlagDeleted, Version: version},
		protocol.FileInfo{Name: "dir2file", Version: version},
		protocol.FileInfo{Name: "dir2file/file1", Flags: protocol.FlagDeleted, Version: version},
		protocol.FileInfo{Name: "file2symlink", Flags: protocol.FlagSymlink, Version: version},
	}
	model.IndexUpdate(deviceID, folder, files, flags, options)

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

	assertEntry(t, model.GetEntry(folder, "unchangedFile"), "unchangedFile", 0)
	assertEntry(t, model.GetEntry(folder, "file2dir"), "file2dir", protocol.FlagDirectory)
	assertEntry(t, model.GetEntry(folder, "dir1"), "dir1", protocol.FlagDirectory)
	assertEntry(t, model.GetEntry(folder, "dir1/dirfile1"), "dir1/dirfile1", 0)
	assertEntry(t, model.GetEntry(folder, "dir1/dirfile2"), "dir1/dirfile2", 0)
	assertEntry(t, model.GetEntry(folder, "dir2file"), "dir2file", 0)
}

func assertContainsChild(t *testing.T, children []protocol.FileInfo, name string, flags uint32) {
	for _, child := range children {
		if child.Name == name && child.Flags == flags {
			return
		}
	}

	t.Error("Missing file", name)
}

func assertEntry(t *testing.T, entry protocol.FileInfo, name string, flags uint32) {
	if entry.Name == name && entry.Flags == flags {
		return
	}

	t.Error("incorrect entry for file", name)
}
