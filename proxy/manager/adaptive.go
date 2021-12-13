package manager

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/robfig/cron/v3"
)

type Adaptive struct {
	CronRule string
	Ports    map[int64]bool
	Tags     map[string]bool
	Cron     *cron.Cron
	RWMutex  sync.RWMutex
}

type RawAdaptive struct {
	Cron  string        `json:"cron"` // cron rule
	Ports []interface{} `json:"ports"`
	Tags  []string      `json:"tags"`
}

const portRangetSplitSign = "-"

func (a *Adaptive) Init(rawAdaptive *RawAdaptive) error {
	if a.Ports == nil {
		a.Ports = map[int64]bool{}
	}
	a.CronRule = rawAdaptive.Cron
	a.Cron = cron.New()
	for _, tag := range rawAdaptive.Tags {
		a.Tags[tag] = true
	}

	for _, portRange := range rawAdaptive.Ports {
		a.AddPort(portRange)
	}
	// 是为了使后面生成raw adaptive时代码更简洁
	a.Ports[math.MaxInt64] = true
	return nil
}

// port可以是字符串形式的port range, 也可以是字符串形式的单个端口或者整型的端口
func (a *Adaptive) AddPort(port interface{}) error {
	a.RWMutex.Lock()
	defer a.RWMutex.Unlock()
	var err error = nil
	switch port.(type) {
	case string:
		err = parseStringPortRange(&a.Ports, port.(string))
	case float64:
		intPort := int64(port.(float64))
		if intPort < minPort || intPort > maxPort {
			err = fmt.Errorf("port shuold in %d-%d", minPort, maxPort)
			break
		}
		a.Ports[intPort] = true
	}
	return err
}

func (a *Adaptive) AddTag(tag string) {
	a.RWMutex.Lock()
	defer a.RWMutex.Unlock()
	a.Tags[tag] = true
}

func (a *Adaptive) DeletePort(port int64) {
	a.RWMutex.Lock()
	defer a.RWMutex.Unlock()
	delete(a.Ports, port)
}

func (a *Adaptive) DeleteTag(tag string) {
	a.RWMutex.Lock()
	defer a.RWMutex.Unlock()
	delete(a.Tags, tag)
}

func (a *Adaptive) GetTags() []string {
	a.RWMutex.RLock()
	defer a.RWMutex.RUnlock()
	tags := []string{}
	for tag := range a.Tags {
		tags = append(tags, tag)
	}
	return tags
}

// 返回配置文件中格式的字节数组
func (a *Adaptive) Build() *RawAdaptive {
	a.RWMutex.Lock()
	defer a.RWMutex.Unlock()
	rawAdaptive := &RawAdaptive{
		Cron: a.CronRule,
	}
	ports := []int{}
	for port := range a.Ports {
		ports = append(ports, int(port))
	}
	for tag := range a.Tags {
		rawAdaptive.Tags = append(rawAdaptive.Tags, tag)
	}
	sort.Ints(ports)
	if len(ports) > 0 {
		startPort := ports[0]
		endPort := ports[0]
		for index := 1; index < len(ports); index++ {
			if ports[index]-ports[index-1] == 1 {
				endPort = ports[index]
				continue
			}
			if startPort == endPort {
				rawAdaptive.Ports = append(rawAdaptive.Ports, startPort)
			} else {
				rawAdaptive.Ports = append(rawAdaptive.Ports, fmt.Sprintf("%d-%d", startPort, endPort))
			}
			startPort = ports[index]
			endPort = ports[index]
		}
	}
	return rawAdaptive
}

func parseStringPortRange(ports *map[int64]bool, stringPortRange string) error {
	if strings.Contains(stringPortRange, portRangetSplitSign) {
		start := strings.Split(stringPortRange, portRangetSplitSign)[0]
		end := strings.Split(stringPortRange, portRangetSplitSign)[1]
		startPort, err := strconv.ParseInt(start, 10, 64)
		if err != nil {
			return err
		}
		endPort, err := strconv.ParseInt(end, 10, 64)
		if err != nil {
			return err
		}
		// 限定端口范围, 如果只会填写有效的端口
		endPort = int64(math.Min(float64(endPort), maxPort))
		startPort = int64(math.Max(float64(startPort), minPort))
		for ; startPort <= endPort; startPort++ {
			(*ports)[startPort] = true
		}
	} else {
		port, err := strconv.ParseInt(stringPortRange, 10, 64)
		if err != nil {
			return err
		}
		if port > maxPort || port < minPort {
			return fmt.Errorf("port shuold in %d-%d", minPort, maxPort)

		}
		(*ports)[port] = true
	}
	return nil
}
