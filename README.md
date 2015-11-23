SyncthingFUSE
=============

SyncthingFUSE allows you to see all of the files in shared [Syncthing](https://syncthing.net) folders, but only stores a fixed amount of data locally.

When you open a file, the contents are served from a local cache, if possible. If the contents are not in the cache, then SyncthingFUSE asks peers for the contents and adds them to the cache. If no peers are currently available for the file, then opening the file will fail.

SyncthingFUSE is available on OS X and Linux.

SynthingFUSE is currently read-only. You can browse and view files but cannot write or modify them. (Supporting writes appears possible, but no one has put in the development effort, yet.)

_SyncthingFUSE is currently an alpha release. Since it's currently read-only, it poses a low threat to damaging your computer or Syncthing folders. There is some risk, however, and you assume all of that yourself._

Getting Started
===============

SyncthingFUSE follows many patterns of Syncthing, so you should be familiar with it before starting. Additionally, SyncthingFUSE requires at least one device running running Syncthing.

To get started, grab a [release](https://github.com/burkemw3/syncthingfuse/releases) for your operating system and unpack it. When you run the `syncthingfuse` binary, it will set itself up with some defaults and start.

By default, a configuration UI is available in a browser at `http://127.0.0.1:8385` (If the default port is taken, check the output of the startup for the line `API listening on`). Upon visiting, you will see a UI similar (albeit uglier) to Syncthing. On the left are folders that are configured, and on the right are devices.

Add devices and folders through the UI and restart SyncthingFUSE for the changes to take effect. Folders have a default cache size of 512 MiB, configurable through the UI. You'll also need to add the SyncthingFUSE device to your Syncthing devices.

By default, a mount point called "SyncthingFUSE" will be created in your home directory. After SyncthingFUSE connects to other Syncthing devices, you will be able to browse folder contents through this mount point.

Syncthing Compatibility
=======================

Supports:

- connecting with Syncthing instances, including:
  - local and global discovery
  - relays

Does not support:

- responding to read requests from other peers
- accurate reporting of status: SyncthingFUSE will always appear as 0% synced on Syncthing devices
- symlink files
- UPnP
