package bound

import (
	"context"

	"github.com/v2fly/v2ray-core/v4/app/proxyman/command"
	"github.com/v2fly/v2ray-core/v4/common/protocol"
	"github.com/v2fly/v2ray-core/v4/common/serial"
)

func AddUser(con command.HandlerServiceClient, user *User) error {
	_, err := con.AlterInbound(context.Background(), &command.AlterInboundRequest{
		Tag: user.InBoundTag,
		Operation: serial.ToTypedMessage(&command.AddUserOperation{
			User: &protocol.User{
				Level:   user.Level,
				Email:   user.Email,
				Account: serial.ToTypedMessage(user.Account),
			},
		}),
	})
	return err
}

func RemoveUser(con command.HandlerServiceClient, user *User) error {
	_, err := con.AlterInbound(context.Background(), &command.AlterInboundRequest{
		Tag: user.InBoundTag,
		Operation: serial.ToTypedMessage(&command.RemoveUserOperation{
			Email: user.Email,
		}),
	})
	return err
}
