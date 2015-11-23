A special FUSE client for Syncthing

- doesn't try to stay in full sync, just cache files locally when they are read
- read-only currently

Filled with lots of crappy code, for now :(

TODO
----

- manage configuration (web GUI?)
  - handle errors
- show if restart needed in UI
- restart from UI/API
- move todo to github issues
- support linux
- Figure out releasing, installing, configuring, updating, etc
  - test on linux
  - base on recent ST Prime code
- show connection status in gui
- undo actions in UI
- FUSE
  - should probably prevent spotlight indexing with metadata_never_index. (spotlight might not work anyway https://github.com/osxfuse/osxfuse/wiki/FAQ#46-can-i-enable-spotlight-on-a-fuse-for-os-x-file-system)
  - support symlinks
  - would be nice to allow some files to be indexed. maybe we can detect the spotlight process and index conditionally
  - show status information in special FUSE files
- track cache statistics
- prefetch some data: get 1st block from each file, then 2nd, etc
- switch to LRU-2Q file cache
- Pin files for offline
- Support writes.
- upnp?
- manage Unified Buffer Cache
  - OSX caches files! probably good, hard to update correctly
  - https://github.com/osxfuse/osxfuse/wiki/SSHFS#frequently-asked-questions
  - http://wagerlabs.com/blog/2008/03/04/hacking-the-mac-osx-unified-buffer-cache/

Decisions
---------

- FUSE: likely Bazil FUSE over alternatives like hanwen go-fuse because 
- present state to peers as what we have in the cache
  - actually accurate
  - can respond to fill block Requests from peers correctly
- process crashes before cached data spread to peers