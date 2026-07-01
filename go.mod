module github.com/fhltang/ntpskew

go 1.21

require (
	github.com/beevik/ntp v0.3.0
	github.com/prometheus/client_golang v1.17.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/prometheus/client_model v0.14.0 // indirect
	github.com/prometheus/common v0.44.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	golang.org/x/sync v0.4.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
)

replace (
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v1.17.0
	github.com/prometheus/client_model => github.com/prometheus/client_model v0.14.0
	github.com/prometheus/common => github.com/prometheus/common v0.44.0
	github.com/prometheus/procfs => github.com/prometheus/procfs v0.12.0
	golang.org/x/net => github.com/golang/net v0.0.0-20231016165611-36dbd6f18d3e
	golang.org/x/sync => github.com/golang/sync v0.4.0
	golang.org/x/sys => github.com/golang/sys v0.13.0
	google.golang.org/protobuf => github.com/protocolbuffers/protobuf-go v1.31.0
)
