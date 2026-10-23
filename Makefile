# Hanko API URL from the Hanko console. Required by `make run`.
HANKO_API_URL ?=
# The user ID `make dev` pretends is signed in. The default holds the role in
# server/demo.toml; any other value shows the 403.
DEV_SUBJECT ?= 00000000-0000-0000-0000-000000000001
ADDR ?= localhost:8080

# Pass the endpoint to Hugo only when it is set, so the placeholder in
# site/config/_default/params.toml applies otherwise.
HUGO_ENV := $(if $(HANKO_API_URL),HUGO_PARAMS_MODULES_HANKO_ENDPOINT=$(HANKO_API_URL))

.PHONY: help build site server run dev test check-imports clean

help:
	@echo "make dev    Build everything and run with a pretend user, no Hanko needed"
	@echo "make run    Build everything and run against Hanko (needs HANKO_API_URL)"
	@echo "make build  Build the site into server/dist, then the binary into bin/"
	@echo "make test   Run the server tests"
	@echo "make check-imports  Check the site's module list against Hinode's"
	@echo "make clean  Remove the build output"

build: site server

site/node_modules: site/package.json site/package-lock.json
	cd site && npm ci --no-audit --no-fund
	@touch site/node_modules

# The site has to exist before the server compiles: go:embed reads
# server/dist at build time, not at run time.
site: site/node_modules
	find server/dist -mindepth 1 ! -name placeholder.txt -delete
	cd site && $(HUGO_ENV) npm run -s build

server:
	cd server && go build -o ../bin/demo-server .

run:
	@test -n "$(HANKO_API_URL)" || { echo "Set HANKO_API_URL, e.g. make run HANKO_API_URL=https://<id>.hanko.io"; exit 1; }
	$(MAKE) build
	HANKO_API_URL=$(HANKO_API_URL) ./bin/demo-server --config server/demo.toml --addr $(ADDR)

dev: build
	./bin/demo-server --config server/demo.toml --addr $(ADDR) --dev-subject $(DEV_SUBJECT)

test:
	cd server && go test ./...

check-imports: site/node_modules
	cd site && npm run -s check:imports

clean:
	find server/dist -mindepth 1 ! -name placeholder.txt -delete
	rm -rf bin
