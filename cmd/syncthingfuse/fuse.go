package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	_ "bazil.org/fuse/fs/fstestutil"
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
	defer c.Close()

	err = fs.Serve(c, FS{})
	if err != nil {
		log.Fatal(err)
	}

	// check if the mount process has an error to report
	<-c.Ready
	if err := c.MountError; err != nil {
		log.Fatal(err)
	}
}

// FS implements the hello world file system.
type FS struct{}

func (FS) Root() (fs.Node, error) {
    dir := Dir{
        directories: []Dir{},
        files: []File{
            File{
                inode: 2,
                name: "file",
            },
        },
    }
	return dir, nil
}

// Dir implements both Node and Handle for the root directory.
type Dir struct{
    inode uint64
    name string
    directories []Dir
    files []File
}

func (d Dir) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Inode = d.inode
	a.Mode = os.ModeDir | 0555
	return nil
}

func (d Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
    if name == "file" {
        return File{}, nil
    } else if name == "directory" {
        return Dir{}, nil
    }
	fmt.Println("Lookup not implemented for ", name)
	return nil, fuse.ENOENT
}

func (d Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
    dirDirs := make([]fuse.Dirent, 0)
    for _, childDir := range d.directories {
        dirDirs = append(dirDirs, fuse.Dirent{Inode: childDir.inode, Name: childDir.name, Type: fuse.DT_Dir})
    }
    for _, childFile := range d.files {
        dirDirs = append(dirDirs, fuse.Dirent{Inode: childFile.inode, Name: childFile.name, Type: fuse.DT_File})
    }
	return dirDirs, nil
}

/*
func (Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
    files := m.GetFiles("syncthingfusetest")
    for _, file := range files {
        if file.Name == name {
            return File{}, nil
        }
    }
	fmt.Println("Lookup not implemented for ", name)
	return nil, fuse.ENOENT
}

func (Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	dirDirs := make([]fuse.Dirent, 0)
	files := m.GetFiles("syncthingfusetest")
	for _, file := range files {
		dirDirs = append(dirDirs, fuse.Dirent{Inode: 2, Name: file.Name, Type: fuse.DT_File})
	}
	return dirDirs, nil
}
*/

// File implements both Node and Handle for the hello file.
type File struct{
    inode uint64
    name string
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
