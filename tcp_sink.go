package stats

import (
	"bufio"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	logger "github.com/sirupsen/logrus"
)

const (
	logOnEveryNDropped = 1000
)

// NewTCPStatsdSink returns a Sink that is backed by a go channel with a limit of 1000 messages.
func NewTCPStatsdSink() Sink {
	sink := &tcpStatsdSink{
		outc: make(chan string, 1000),
	}
	go sink.run()
	return sink
}

type tcpStatsdSink struct {
	conn            net.Conn
	outc            chan string
	droppedTimers   uint64
	droppedCounters uint64
	droppedGauges   uint64
}

func (s *tcpStatsdSink) FlushCounter(name string, value uint64) {
	select {
	case s.outc <- fmt.Sprintf("%s:%d|c\n", name, value):
	default:
		new := atomic.AddUint64(&s.droppedCounters, 1)
		if new%logOnEveryNDropped == 0 {
			logger.WithField("total_dropped_records", new).
				Error("statsd channel full, discarding counter flush value")
		}
	}

}

func (s *tcpStatsdSink) FlushGauge(name string, value uint64) {
	select {
	case s.outc <- fmt.Sprintf("%s:%d|g\n", name, value):
	default:
		new := atomic.AddUint64(&s.droppedGauges, 1)
		if new%logOnEveryNDropped == 0 {
			logger.WithField("total_dropped_records", new).
				Error("statsd channel full, discarding guage flush value")
		}
	}
}

func (s *tcpStatsdSink) FlushTimer(name string, value float64) {
	select {
	case s.outc <- fmt.Sprintf("%s:%f|ms\n", name, value):
	default:
		new := atomic.AddUint64(&s.droppedTimers, 1)
		if new%logOnEveryNDropped == 0 {
			logger.WithField("total_dropped_records", new).
				Error("statsd channel full, discarding timer flush value")
		}
	}
}

func (s *tcpStatsdSink) run() {
	settings := GetSettings()
	var writer *bufio.Writer
	var err error
	for {
		if s.conn == nil {
			s.conn, err = net.Dial("tcp", fmt.Sprintf("%s:%d", settings.StatsdHost,
				settings.StatsdPort))
			if err != nil {
				logger.Warnf("statsd connection error: %s", err)
				time.Sleep(3 * time.Second)
				continue
			}
			writer = bufio.NewWriter(s.conn)
		}

		// Receive from the channel and check if the channel has been closed
		metric, ok := <-s.outc
		if !ok {
			logger.Warnf("Closing statsd client")
			s.conn.Close()
			return
		}

		writer.WriteString(metric)
		err = writer.Flush()

		if err != nil {
			logger.Warnf("Writing to statsd failed: %s", err)
			_ = s.conn.Close() // Ignore close failures
			s.conn = nil
			writer = nil
		}
	}
}
