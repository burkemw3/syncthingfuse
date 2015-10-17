package fileblockcache

import (
	"bytes"
	b64 "encoding/base64"
	"encoding/gob"
	"io/ioutil"
	"os"
	"path"

	"github.com/boltdb/bolt"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/protocol"
)

type FileBlockCache struct {
	cfg             *config.Wrapper
	db              *bolt.DB
	folder          string
	folderBucketKey []byte

	maximumBytesStored int32
	currentBytesStored int32
	mostRecentlyUsed   []byte
	leastRecentlyUsed  []byte
}

var cachedFilesBucket = []byte("cachedFiles")

type fileCacheEntry struct {
	Hash     []byte
	Previous []byte
	Next     []byte
	Size     int32
}

func NewFileBlockCache(cfg *config.Wrapper, db *bolt.DB, folder string, maximumCacheBytes int32) *FileBlockCache {
	d := &FileBlockCache{
		cfg:                cfg,
		db:                 db,
		folder:             folder,
		folderBucketKey:    []byte(folder),
		maximumBytesStored: maximumCacheBytes,
	}

	d.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(d.folderBucketKey)
		if err != nil {
			l.Warnln("error creating bucket for folder", folder, err)
			return err
		}
		cfb, err := b.CreateBucketIfNotExists(cachedFilesBucket)
		if err != nil {
			l.Warnln("error creating cached files bucket for folder", folder, err)
			return err
		}
		cfb.ForEach(func(k, v []byte) error {
			buf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(buf)
			var focus fileCacheEntry
			dec.Decode(&focus)

			if focus.Previous == nil {
				d.mostRecentlyUsed = focus.Hash
			}
			if focus.Next == nil {
				d.leastRecentlyUsed = focus.Hash
			}
			d.currentBytesStored += focus.Size

			return nil
		})
		return nil
	})

	diskCacheFolder := path.Join(path.Dir(d.cfg.ConfigPath()), d.folder)
	os.Mkdir(diskCacheFolder, 0744)

	return d
}

func (d *FileBlockCache) GetCachedBlockData(blockHash []byte) ([]byte, bool) {
	found := false
	var current, previous, next fileCacheEntry
	var data []byte

	d.db.Update(func(tx *bolt.Tx) error {
		cfb := tx.Bucket(d.folderBucketKey).Bucket(cachedFilesBucket)

		/* get nodes */
		// current
		current, found = getEntryUnsafely(cfb, blockHash)
		if false == found {
			return nil
		}
		found = true

		// previous
		if current.Previous != nil {
			previous, _ = getEntryUnsafely(cfb, current.Previous)
		}

		// next
		if current.Next != nil {
			next, _ = getEntryUnsafely(cfb, current.Next)
		}

		/* manipulate LRU cache */
		if false == bytes.Equal(blockHash, d.mostRecentlyUsed) {
			if nil == current.Previous {
				l.Warnln("broken LRU. no previous node for", b64.URLEncoding.EncodeToString(blockHash), "but not at MRU either", b64.URLEncoding.EncodeToString(d.mostRecentlyUsed))
			}

			// remove current node
			previous.Next = next.Hash
			setEntryUnsafely(cfb, previous)

			if current.Next != nil {
				next.Previous = previous.Hash
				setEntryUnsafely(cfb, next)
			} else {
				d.leastRecentlyUsed = previous.Hash
			}

			// add current node at front
			oldMru, _ := getEntryUnsafely(cfb, d.mostRecentlyUsed)
			oldMru.Previous = current.Hash
			setEntryUnsafely(cfb, oldMru)

			current.Next = oldMru.Hash
			current.Previous = nil
			setEntryUnsafely(cfb, current)
			d.mostRecentlyUsed = current.Hash
		}

		/* get cached data */
		diskCachePath := getDiskCachePath(d.cfg, d.folder, blockHash)
		data, _ = ioutil.ReadFile(diskCachePath) // TODO check error

		if debug {
			blockHashString := b64.URLEncoding.EncodeToString(blockHash)
			l.Debugln("file cache hit for block", blockHashString, "at", diskCachePath)
		}
		return nil
	})

	if found {
		return data, true
	}

	if debug {
		blockHashString := b64.URLEncoding.EncodeToString(blockHash)
		l.Debugln("file cache miss for block", blockHashString)
	}

	return []byte(""), false
}

func (d *FileBlockCache) AddCachedFileData(block protocol.BlockInfo, data []byte) {
	d.db.Update(func(tx *bolt.Tx) error {
		cfb := tx.Bucket(d.folderBucketKey).Bucket(cachedFilesBucket)

		if debug {
			l.Debugln("Putting block", b64.URLEncoding.EncodeToString(block.Hash), "with", block.Size, "bytes. max bytes", d.maximumBytesStored)
		}

		// make room for new block
		for d.currentBytesStored+block.Size > d.maximumBytesStored && d.leastRecentlyUsed != nil {
			// evict LRU
			victim, _ := getEntryUnsafely(cfb, d.leastRecentlyUsed)
			d.leastRecentlyUsed = victim.Previous

			// remove from db
			cfb.Delete(victim.Hash)

			// remove from disk
			diskCachePath := getDiskCachePath(d.cfg, d.folder, victim.Hash)
			os.Remove(diskCachePath)

			d.currentBytesStored -= victim.Size

			if debug {
				l.Debugln("Evicted", b64.URLEncoding.EncodeToString(victim.Hash), "for", victim.Size, "bytes. currently stored", d.currentBytesStored)
			}
		}

		// add current node to front in db
		current := fileCacheEntry{
			Hash: block.Hash,
			Next: d.mostRecentlyUsed,
			Size: block.Size,
		}
		if d.mostRecentlyUsed != nil {
			oldMru, _ := getEntryUnsafely(cfb, d.mostRecentlyUsed)
			oldMru.Previous = current.Hash
			setEntryUnsafely(cfb, oldMru)
		}
		setEntryUnsafely(cfb, current)
		d.mostRecentlyUsed = current.Hash

		if d.leastRecentlyUsed == nil {
			d.leastRecentlyUsed = current.Hash
		}

		// write block data to disk
		diskCachePath := getDiskCachePath(d.cfg, d.folder, block.Hash)
		ioutil.WriteFile(diskCachePath, data, 0644) // TODO error handle

		d.currentBytesStored += current.Size

		return nil
	})
}

func getDiskCachePath(cfg *config.Wrapper, folder string, blockHash []byte) string {
	blockHashString := b64.URLEncoding.EncodeToString(blockHash)
	return path.Join(path.Dir(cfg.ConfigPath()), folder, blockHashString)
}

func getEntryUnsafely(bucket *bolt.Bucket, blockHash []byte) (fileCacheEntry, bool) {
	v := bucket.Get(blockHash)
	if v == nil {
		// not found, escape!
		return fileCacheEntry{}, false
	}
	buf := bytes.NewBuffer(v)
	dec := gob.NewDecoder(buf)
	var entry fileCacheEntry
	dec.Decode(&entry)
	return entry, true
}

func setEntryUnsafely(bucket *bolt.Bucket, entry fileCacheEntry) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(entry)
	bucket.Put(entry.Hash, buf.Bytes())
}
