package ping

import (
	"math"
	"sync"

	"github.com/lureiny/v2raymg/server/rpc/proto"
)

const (
	maxDelayNum  = 100
	invalidDelay = -1
)

type PingResult struct {
	Geo        string // location
	ISP        string // provider
	delayList  [maxDelayNum]float64
	delayIndex int64

	mutex sync.RWMutex
}

func NewPingResult(geo, isp string) *PingResult {
	return &PingResult{
		Geo:        geo,
		ISP:        isp,
		delayList:  [maxDelayNum]float64{},
		delayIndex: 0,
	}
}

// Update
// delay = -1 mean has not get result
// delay = -2 mean loss
func (r *PingResult) Update(delay float64) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.delayList[r.delayIndex%maxDelayNum] = delay
	r.delayIndex++
}

func (r *PingResult) GetAvg() float64 {
	return r.loopReadData(calculateAvg)
}

func (r *PingResult) GetMax() float64 {
	return r.loopReadData(findMax)
}

func (r *PingResult) GetMin() float64 {
	return r.loopReadData(findMin)
}

func (r *PingResult) GetStDev() float64 {
	return r.loopReadData(calculateStDev)
}

func (r *PingResult) GetLoss() float64 {
	return r.loopReadData(calculateLoss)
}

func (r *PingResult) GetLatestDelay() float64 {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if r.delayIndex <= 0 {
		return invalidDelay
	}
	return r.delayList[(r.delayIndex-1)%maxDelayNum]
}

func (r *PingResult) ConvertToProtoPingResult() *proto.PingResult {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return &proto.PingResult{
		Geo:         r.Geo,
		Isp:         r.ISP,
		MaxDelay:    float32(r.GetMax()),
		MinDelay:    float32(r.GetMin()),
		AvgDelay:    float32(r.GetAvg()),
		StDevDelay:  float32(r.GetStDev()),
		Loss:        float32(r.GetLoss()),
		LatestDelay: float32(r.GetLatestDelay()),
	}
}

func (r *PingResult) Copy() *PingResult {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	newPingResult := NewPingResult(r.Geo, r.ISP)
	newPingResult.delayList = r.delayList
	newPingResult.delayIndex = r.delayIndex
	return newPingResult
}

func (r *PingResult) GetIndex() int64 {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.delayIndex
}

type dataOperation func([]float64) float64

func (r *PingResult) loopReadData(fn dataOperation) float64 {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if r.delayIndex >= maxDelayNum {
		return fn(r.delayList[:])
	} else {
		return fn(r.delayList[:r.delayIndex])
	}
}

func calculateAvg(data []float64) float64 {
	sum := float64(0)
	num := 0
	for _, value := range data {
		if value > 0 {
			sum += value
			num += 1
		}
	}
	return float64(sum) / float64(num)
}

func findMax(data []float64) float64 {
	max := float64(-1)
	for _, value := range data {
		if value > 0 {
			max = math.Max(max, value)
		}
	}
	return max
}

func findMin(data []float64) float64 {
	min := math.MaxFloat64
	for _, value := range data {
		if value > 0 {
			min = math.Min(min, value)
		}
	}
	return min
}

func calculateStDev(data []float64) float64 {
	avg := calculateAvg(data)
	var sumSquaredDiff float64
	num := 0
	for _, value := range data {
		if value > 0 {
			diff := value - avg
			sumSquaredDiff += diff * diff
			num += 1
		}
	}
	return sumSquaredDiff / float64(num)
}

func calculateLoss(data []float64) float64 {
	lossNum := 0
	validNum := 0
	for _, value := range data {
		if value == invalidDelay {
			continue
		}
		if value == -2 {
			lossNum++
		}
		validNum++
	}
	return float64(lossNum) / float64(validNum) * 100
}

type PingChecker interface {
	Init() error
	// ping host
	StartPing(host string) error
	StopPing()
	IsRunning() bool
	GetChan() <-chan *PingResult
}
