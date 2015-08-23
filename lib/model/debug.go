package model

import (
	"os"
	"strings"

	"github.com/calmh/logger"
)

var (
	debug = strings.Contains(os.Getenv("STTRACE"), "model") || os.Getenv("STTRACE") == "all"
	l     = logger.DefaultLogger
)
