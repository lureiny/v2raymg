package client

import (
	"time"

	"golang.org/x/net/context"
)

const RpcTimeOut = 1 // 秒

func NewContext() context.Context {
	ctx, _ := context.WithTimeout(context.Background(), RpcTimeOut*time.Second)
	return ctx
}
