package config

import (
	"encoding/xml"
	"io"
	"os/user"
	"path"
	"reflect"
	"strconv"
	"strings"

	human "github.com/dustin/go-humanize"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/protocol"
)

const (
	CurrentVersion = 0
)

type Configuration struct {
	Version    int                          `xml:"version,attr" json:"version"`
	MyID       string                       `xml:"-" json:"myID"`
	MountPoint string                       `xml:"mountPoint" json:"mountPoint"`
	Folders    []FolderConfiguration        `xml:"folder" json:"folders"`
	Devices    []config.DeviceConfiguration `xml:"device" json:"devices"`
	Options    OptionsConfiguration         `xml:"options" json:"options"`
	GUI        GUIConfiguration             `xml:"gui" json:"gui"`
	XMLName    xml.Name                     `xml:"configuration" json:"-"`
}

type FolderConfiguration struct {
	ID        string                             `xml:"id,attr" json:"id"`
	Devices   []config.FolderDeviceConfiguration `xml:"device" json:"devices"`
	CacheSize string                             `xml:"cacheSize" json:"cacheSize" default:"512MiB"`
}

type GUIConfiguration struct {
	Enabled    bool   `xml:"enabled,attr" json:"enabled" default:"true"`
	RawAddress string `xml:"address" json:"address" default:"127.0.0.1:5833"`
}

func (f FolderConfiguration) GetCacheSizeBytes() (int32, error) {
	bytes, err := human.ParseBytes(f.CacheSize)
	return int32(bytes), err
}

type OptionsConfiguration struct {
	ListenAddress              []string `xml:"listenAddress" json:"listenAddress" default:"tcp://0.0.0.0:22000"`
	LocalAnnounceEnabled       bool     `xml:"localAnnounceEnabled" json:"localAnnounceEnabled" default:"true"`
	LocalAnnouncePort          int      `xml:"localAnnouncePort" json:"localAnnouncePort" default:"21027"`
	LocalAnnounceMCAddr        string   `xml:"localAnnounceMCAddr" json:"localAnnounceMCAddr"`
	GlobalAnnounceEnabled      bool     `xml:"globalAnnounceEnabled" json:"globalAnnounceEnabled" default:"true"`
	GlobalAnnounceServers      []string `xml:"globalAnnounceServer" json:"globalAnnounceServers" default:"default"`
	RelaysEnabled              bool     `xml:"relaysEnabled" json:"relaysEnabled" default:"true"`
	RelayWithoutGlobalAnnounce bool     `xml:"relayWithoutGlobalAnn" json:"relayWithoutGlobalAnn" default:"false"`
	RelayServers               []string `xml:"relayServer" json:"relayServers" default:"dynamic+https://relays.syncthing.net/endpoint"`
	RelayReconnectIntervalM    int      `xml:"relayReconnectIntervalM" json:"relayReconnectIntervalM" default:"10"`
}

func New(myID protocol.DeviceID) Configuration {
	var cfg Configuration
	cfg.Version = CurrentVersion

	cfg.MyID = myID.String()
	setDefaults(&cfg)
	setDefaults(&cfg.GUI)
	setDefaults(&cfg.Options)

	cfg.prepare()

	usr, _ := user.Current()
	cfg.MountPoint = path.Join(usr.HomeDir, "SyncthingFUSE")

	return cfg
}

func ReadXML(r io.Reader, myID protocol.DeviceID) (Configuration, error) {
	var cfg Configuration

	cfg.MyID = myID.String()
	setDefaults(&cfg)
	setDefaults(&cfg.GUI)
	setDefaults(&cfg.Options)

	err := xml.NewDecoder(r).Decode(&cfg)

	cfg.prepare()

	return cfg, err
}

func (cfg *Configuration) WriteXML(w io.Writer) error {
	e := xml.NewEncoder(w)
	e.Indent("", "    ")
	err := e.Encode(cfg)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte("\n"))
	return err
}

func (cfg *Configuration) prepare() {
	fillNilSlices(cfg)
	fillNilSlices(&(cfg.Options))
}

func setDefaults(data interface{}) error {
	s := reflect.ValueOf(data).Elem()
	t := s.Type()

	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		tag := t.Field(i).Tag

		v := tag.Get("default")
		if len(v) > 0 {
			switch f.Interface().(type) {
			case string:
				f.SetString(v)

			case int:
				i, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return err
				}
				f.SetInt(i)

			case float64:
				i, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return err
				}
				f.SetFloat(i)

			case bool:
				f.SetBool(v == "true")

			case []string:
				// We don't do anything with string slices here. Any default
				// we set will be appended to by the XML decoder, so we fill
				// those after decoding.

			default:
				panic(f.Type())
			}
		}
	}
	return nil
}

// fillNilSlices sets default value on slices that are still nil.
func fillNilSlices(data interface{}) error {
	s := reflect.ValueOf(data).Elem()
	t := s.Type()

	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		tag := t.Field(i).Tag

		v := tag.Get("default")
		if len(v) > 0 {
			switch f.Interface().(type) {
			case []string:
				if f.IsNil() {
					// Treat the default as a comma separated slice
					vs := strings.Split(v, ",")
					for i := range vs {
						vs[i] = strings.TrimSpace(vs[i])
					}

					rv := reflect.MakeSlice(reflect.TypeOf([]string{}), len(vs), len(vs))
					for i, v := range vs {
						rv.Index(i).SetString(v)
					}
					f.Set(rv)
				}
			}
		}
	}
	return nil
}
