package statsd

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/rcrowley/go-metrics"
)

// StatsDConfig provides a container with
// configuration parameters for the StatsD exporter
type StatsDConfig struct {
	Addr            *net.UDPAddr     // Network address to connect to
	Registry        metrics.Registry // Registry to be exported
	FlushInterval   time.Duration    // Flush interval
	DurationUnit    time.Duration    // Time conversion unit for durations
	Prefix          string           // Prefix to be prepended to metric names
	Percentiles     []float64        // Percentiles to export from timers and histograms
	lastTimerValues map[string]int64 // cache of last values reported in the timer
}

// StatsD is a blocking exporter function which reports metrics in r
// to a statsd server located at addr, flushing them every d duration
// and prepending metric names with prefix.
func StatsD(r metrics.Registry, d time.Duration, prefix string, addr *net.UDPAddr) {
	StatsDWithConfig(StatsDConfig{
		Addr:            addr,
		Registry:        r,
		FlushInterval:   d,
		DurationUnit:    time.Nanosecond,
		Prefix:          prefix,
		Percentiles:     []float64{0.5, 0.75, 0.95, 0.99, 0.999},
		lastTimerValues: make(map[string]int64),
	})
}

// StatsDWithConfig is a blocking exporter function just like StatsD,
// but it takes a StatsDConfig instead.
func StatsDWithConfig(c StatsDConfig) {
	for _ = range time.Tick(c.FlushInterval) {
		if err := statsd(&c); nil != err {
			log.Println(err)
		}
	}
}

func statsd(c *StatsDConfig) error {
	du := float64(c.DurationUnit)

	conn, err := net.DialUDP("udp", nil, c.Addr)

	if nil != err {
		return err
	}

	// this will be executed when statsd func returns
	defer conn.Close()

	// constuct a buffer to write statsd wire format
	w := bufio.NewWriter(conn)

	// for each metric in the registry format into statsd wireformat and send
	c.Registry.Each(func(name string, metric interface{}) {
		if c.Prefix != "" {
			name = fmt.Sprintf("%s.%s", c.Prefix, name)
		}
		switch m := metric.(type) {
		case metrics.Counter:
			fmt.Fprintf(w, "%s.count:%d|c\n", name, m.Count())
			m.Clear()
		case metrics.Gauge:
			fmt.Fprintf(w, "%s.value:%d|g\n", name, m.Value())
		case metrics.GaugeFloat64:
			fmt.Fprintf(w, "%s.value:%f|g\n", name, m.Value())
		case metrics.Histogram:
			h := m.Snapshot()
			ps := h.Percentiles(c.Percentiles)
			fmt.Fprintf(w, "%s.count:%d|c\n", name, h.Count())
			fmt.Fprintf(w, "%s.min:%d|g\n", name, h.Min())
			fmt.Fprintf(w, "%s.max:%d|g\n", name, h.Max())
			fmt.Fprintf(w, "%s.mean:%.2f|g\n", name, h.Mean())
			fmt.Fprintf(w, "%s.std-dev:%.2f|g\n", name, h.StdDev())
			for psIdx, psKey := range c.Percentiles {
				key := strings.Replace(strconv.FormatFloat(psKey*100.0, 'f', -1, 64), ".", "", 1)
				fmt.Fprintf(w, "%s.%s-percentile:%.2f|g\n", name, key, ps[psIdx])
			}
			m.Clear()
		case metrics.Meter:
			ss := m.Snapshot()
			fmt.Fprintf(w, "%s.count:%d|c\n", name, ss.Count())
			fmt.Fprintf(w, "%s.one-minute:%.2f|g\n", name, ss.Rate1())
			fmt.Fprintf(w, "%s.five-minute:%.2f|g\n", name, ss.Rate5())
			fmt.Fprintf(w, "%s.fifteen-minute:%.2f|g\n", name, ss.Rate15())
			fmt.Fprintf(w, "%s.mean:%.2f|g\n", name, ss.RateMean())
		case metrics.Timer:
			t := m.Snapshot()
			ps := t.Percentiles(c.Percentiles)

			last := c.lastTimerValues[name]
			fmt.Fprintf(w, "%s.count:%d|c\n", name, t.Count()-last)
			c.lastTimerValues[name] = t.Count()
			fmt.Fprintf(w, "%s.min:%d|g\n", name, t.Min()/int64(du))
			fmt.Fprintf(w, "%s.max:%d|g\n", name, t.Max()/int64(du))
			fmt.Fprintf(w, "%s.mean:%.2f|g\n", name, t.Mean()/du)
			fmt.Fprintf(w, "%s.std-dev:%.2f|g\n", name, t.StdDev()/du)
			for psIdx, psKey := range c.Percentiles {
				key := strings.Replace(strconv.FormatFloat(psKey*100.0, 'f', -1, 64), ".", "", 1)
				fmt.Fprintf(w, "%s.%s-percentile:%.2f|g\n", name, key, ps[psIdx]/du)
			}
			fmt.Fprintf(w, "%s.one-minute:%.2f|g\n", name, t.Rate1())
			fmt.Fprintf(w, "%s.five-minute:%.2f|g\n", name, t.Rate5())
			fmt.Fprintf(w, "%s.fifteen-minute:%.2f|g\n", name, t.Rate15())
			fmt.Fprintf(w, "%s.mean-rate:%.2f|g\n", name, t.RateMean())
		default:
			log.Println("[WARN] No Metric", name, reflect.TypeOf(m))
		}
		w.Flush()
	})

	return nil
}
