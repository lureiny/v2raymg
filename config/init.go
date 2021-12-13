package config

import (
	"log"
	"os"
)

func init() {
	Error = log.New(os.Stdout, "Error: ", log.LUTC|log.LstdFlags)
	Info = log.New(os.Stdout, "Info: ", log.LUTC|log.LstdFlags)
}
