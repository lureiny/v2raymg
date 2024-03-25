package lego

import (
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/urfave/cli/v2"
)

func Before(ctx *cli.Context) error {
	if ctx.String("path") == "" {
		logger.Debug("Could not determine current working directory. Please pass --path.")
	}

	err := createNonExistingFolder(ctx.String("path"))
	if err != nil {
		logger.Error("Could not check/create path: %v", err)
	}

	if ctx.String("server") == "" {
		logger.Debug("Could not determine current working server. Please pass --server.")
	}

	return nil
}
