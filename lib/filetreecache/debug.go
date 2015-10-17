package filetreecache

import (
	"os"
	"strings"

	"github.com/calmh/logger"
)

var (
	debug = strings.Contains(os.Getenv("STTRACE"), "filetreecache") || os.Getenv("STTRACE") == "all"
	l     = logger.DefaultLogger
)
