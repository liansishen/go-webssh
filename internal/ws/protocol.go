package ws

import "encoding/json"

type Message struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type ConnectData struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	PrivateKey    string `json:"privateKey"`
	Passphrase    string `json:"passphrase"`
	Term          string `json:"term"`
	Cols          int    `json:"cols"`
	Rows          int    `json:"rows"`
	UseHerdr      bool   `json:"useHerdr"`
	LegacyUseTmux bool   `json:"useTmux"`
	HerdrSession  string `json:"herdrSession,omitempty"`
	TmuxSession   string `json:"tmuxSession,omitempty"`
}

type TunnelConnectData struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type TunnelConnectedData struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type ConnectedData struct {
	SessionID    string `json:"sessionId"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	HerdrSession string `json:"herdrSession,omitempty"`
	TmuxSession  string `json:"tmuxSession,omitempty"`
}

type ResizeData struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type ErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ClosedData struct {
	Reason     string `json:"reason"`
	ExitStatus *int   `json:"exitStatus,omitempty"`
}

type MetricsData struct {
	CPUPercent      float64 `json:"cpuPercent"`
	MemoryUsed      uint64  `json:"memoryUsed"`
	MemoryTotal     uint64  `json:"memoryTotal"`
	MemoryPercent   float64 `json:"memoryPercent"`
	DiskUsed        uint64  `json:"diskUsed"`
	DiskTotal       uint64  `json:"diskTotal"`
	DiskPercent     float64 `json:"diskPercent"`
	NetworkRXBytes  uint64  `json:"networkRxBytes"`
	NetworkTXBytes  uint64  `json:"networkTxBytes"`
	NetworkRXPerSec float64 `json:"networkRxPerSec"`
	NetworkTXPerSec float64 `json:"networkTxPerSec"`
	Load1           float64 `json:"load1"`
	UptimeSeconds   float64 `json:"uptimeSeconds"`
}

func EncodeMessage(typ string, data any) ([]byte, error) {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return json.Marshal(Message{Type: typ, Data: raw})
}

func DecodeMessage(b []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(b, &m); err != nil {
		return Message{}, err
	}
	return m, nil
}
