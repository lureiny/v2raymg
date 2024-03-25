package common

import (
	"sync"

	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type StatsWithMutex struct {
	StatsMap map[string]*proto.Stats
	Ch       chan *proto.Stats
	Mutex    sync.Mutex
}

type collectCallbackFunc func(*StatsWithMutex, *proto.Stats)

func (s *StatsWithMutex) Collect(cb collectCallbackFunc) {
	for stat := range s.Ch {
		cb(s, stat)
	}
}

func CollectCBFunc(s *StatsWithMutex, stat *proto.Stats) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	// 按照 name, type, source, proxy四个维度来聚合, 防止长时间没有采集时堆积
	id := stat.Name + "_" + stat.Type + "_" + stat.Source + "_" + stat.Proxy

	if tmpStat, ok := s.StatsMap[id]; ok {
		tmpStat.Downlink += stat.Downlink
		tmpStat.Uplink += stat.Uplink
	} else {
		s.StatsMap[id] = &proto.Stats{
			Name:     stat.Name,
			Type:     stat.Type,
			Downlink: stat.Downlink,
			Uplink:   stat.Uplink,
			Proxy:    stat.Proxy,
			Source:   stat.Source,
		}
	}
}
