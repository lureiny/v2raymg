package lego

import (
	"github.com/go-acme/lego/v4/log"
	"github.com/urfave/cli/v2"
)

func Before(ctx *cli.Context) error {
	if ctx.String("path") == "" {
		log.Printf("Could not determine current working directory. Please pass --path.")
	}

	err := createNonExistingFolder(ctx.String("path"))
	if err != nil {
		log.Printf("Could not check/create path: %v", err)
	}

	if ctx.String("server") == "" {
		log.Printf("Could not determine current working server. Please pass --server.")
	}

	return nil
}
