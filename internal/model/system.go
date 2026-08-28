package model

type SystemSample struct {
	Time                 int64  `json:"time"`
	UptimeSeconds        uint64 `json:"uptime_seconds"`
	BootTime             int64  `json:"boot_time"`
	Processes            uint64 `json:"processes"`
	OS                   string `json:"os"`
	Platform             string `json:"platform"`
	PlatformFamily       string `json:"platform_family"`
	PlatformVersion      string `json:"platform_version"`
	KernelVersion        string `json:"kernel_version"`
	KernelArchitecture   string `json:"kernel_architecture"`
	VirtualizationSystem string `json:"virtualization_system,omitempty"`
	VirtualizationRole   string `json:"virtualization_role,omitempty"`
	PhysicalCores        int    `json:"physical_cores"`
	LogicalCPUs          int    `json:"logical_cpus"`
}
