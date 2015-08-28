Can list directory contents and show file contents from multiple peers!

Filled with lots of crappy code, for now :(

TODO
----

- FUSE
  - cache file contents (will need to switch to persistent local model)
  - should probably prevent spotlight indexing (metadata_never_index)
  - support Syncthing Folders
  - support symlinks
  - would be nice to allow some files to be indexed. maybe we can detect the spotlight process and index conditionally
  - show status information in special FUSE files
- Figure out releasing, installing, configuring, updating, etc
- CLI
  - manage configuration
- Pin files for offline
- Support writes.
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