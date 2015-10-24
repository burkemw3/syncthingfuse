package config

import (
	stconfig "github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/protocol"
)

func (w *Wrapper) AsStCfg(myID protocol.DeviceID) *stconfig.Wrapper {
	cfg := stconfig.New(myID)

	cfg.Folders = make([]stconfig.FolderConfiguration, len(w.Raw().Folders))
	for i, fldr := range w.Raw().Folders {
		cfg.Folders[i].ID = fldr.ID
		cfg.Folders[i].Devices = make([]stconfig.FolderDeviceConfiguration, len(fldr.Devices))
		copy(cfg.Folders[i].Devices, fldr.Devices)
	}

	cfg.Devices = w.Raw().Devices
	cfg.Options.ListenAddress = w.Raw().Options.ListenAddress

	return stconfig.Wrap("/shouldnotexist", cfg)
}
