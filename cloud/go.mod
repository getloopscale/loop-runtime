module github.com/loopzero/loopzero/cloud

go 1.25.0

require (
	github.com/go-chi/chi/v5 v5.2.5
	github.com/golang-jwt/jwt/v5 v5.2.2
)

require github.com/lib/pq v1.12.3 // indirect

replace github.com/loopzero/loopzero/pkg => ../pkg
