v2ray:
	@CGO_ENABLED=0 go build -gcflags "-N -l" -tags v2ray -o bin/v2raymg main.go

xray:
	@CGO_ENABLED=0 go build -gcflags "-N -l" -tags xray -o bin/v2raymg main.go 
