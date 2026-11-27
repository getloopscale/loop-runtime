module github.com/getloopscale/loop-runtime/infra/scripts

go 1.26.1

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/getloopscale/loop-runtime/pkg v0.0.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/getloopscale/loop-runtime/pkg => ../../pkg
