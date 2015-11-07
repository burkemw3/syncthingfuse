package config

import (
	"os"
	"strings"

	"github.com/calmh/logger"
)

var (
	debug = strings.Contains(os.Getenv("STTRACE"), "config") || os.Getenv("STTRACE") == "all"
	l     = logger.DefaultLogger
)
