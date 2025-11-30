privi: air --build.cmd "go build -o bin/privi ./cmd/privi" --build.bin "bin/privi" --build.include_ext "go" --build.exclude_dir "frontend,node_modules,camera" -- --profile cmd/privi/dev.yaml --port 8080
funnel-privi: go build -o ./bin/funnel ./cmd/funnel; ./bin/funnel 8080 privi
frontend: cd frontend && pnpm start
funnel-frontend: go build -o ./bin/funnel ./cmd/funnel; ./bin/funnel 5173 frontend 
funnel-calendar: go build -o ./bin/funnel ./cmd/funnel; ./bin/funnel 5174 calendar
