module github.com/getloopscale/loopzero/cmd/loopzero-ops

go 1.26.1

require (
	github.com/getloopscale/loopzero/pkg v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/kr/text v0.2.0 // indirect

replace github.com/getloopscale/loopzero/pkg => ../../pkg
