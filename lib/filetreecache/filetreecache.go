package filetreecache

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"path"
	"strings"

	"github.com/boltdb/bolt"
	"github.com/burkemw3/syncthingfuse/lib/config"
	"github.com/syncthing/syncthing/lib/protocol"
)

type FileTreeCache struct {
	fldrCfg         config.FolderConfiguration
	db              *bolt.DB
	folder          string
	folderBucketKey []byte
}

var (
	entriesBucket      = []byte("entries")
	entryDevicesBucket = []byte("entryDevices") // devices that have the current version
	childLookupBucket  = []byte("childLookup")
)

func NewFileTreeCache(fldrCfg config.FolderConfiguration, db *bolt.DB, folder string) *FileTreeCache {
	d := &FileTreeCache{
		fldrCfg:         fldrCfg,
		db:              db,
		folder:          folder,
		folderBucketKey: []byte(folder),
	}

	d.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(d.folderBucketKey)
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}

		_, err = b.CreateBucketIfNotExists([]byte(entriesBucket))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}

		_, err = b.CreateBucketIfNotExists([]byte(entryDevicesBucket))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}

		_, err = b.CreateBucketIfNotExists([]byte(childLookupBucket))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}

		return nil
	})

	d.cleanupForUnsharedDevices()

	return d
}

func (d *FileTreeCache) cleanupForUnsharedDevices() {
	configuredDevices := make(map[string]bool)
	for _, device := range d.fldrCfg.Devices {
		configuredDevices[device.DeviceID.String()] = true
	}

	victims := make([]string, 0)

	d.db.Update(func(tx *bolt.Tx) error {
		edb := tx.Bucket(d.folderBucketKey).Bucket(entryDevicesBucket)
		edb.ForEach(func(key []byte, v []byte) error {
			var devices map[string]bool
			rbuf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(rbuf)
			dec.Decode(&devices)

			changed := false
			for k, _ := range devices {
				if _, ok := configuredDevices[k]; !ok {
					delete(devices, k)
					changed = true
				}
			}

			if 0 == len(devices) {
				victims = append(victims, string(key))
			} else if changed {
				var wbuf bytes.Buffer
				enc := gob.NewEncoder(&wbuf)
				enc.Encode(devices)
				edb.Put(key, wbuf.Bytes())
			}

			return nil
		})
		return nil
	})

	for _, victim := range victims {
		d.RemoveEntry(victim)
	}
}

func (d *FileTreeCache) AddEntry(entry protocol.FileInfo, peer protocol.DeviceID) {
	d.db.Update(func(tx *bolt.Tx) error {
		eb := tx.Bucket(d.folderBucketKey).Bucket(entriesBucket)

		/* save entry */
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		enc.Encode(entry)
		eb.Put([]byte(entry.Name), buf.Bytes()) // TODO handle error?

		/* add peer */
		edb := tx.Bucket(d.folderBucketKey).Bucket(entryDevicesBucket)
		v := edb.Get([]byte(entry.Name))
		var devices map[string]bool
		if v == nil {
			devices = make(map[string]bool)
		} else {
			rbuf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(rbuf)
			dec.Decode(&devices)
		}
		devices[peer.String()] = true
		var dbuf bytes.Buffer
		enc = gob.NewEncoder(&dbuf)
		enc.Encode(devices)
		edb.Put([]byte(entry.Name), dbuf.Bytes())

		/* add child lookup */
		dir := path.Dir(entry.Name)
		clb := tx.Bucket(d.folderBucketKey).Bucket(childLookupBucket)
		v = clb.Get([]byte(dir))
		if debug {
			l.Debugln("Adding child", entry.Name, "for dir", dir)
		}

		var children map[string]bool
		if v == nil {
			children = make(map[string]bool)
		} else {
			rbuf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(rbuf)
			dec.Decode(&children)
		}
		children[entry.Name] = true

		var cbuf bytes.Buffer
		enc = gob.NewEncoder(&cbuf)
		enc.Encode(children)
		clb.Put([]byte(dir), cbuf.Bytes())

		return nil
	})
}

func (d *FileTreeCache) GetEntry(filepath string) (protocol.FileInfo, bool) {
	var entry protocol.FileInfo
	found := false

	d.db.View(func(tx *bolt.Tx) error {
		eb := tx.Bucket(d.folderBucketKey).Bucket(entriesBucket)
		v := eb.Get([]byte(filepath))
		if v == nil {
			return nil
		}
		found = true
		buf := bytes.NewBuffer(v)
		dec := gob.NewDecoder(buf)
		dec.Decode(&entry)
		return nil
	})

	return entry, found
}

func (d *FileTreeCache) GetEntryDevices(filepath string) ([]protocol.DeviceID, bool) {
	var devices []protocol.DeviceID
	found := false

	d.db.View(func(tx *bolt.Tx) error {
		edb := tx.Bucket(d.folderBucketKey).Bucket(entryDevicesBucket)
		d := edb.Get([]byte(filepath))

		if d == nil {
			devices = make([]protocol.DeviceID, 0)
		} else {
			found = true
			var deviceMap map[string]bool
			rbuf := bytes.NewBuffer(d)
			dec := gob.NewDecoder(rbuf)
			dec.Decode(&deviceMap)

			devices = make([]protocol.DeviceID, len(deviceMap))
			i := 0
			for k, _ := range deviceMap {
				devices[i], _ = protocol.DeviceIDFromString(k)
				i += 1
			}
		}

		return nil
	})

	return devices, found
}

func (d *FileTreeCache) RemoveEntry(filepath string) {
	entries := d.GetChildren(filepath)
	for _, childPath := range entries {
		d.RemoveEntry(childPath)
	}

	d.db.Update(func(tx *bolt.Tx) error {
		// remove from entries
		eb := tx.Bucket(d.folderBucketKey).Bucket(entriesBucket)
		eb.Delete([]byte(filepath)) // TODO handle error?

		// remove devices
		db := tx.Bucket(d.folderBucketKey).Bucket(entryDevicesBucket)
		db.Delete([]byte(filepath))

		// remove from children lookup
		dir := path.Dir(filepath)
		clb := tx.Bucket(d.folderBucketKey).Bucket(childLookupBucket)
		v := clb.Get([]byte(dir))
		if v != nil {
			var children map[string]bool
			cbuf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(cbuf)
			dec.Decode(&children)

			delete(children, filepath)

			var wbuf bytes.Buffer
			enc := gob.NewEncoder(&wbuf)
			enc.Encode(children)
			clb.Put([]byte(dir), wbuf.Bytes())
		} else {
			l.Warnln("missing expected parent entry for", filepath)
		}

		return nil
	})
}

func (d *FileTreeCache) GetChildren(path string) []string {
	var children []string
	d.db.View(func(tx *bolt.Tx) error {
		clb := tx.Bucket(d.folderBucketKey).Bucket(childLookupBucket)
		v := clb.Get([]byte(path))

		if v != nil {
			var childrenMap map[string]bool
			cbuf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(cbuf)
			dec.Decode(&childrenMap)

			children = make([]string, len(childrenMap))
			i := 0
			for k, _ := range childrenMap {
				children[i] = k
				i += 1
			}
		}
		return nil
	})

	if debug {
		l.Debugln("Found", len(children), "children for path", path)
	}

	return children
}

func (d *FileTreeCache) GetPathsMatchingPrefix(pathPrefix string) []string {
	result := make([]string, 0)

	prefixBase := path.Base(pathPrefix)
	prefixDir := path.Dir(pathPrefix)

	d.db.View(func(tx *bolt.Tx) error {
		edb := tx.Bucket(d.folderBucketKey).Bucket(entryDevicesBucket)
		edb.ForEach(func(key []byte, v []byte) error {
			if len(result) > 13 {
				return nil
			}

			candidatePath := string(key)
			candidateDir := path.Dir(candidatePath)
			if candidateDir == prefixDir {
				candidateBase := path.Base(candidatePath)
				if strings.HasPrefix(candidateBase, prefixBase) {
					result = append(result, candidatePath)
				}
			}

			return nil
		})
		return nil
	})

	return result
}
