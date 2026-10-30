.PHONY: verify smoke build build-windows docker-build docker-smoke compose-up compose-down

verify:
	bash ./scripts/verify.sh

smoke:
	bash ./scripts/smoke.sh

build:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/omid-iran-bermuda-linux-amd64 ./cmd/server

docker-smoke:
	bash ./scripts/docker-smoke.sh

docker-build:
	docker build -t omid-iran-bermuda:1.0.1 .

compose-up:
	docker compose --env-file .env -f docker-compose.example.yml up -d --build

compose-down:
	docker compose -f docker-compose.example.yml down

build-windows:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o dist/omid-iran-bermuda-windows-amd64.exe ./cmd/server
