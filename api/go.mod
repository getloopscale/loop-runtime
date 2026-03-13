module github.com/loopzero/loopzero/api

go 1.23

require (
	github.com/go-chi/chi/v5 v5.2.5
	github.com/google/uuid v1.6.0
	github.com/loopzero/loopzero/pkg v0.0.0
)

replace github.com/loopzero/loopzero/pkg => ../pkg
