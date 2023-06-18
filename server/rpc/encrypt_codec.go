package rpc

import (
	"fmt"

	"github.com/lureiny/v2raymg/common/util"
	"google.golang.org/protobuf/proto"
)

type EncryptMessageCodec struct{}

func (e *EncryptMessageCodec) Marshal(v interface{}) ([]byte, error) {
	m := v.(proto.Message)
	byteReq, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}
	return util.EncryptWithAES(byteReq, RpcServerKey)
}

func (e *EncryptMessageCodec) Unmarshal(data []byte, v interface{}) error {
	decryptMessage, err := util.DecryptWithAES(data, RpcServerKey)
	if err != nil {
		return err
	}
	vv, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}
	return proto.Unmarshal(decryptMessage, vv)
}

func (e *EncryptMessageCodec) Name() string {
	return "EncryptMessageCodec"
}
