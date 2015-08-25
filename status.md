Can list a file (not sure about folders) from a peer.

Filled with lots of crappy code, for now :(

TODO
----

- long running process
  - serve file contents (rwfolder.go pullerRoutine)
  - cache file contents
  - fix unmounting problem (have to eject in Finder before re-mounting)
  - update Model from peers
  - should probably prevent spotlight indexing (metadata_never_index)
  - would be nice to allow some files to be indexed. maybe we can detect the spotlight process and index conditionally
- CLI
  - manage configuration
- Pin files for offline
- Support writes.

Decisions
---------

- FUSE: likely Bazil FUSE over alternatives like hanwen go-fuse because 
- present state to peers as what we have in the cache
  - actually accurate
  - can respond to fill block Requests from peers correctly
- process crashes before cached data spread to peers