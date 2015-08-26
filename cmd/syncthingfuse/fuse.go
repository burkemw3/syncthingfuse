package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	_ "bazil.org/fuse/fs/fstestutil"
	"github.com/burkemw3/syncthing-fuse/lib/model"
	"github.com/syncthing/protocol"
	"golang.org/x/net/context"
)

var Usage = func() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s MOUNTPOINT\n", os.Args[0])
	flag.PrintDefaults()
}

func MountFuse(mountpoint string) {
	c, err := fuse.Mount(
		mountpoint,
		fuse.FSName("syncthingfuse"),
		fuse.Subtype("syncthingfuse"),
		fuse.LocalVolume(),
		fuse.VolumeName("Syncthing FUSE"),
	)
	if err != nil {
		log.Fatal(err)
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)

	doneServe := make(chan error, 1)
	go func() {
		doneServe <- fs.Serve(c, FS{m: makeModel()}) // TODO use real model
	}()

	select {
	case err := <-doneServe:
		log.Printf("conn.Serve returned %v", err)

		// check if the mount process has an error to report
		<-c.Ready
		if err := c.MountError; err != nil {
			log.Printf("conn.MountError: %v", err)
		}
	case sig := <-sigc:
		log.Printf("Signal %s received, shutting down.", sig)
	}

	time.AfterFunc(3*time.Second, func() {
		os.Exit(1)
	})
	log.Printf("Unmounting...")
	err = Unmount(mountpoint)
	log.Printf("Unmount = %v", err)

	log.Printf("syncthing FUSE process ending.")
}

var (
	folder = "syncthingfusetest"
)

func makeModel() *model.Model {
	m := model.NewModel()

	deviceID := protocol.DeviceID{}
	flags := uint32(0)
	options := []protocol.Option{}

	files := []protocol.FileInfo{
		protocol.FileInfo{Name: "file1"},
		protocol.FileInfo{Name: "file2"},
		protocol.FileInfo{Name: "dir1", Flags: protocol.FlagDirectory},
		protocol.FileInfo{Name: "dir1/dirfile1"},
		protocol.FileInfo{Name: "dir1/dirfile2"},
	}

	m.Index(deviceID, folder, files, flags, options)

	return m
}

type FS struct {
	m *model.Model
}

func (fs FS) Root() (fs.Node, error) {
	log.Printf("Root")
	return Dir{m: fs.m}, nil
}

// Dir implements both Node and Handle for the root directory.
type Dir struct {
	path string
	m    *model.Model
}

func (d Dir) Attr(ctx context.Context, a *fuse.Attr) error {
	log.Printf("Dir Attr")
	a.Mode = os.ModeDir | 0555
	return nil
}

func (d Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	log.Printf("Dir %s Lookup for %s", d.path, name)
	entry := d.m.GetEntry(folder, filepath.Join(d.path, name))

	var node fs.Node
	if entry.IsDirectory() {
		node = Dir{
			path: entry.Name,
			m:    d.m,
		}
	} else {
		node = File{
		// TODO
		// path: entry.Name,
		// m: d.m,
		}
	}

	return node, nil
}

func (d Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	log.Printf("ReadDirAll %s", d.path)

	p := path.Clean(d.path)

	entries := d.m.GetChildren(folder, p)
	result := make([]fuse.Dirent, len(entries))
	for i, entry := range entries {
		eType := fuse.DT_File
		if entry.IsDirectory() {
			eType = fuse.DT_Dir
		}
		result[i] = fuse.Dirent{
			Name: path.Base(entry.Name),
			Type: eType,
		}
	}

	return result, nil
}

// File implements both Node and Handle for the hello file.
type File struct {
	inode uint64
	name  string
}

const greeting = "hello, world\n"

func (f File) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Inode = f.inode
	a.Mode = 0444
	a.Size = uint64(len(greeting))
	return nil
}

func (File) ReadAll(ctx context.Context) ([]byte, error) {
	return []byte(greeting), nil
}

// Unmount attempts to unmount the provided FUSE mount point, forcibly
// if necessary.
func Unmount(point string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("/usr/sbin/diskutil", "umount", "force", point)
	case "linux":
		cmd = exec.Command("fusermount", "-u", point)
	default:
		return errors.New("unmount: unimplemented")
	}

	errc := make(chan error, 1)
	go func() {
		if err := exec.Command("umount", point).Run(); err == nil {
			errc <- err
		}
		// retry to unmount with the fallback cmd
		errc <- cmd.Run()
	}()
	select {
	case <-time.After(1 * time.Second):
		return errors.New("umount timeout")
	case err := <-errc:
		return err
	}
}
