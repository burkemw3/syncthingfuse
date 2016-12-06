package config

import (
	"os"

	stconfig "github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/osutil"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/sync"
)

type Wrapper struct {
	cfg  Configuration
	path string
	mut  sync.Mutex
}

// Wrap wraps an existing Configuration structure and ties it to a file on
// disk.
func Wrap(path string, cfg Configuration) *Wrapper {
	w := &Wrapper{
		cfg:  cfg,
		path: path,
		mut:  sync.NewMutex(),
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

func (w *Wrapper) Devices() map[protocol.DeviceID]stconfig.DeviceConfiguration {
	w.mut.Lock()
	defer w.mut.Unlock()

	deviceMap := make(map[protocol.DeviceID]stconfig.DeviceConfiguration, len(w.cfg.Devices))
	for _, devCfg := range w.cfg.Devices {
		deviceMap[devCfg.DeviceID] = devCfg
	}

	return deviceMap
}

func (w *Wrapper) SetDevice(devCfg stconfig.DeviceConfiguration) {
	w.mut.Lock()
	defer w.mut.Unlock()

	replaced := false
	for i := range w.cfg.Devices {
		if w.cfg.Devices[i].DeviceID == devCfg.DeviceID {
			w.cfg.Devices[i] = devCfg
			replaced = true
			break
		}
	}
	if !replaced {
		w.cfg.Devices = append(w.cfg.Devices, devCfg)
	}
}

func (w *Wrapper) MyDeviceConfiguration() stconfig.DeviceConfiguration {
	for _, d := range w.cfg.Devices {
		if d.DeviceID.String() == w.cfg.MyID {
			return d
		}
	}
	return stconfig.DeviceConfiguration{}
}

// Folders returns a map of folders. Folder structures should not be changed,
// other than for the purpose of updating via SetFolder().
func (w *Wrapper) Folders() map[string]FolderConfiguration {
	w.mut.Lock()
	defer w.mut.Unlock()

	folderMap := make(map[string]FolderConfiguration, len(w.cfg.Folders))
	for _, fld := range w.cfg.Folders {
		folderMap[fld.ID] = fld
	}
	return folderMap
}

func (w *Wrapper) SetFolder(fldCfg FolderConfiguration) {
	w.mut.Lock()
	defer w.mut.Unlock()

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

func (w *Wrapper) Replace(to Configuration) error {
	w.mut.Lock()
	defer w.mut.Unlock()

	// validate
	for _, fldrCfg := range to.Folders {
		if _, err := fldrCfg.GetCacheSizeBytes(); err != nil {
			l.Debugln("rejected config, cannot parse cache size:", err)
			return err
		}
	}

	// set
	w.cfg = to

	return nil
}

// Save writes the configuration to disk
func (w *Wrapper) Save() error {
	fd, err := osutil.CreateAtomic(w.path)
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
