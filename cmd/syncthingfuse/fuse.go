package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strings"
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

func MountFuse(mountpoint string, m *model.Model) {
	c, err := fuse.Mount(
		mountpoint,
		fuse.FSName("syncthingfuse"),
		fuse.Subtype("syncthingfuse"),
		fuse.LocalVolume(),
		fuse.VolumeName("Syncthing FUSE"),
	)
	if err != nil {
		l.Warnln(err)
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)

	doneServe := make(chan error, 1)
	go func() {
		doneServe <- fs.Serve(c, FS{m: m})
	}()

	select {
	case err := <-doneServe:
		l.Infoln("conn.Serve returned %v", err)

		// check if the mount process has an error to report
		<-c.Ready
		if err := c.MountError; err != nil {
			l.Warnln("conn.MountError: %v", err)
		}
	case sig := <-sigc:
		l.Infoln("Signal %s received, shutting down.", sig)
	}

	time.AfterFunc(3*time.Second, func() {
		os.Exit(1)
	})
	l.Infoln("Unmounting...")
	err = Unmount(mountpoint)
	l.Infoln("Unmount = %v", err)

	l.Infoln("syncthing FUSE process ending.")
}

var (
	folder    = "syncthingfusetest"
	debugFuse = strings.Contains(os.Getenv("STTRACE"), "fuse") || os.Getenv("STTRACE") == "all"
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
	if debugFuse {
		l.Debugln("Root")
	}
	return Dir{m: fs.m}, nil
}

// Dir implements both Node and Handle for the root directory.
type Dir struct {
	path string
	m    *model.Model
}

func (d Dir) Attr(ctx context.Context, a *fuse.Attr) error {
	if debugFuse {
		l.Debugln("Dir Attr")
	}
	a.Mode = os.ModeDir | 0555
	return nil
}

func (d Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	if debugFuse {
		l.Debugln("Dir %s Lookup for %s", d.path, name)
	}
	entry := d.m.GetEntry(folder, filepath.Join(d.path, name))

	var node fs.Node
	if entry.IsDirectory() {
		node = Dir{
			path: entry.Name,
			m:    d.m,
		}
	} else {
		node = File{
			path: entry.Name,
			m:    d.m,
		}
	}

	return node, nil
}

func (d Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	if debugFuse {
		l.Debugln("ReadDirAll %s", d.path)
	}

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
	path string
	m    *model.Model
}

func (f File) Attr(ctx context.Context, a *fuse.Attr) error {
	entry := f.m.GetEntry(folder, f.path)

	a.Mode = 0444
	a.Mtime = time.Now()
	a.Size = uint64(entry.Size())
	return nil
}

func (f File) ReadAll(ctx context.Context) ([]byte, error) {
	data, err := f.m.GetFileData(folder, f.path)

	return data, err
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
