all: v2ray xray

v2ray:
	@CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -gcflags "-N -l" -tags v2ray -o bin/v2raymg_v main.go 

xray:
	@CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -gcflags "-N -l" -tags xray -o bin/v2raymg_x main.go 
	
	