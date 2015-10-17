package filetreecache

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"path"

	"github.com/boltdb/bolt"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/protocol"
)

type FileTreeCache struct {
	cfg             *config.Wrapper
	db              *bolt.DB
	folder          string
	folderBucketKey []byte
}

var (
	entriesBucket     = []byte("entries")
	childLookupBucket = []byte("childLookup")
)

func NewFileTreeCache(cfg *config.Wrapper, db *bolt.DB, folder string) *FileTreeCache {
	d := &FileTreeCache{
		cfg:             cfg,
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

		_, err = b.CreateBucketIfNotExists([]byte(childLookupBucket))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}

		return nil
	})

	return d
}

func (d *FileTreeCache) AddEntry(entry protocol.FileInfo) {
	d.db.Update(func(tx *bolt.Tx) error {
		// add entry
		eb := tx.Bucket(d.folderBucketKey).Bucket(entriesBucket)
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		enc.Encode(entry)
		eb.Put([]byte(entry.Name), buf.Bytes()) // TODO handle error?

		// add child lookup
		dir := path.Dir(entry.Name)
		clb := tx.Bucket(d.folderBucketKey).Bucket(childLookupBucket)
		v := clb.Get([]byte(dir))
		if debug {
			l.Debugln("Adding child", entry.Name, "for dir", dir)
		}

		var children []string
		if v == nil {
			children = make([]string, 1)
			children[0] = entry.Name
		} else {
			rbuf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(rbuf)
			dec.Decode(&children)

			children = append(children, entry.Name)
		}

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

func (d *FileTreeCache) RemoveEntry(filepath string) {
	d.db.Update(func(tx *bolt.Tx) error {
		// remove from entries
		eb := tx.Bucket(d.folderBucketKey).Bucket(entriesBucket)
		eb.Delete([]byte(filepath)) // TODO handle error?

		// remove from children lookup
		dir := path.Dir(filepath)
		clb := tx.Bucket(d.folderBucketKey).Bucket(childLookupBucket)
		v := clb.Get([]byte(dir))
		if v != nil {
			var children []string

			rbuf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(rbuf)
			dec.Decode(&children)

			for candidate := 0; candidate < len(children); candidate = candidate + 1 {
				if children[candidate] == filepath {
					indexAndNewLength := len(children) - 1
					children[candidate] = children[indexAndNewLength]
					children = children[:indexAndNewLength]
					break
				}
			}

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
			cbuf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(cbuf)
			dec.Decode(&children)
		}
		return nil
	})

	if debug {
		l.Debugln("Found", len(children), "children for path", path)
	}

	return children
}
