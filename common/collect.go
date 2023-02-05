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

const statsChSize = 100

var StatsForPrometheus = &StatsWithMutex{
	StatsMap: make(map[string]*proto.Stats),
	Ch:       make(chan *proto.Stats, statsChSize),
}
var SumStats = &StatsWithMutex{
	StatsMap: make(map[string]*proto.Stats),
	Ch:       make(chan *proto.Stats, statsChSize),
}

type collectCallbackFunc func(*StatsWithMutex, *proto.Stats)

func (s *StatsWithMutex) Collect(cb collectCallbackFunc) {
	for stat := range s.Ch {
		cb(s, stat)
	}
}

func commonCollectCBFunc(s *StatsWithMutex, stat *proto.Stats) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	if tmpStat, ok := s.StatsMap[stat.Name+"_"+stat.Type]; ok {
		tmpStat.Downlink += stat.Downlink
		tmpStat.Uplink += stat.Uplink
	} else {
		s.StatsMap[stat.Name+"_"+stat.Type] = &proto.Stats{
			Name:     stat.Name,
			Type:     stat.Type,
			Downlink: stat.Downlink,
			Uplink:   stat.Uplink,
		}
	}
}

func init() {
	go StatsForPrometheus.Collect(commonCollectCBFunc)
	go SumStats.Collect(commonCollectCBFunc)
}
