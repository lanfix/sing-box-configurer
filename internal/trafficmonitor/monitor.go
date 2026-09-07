package trafficmonitor

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/repository/singboxclashapi"
)

const reconnectDelay = time.Second * 5

// Monitor держит скользящее окно измерений скорости, вычитывая поток /traffic из Clash API sing-box.
type Monitor struct {
	clashAPI *singboxclashapi.ClashAPI
	capacity int

	mu        sync.RWMutex
	samples   []singboxclashapi.TrafficSample
	connected bool
}

// New создаёт монитор со скользящим окном на capacity измерений
// (Clash API отдаёт одно измерение в секунду).
func New(clashAPI *singboxclashapi.ClashAPI, capacity int) *Monitor {
	return &Monitor{
		clashAPI: clashAPI,
		capacity: capacity,
		samples:  make([]singboxclashapi.TrafficSample, 0, capacity),
	}
}

// Start запускает фоновое чтение потока с переподключением при обрыве.
func (m *Monitor) Start(ctx context.Context) {
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}

			err := m.clashAPI.StreamTraffic(ctx, m.push)

			m.setConnected(false)

			if ctx.Err() != nil {
				return
			}

			log.Printf("Traffic stream disconnected: %v; retry in %s", err, reconnectDelay)

			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnectDelay):
			}
		}
	}()
}

func (m *Monitor) push(sample singboxclashapi.TrafficSample) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.connected = true

	if len(m.samples) >= m.capacity {
		copy(m.samples, m.samples[len(m.samples)-m.capacity+1:])
		m.samples = m.samples[:m.capacity-1]
	}

	m.samples = append(m.samples, sample)
}

func (m *Monitor) setConnected(connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.connected = connected
}

// Samples возвращает копию окна измерений от старых к новым.
func (m *Monitor) Samples() []singboxclashapi.TrafficSample {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]singboxclashapi.TrafficSample, len(m.samples))
	copy(out, m.samples)

	return out
}

// Connected сообщает, читается ли поток прямо сейчас.
func (m *Monitor) Connected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.connected
}
