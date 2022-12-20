package manager

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

type ProxyClient struct {
	Host string
	Port int

	grpcClientConn *grpc.ClientConn
}

var proxyClient *ProxyClient = nil

func (proxyClient *ProxyClient) GetGrpcClientConn() (*grpc.ClientConn, error) {
	var err error = nil
	if proxyClient.grpcClientConn == nil || proxyClient.grpcClientConn.GetState() != connectivity.Ready {
		addr := fmt.Sprintf("%s:%d", proxyClient.Host, proxyClient.Port)
		proxyClient.grpcClientConn, err = grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	return proxyClient.grpcClientConn, err
}

func GetProxyClient(host string, port int) *ProxyClient {
	if proxyClient == nil {
		proxyClient = &ProxyClient{
			Host: host,
			Port: port,
		}
	}
	return proxyClient
}
