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
	"github.com/burkemw3/syncthingfuse/lib/model"
	"github.com/thejerf/suture"
	"golang.org/x/net/context"
)

var Usage = func() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s MOUNTPOINT\n", os.Args[0])
	flag.PrintDefaults()
}

func MountFuse(mountpoint string, m *model.Model, mainSvc suture.Service) {
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
		l.Infoln("conn.Serve returned", err)

		// check if the mount process has an error to report
		<-c.Ready
		if err := c.MountError; err != nil {
			l.Warnln("conn.MountError:", err)
		}
	case sig := <-sigc:
		l.Infoln("Signal", sig, "received, shutting down.")
	}

	mainSvc.Stop()

	l.Infoln("Unmounting...")
	err = Unmount(mountpoint)
	if err == nil {
		l.Infoln("Unmounted")
	} else {
		l.Infoln("Unmount failed:", err)
	}
}

var (
	debugFuse = strings.Contains(os.Getenv("STTRACE"), "fuse") || os.Getenv("STTRACE") == "all"
)

type FS struct {
	m *model.Model
}

func (fs FS) Root() (fs.Node, error) {
	if debugFuse {
		l.Debugln("Root")
	}
	return STFolder{m: fs.m}, nil
}

type STFolder struct {
	m *model.Model
}

func (stf STFolder) Attr(ctx context.Context, a *fuse.Attr) error {
	if debugFuse {
		l.Debugln("stf Attr")
	}
	a.Mode = os.ModeDir | 0555
	return nil
}

func (stf STFolder) Lookup(ctx context.Context, folderName string) (fs.Node, error) {
	if debugFuse {
		l.Debugln("STF Lookup folder", folderName)
	}

	if stf.m.HasFolder(folderName) {
		return Dir{
			folder: folderName,
			m:      stf.m,
		}, nil
	}

	return Dir{}, fuse.ENOENT
}

func (stf STFolder) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	if debugFuse {
		l.Debugln("ReadDirAll stf")
	}

	entries := stf.m.GetFolders()
	result := make([]fuse.Dirent, len(entries))
	for i, entry := range entries {
		result[i] = fuse.Dirent{
			Name: entry,
			Type: fuse.DT_Dir,
		}
	}

	return result, nil
}

// Dir implements both Node and Handle for the root directory.
type Dir struct {
	path   string
	folder string
	m      *model.Model
}

func (d Dir) Attr(ctx context.Context, a *fuse.Attr) error {
	if debugFuse {
		l.Debugln("Dir Attr folder", d.folder, "path", d.path)
	}

	entry, _ := d.m.GetEntry(d.folder, d.path)

	// TODO assert directory?

	a.Mode = os.ModeDir | 0555
	a.Mtime = time.Unix(entry.ModifiedS, 0)
	return nil
}

func (d Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	if debugFuse {
		l.Debugln("Dir Lookup folder", d.folder, "path", d.path, "for", name)
	}
	entry, found := d.m.GetEntry(d.folder, filepath.Join(d.path, name))

	if false == found {
		return nil, fuse.ENOENT
	}

	var node fs.Node
	if entry.IsDirectory() {
		node = Dir{
			path:   entry.Name,
			folder: d.folder,
			m:      d.m,
		}
	} else {
		node = File{
			path:   entry.Name,
			folder: d.folder,
			m:      d.m,
		}
	}

	return node, nil
}

func (d Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	if debugFuse {
		l.Debugln("ReadDirAll", d.path)
	}

	p := path.Clean(d.path)

	entries := d.m.GetChildren(d.folder, p)
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
	path   string
	folder string
	m      *model.Model
}

func (f File) Attr(ctx context.Context, a *fuse.Attr) error {
	entry, found := f.m.GetEntry(f.folder, f.path)

	// TODO assert file?

	if false == found {
		return fuse.ENOENT
	}

	a.Mode = 0444
	a.Mtime = time.Unix(entry.ModifiedS, 0)
	a.Size = uint64(entry.Size)
	return nil
}

func (f File) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	data, err := f.m.GetFileData(f.folder, f.path, req.Offset, req.Size)

	if err != nil {
		return err
	}

	resp.Data = data

	return err
}

// Unmount attempts to unmount the provided FUSE mount point, forcibly
// if necessary.
func Unmount(point string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("/usr/sbin/diskutil", "umount", "force", point)
	case "linux":
		cmd = exec.Command("fusermount", "-z", "-u", point)
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
	case <-time.After(10 * time.Second):
		return errors.New("umount timeout")
	case err := <-errc:
		return err
	}
}
