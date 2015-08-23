Can say HELO to one specific, hard-coded peer and see files in default folder.

Filled with lots of crappy code, for now :(

TODO
----

- long running process
  - build it.
  - do real peer discovery
  - respond to a simple CLI command (e.g. get cluster status) (http://jaytaylor.com/practical-go/#%2846%29, http://golang.org/pkg/net/rpc/)
  - create a Model
  - respond to CLI ls commands
  - respond to CLI fetch commands
  - update Model from peers
  - host FUSE (https://github.com/bazil/fuse/tree/master/examples/hellofs)
- CLI
  - manage configuration
  - handle ls commands
  - handle fetch commands
- Pin files for offline
- Support writes.

Decisions
---------

- FUSE: likely Bazil FUSE over alternatives like hanwen go-fuse because 
- present state to peers as what we have in the cache
  - actually accurate
  - can respond to fill block Requests from peers correctly
- process crashes before cached data spread to peers