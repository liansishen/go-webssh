package ws

import (
	"encoding/json"
	"testing"
)

func TestEncodeDecodeMessage(t *testing.T) {
	b, err := EncodeMessage("output", "hello")
	if err != nil {
		t.Fatal(err)
	}
	m, err := DecodeMessage(b)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "output" {
		t.Fatalf("type=%s", m.Type)
	}
	var s string
	if err := json.Unmarshal(m.Data, &s); err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Fatalf("data=%q", s)
	}
}

func TestConnectDataRoundTrip(t *testing.T) {
	cd := ConnectData{Host: "h", Port: 22, Username: "u", PrivateKey: "k", Cols: 80, Rows: 24, UseHerdr: true, HerdrSession: "gowebssh-test"}
	b, err := EncodeMessage("connect", cd)
	if err != nil {
		t.Fatal(err)
	}
	m, err := DecodeMessage(b)
	if err != nil {
		t.Fatal(err)
	}
	var out ConnectData
	if err := json.Unmarshal(m.Data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Host != "h" || out.Port != 22 || out.Username != "u" || !out.UseHerdr || out.HerdrSession != "gowebssh-test" {
		t.Fatalf("%+v", out)
	}
}
