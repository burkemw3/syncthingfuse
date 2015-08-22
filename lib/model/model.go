package model

import (
    "fmt"
	"github.com/syncthing/protocol"
)

type Model struct {
}

// An index was received from the peer device
func (m Model) Index(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo, flags uint32, options []protocol.Option) {
    fmt.Println("model: receiving index from device ", deviceID, " for folder ", folder)

    for _, file := range files {
        fmt.Println("model Index: peer has file ", file.Name)
    }
}

// An index update was received from the peer device
func (m Model) IndexUpdate(deviceID protocol.DeviceID, folder string, files []protocol.FileInfo, flags uint32, options []protocol.Option) {
}

// A request was made by the peer device
func (m Model) Request(deviceID protocol.DeviceID, folder string, name string, offset int64, hash []byte, flags uint32, options []protocol.Option, buf []byte) error {
    return protocol.ErrNoSuchFile
}

// A cluster configuration message was received
func (m Model) ClusterConfig(deviceID protocol.DeviceID, config protocol.ClusterConfigMessage) {
    fmt.Println("model: receiving cluster config from device ", deviceID)
}

// The peer device closed the connection
func (m Model) Close(deviceID protocol.DeviceID, err error) {
}
