package config

import (
	"os"

	"github.com/syncthing/syncthing/lib/osutil"
	"github.com/syncthing/syncthing/lib/protocol"
)

type Wrapper struct {
	cfg  Configuration
	path string
}

// Wrap wraps an existing Configuration structure and ties it to a file on
// disk.
func Wrap(path string, cfg Configuration) *Wrapper {
	w := &Wrapper{
		cfg:  cfg,
		path: path,
	}
	return w
}

// Load loads an existing file on disk and returns a new configuration
// wrapper.
func Load(path string, myID protocol.DeviceID) (*Wrapper, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	cfg, err := ReadXML(fd, myID)
	if err != nil {
		return nil, err
	}

	return Wrap(path, cfg), nil
}

func (w *Wrapper) ConfigPath() string {
	return w.path
}

// Raw returns the currently wrapped Configuration object.
func (w *Wrapper) Raw() Configuration {
	return w.cfg
}

// Folders returns a map of folders. Folder structures should not be changed,
// other than for the purpose of updating via SetFolder().
func (w *Wrapper) Folders() map[string]FolderConfiguration {
	folderMap := make(map[string]FolderConfiguration, len(w.cfg.Folders))
	for _, fld := range w.cfg.Folders {
		folderMap[fld.ID] = fld
	}
	return folderMap
}

func (w *Wrapper) SetFolder(fldCfg FolderConfiguration) {
	replaced := false
	for i := range w.cfg.Folders {
		if w.cfg.Folders[i].ID == fldCfg.ID {
			w.cfg.Folders[i] = fldCfg
			replaced = true
			break
		}
	}
	if !replaced {
		w.cfg.Folders = append(w.cfg.Folders, fldCfg)
	}
}

// Save writes the configuration to disk
func (w *Wrapper) Save() error {
	fd, err := osutil.CreateAtomic(w.path, 0600)
	if err != nil {
		return err
	}

	if err := w.cfg.WriteXML(fd); err != nil {
		fd.Close()
		return err
	}

	if err := fd.Close(); err != nil {
		return err
	}

	return nil
}
