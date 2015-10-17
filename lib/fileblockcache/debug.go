package fileblockcache

import (
	"os"
	"strings"

	"github.com/calmh/logger"
)

var (
	debug = strings.Contains(os.Getenv("STTRACE"), "fileblockcache") || os.Getenv("STTRACE") == "all"
	l     = logger.DefaultLogger
)
