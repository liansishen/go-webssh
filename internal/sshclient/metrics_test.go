package sshclient

import "testing"

func TestParseMetricsOutput(t *testing.T) {
	m, err := parseMetricsOutput("1000 250\n8000000 2000000\n1000000000 400000000\n12345 67890\n0.75\n86461.5\n")
	if err != nil {
		t.Fatal(err)
	}
	if m.CPUTotal != 1000 || m.CPUIdle != 250 || m.MemoryTotal != 8000000*1024 || m.DiskTotal != 1000000000*1024 || m.DiskUsed != 400000000*1024 || m.NetworkTX != 67890 || m.Load1 != .75 {
		t.Fatalf("metrics=%+v", m)
	}
}

func TestParseMetricsOutputRejectsInvalid(t *testing.T) {
	if _, err := parseMetricsOutput("not linux"); err == nil {
		t.Fatal("expected error")
	}
}
