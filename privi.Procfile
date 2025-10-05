privi: air --build.cmd "go build -o bin/privi cmd/privi/main.go" --build.bin "bin/privi" 
funnel-privi: go run cmd/funnel/main.go 8080 privi 
frontend: cd frontend && pnpm start
