package sshclient

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

type RawMetrics struct {
	CPUTotal    uint64
	CPUIdle     uint64
	MemoryTotal uint64
	MemoryAvail uint64
	NetworkRX   uint64
	NetworkTX   uint64
	Load1       float64
	Uptime      float64
	CollectedAt time.Time
}

func (s *Session) CollectMetrics() (RawMetrics, error) {
	if s == nil || s.Client == nil {
		return RawMetrics{}, errors.New("SSH client is closed")
	}
	probe, err := s.Client.NewSession()
	if err != nil {
		return RawMetrics{}, err
	}
	defer probe.Close()
	const command = `sh -c 'awk '\''/^cpu /{t=0; for(i=2;i<=NF;i++)t+=$i; print t, $5+$6}'\'' /proc/stat; awk '\''/MemTotal:/{t=$2} /MemAvailable:/{a=$2} END{print t, a}'\'' /proc/meminfo; awk -F"[: ]+" '\''NR>2 && $2!="lo"{rx+=$3; tx+=$11} END{print rx+0, tx+0}'\'' /proc/net/dev; awk '\''{print $1}'\'' /proc/loadavg; awk '\''{print $1}'\'' /proc/uptime'`
	out, err := probe.Output(command)
	if err != nil {
		return RawMetrics{}, err
	}
	return parseMetricsOutput(string(out))
}

func parseMetricsOutput(output string) (RawMetrics, error) {
	lines := strings.Fields(output)
	if len(lines) < 8 {
		return RawMetrics{}, errors.New("unsupported metrics response")
	}
	values := make([]float64, 8)
	for i := range values {
		value, err := strconv.ParseFloat(lines[i], 64)
		if err != nil {
			return RawMetrics{}, errors.New("invalid metrics response")
		}
		values[i] = value
	}
	return RawMetrics{CPUTotal: uint64(values[0]), CPUIdle: uint64(values[1]), MemoryTotal: uint64(values[2]) * 1024, MemoryAvail: uint64(values[3]) * 1024, NetworkRX: uint64(values[4]), NetworkTX: uint64(values[5]), Load1: values[6], Uptime: values[7], CollectedAt: time.Now()}, nil
}
