package model

const PayloadSchemaVersion = 1

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
	SchemaVersion int           `json:"schema_version"`
	BatchID       string        `json:"batch_id"`
	SentAt        int64         `json:"sent_at"`
	AgentVersion  string        `json:"agent_version"`
	Hostname      string        `json:"hostname"`
	System        *SystemSample `json:"system,omitempty"`
	Core          []CoreSample  `json:"core"`
	Points        []Point       `json:"points"`
}
