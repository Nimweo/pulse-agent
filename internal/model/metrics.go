package model

type CoreSample struct {
	Time     int64   `json:"time"`
	CPU      float64 `json:"cpu"`
	MemUsed  uint64  `json:"mem_used"`
	MemTotal uint64  `json:"mem_total"`
	Load1    float64 `json:"load_1"`
	Load5    float64 `json:"load_5"`
	Load15   float64 `json:"load_15"`
}

type Point struct {
	Time   int64   `json:"time"`
	Metric string  `json:"metric"`
	Device string  `json:"device"`
	Value  float64 `json:"value"`
}

type Payload struct {
	AgentVersion string       `json:"agent_version"`
	Hostname     string       `json:"hostname"`
	Core         []CoreSample `json:"core,omitempty"`
	Points       []Point      `json:"points,omitempty"`
}
