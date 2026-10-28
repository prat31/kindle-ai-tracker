KINDLE  ?= /Volumes/Kindle
VERSION := $(shell cat VERSION)
BIN      = build/ai-dash
TZNAME  := $(shell readlink /etc/localtime | sed 's|.*/zoneinfo/||')
LDFLAGS  = -s -w -X main.version=$(VERSION)

.PHONY: kindle mac preview test dist install clean

# Static ARMv7 hard-float binary for Kindle firmware 5.16.3+.
kindle:
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/ai-dash

mac:
	go build -ldflags="-X main.version=$(VERSION)" -o build/ai-dash-mac ./cmd/ai-dash

preview: mac
	./build/ai-dash-mac -dir build -out build/preview preview
	cp build/preview/dashboards.gif docs/dashboards.gif
	cp build/preview/cover.png kindle/cover.png

test:
	go vet ./... && go test ./...

# Release zip (manual / KUAL install) and a KPM repository, in build/.
dist: kindle
	./scripts/package.sh

# Copies everything onto the Kindle mounted over USB. Keeps settings, logins and history.
# macOS still writes ._ metadata files on FAT volumes; they're removed at the end
# (in documents/ they'd show up as junk in the Kindle library).
install: dist
	@test -d "$(KINDLE)/documents" || (echo "Kindle not mounted at $(KINDLE)"; exit 1)
	cp -RX build/dist/root/ "$(KINDLE)/"
	rm -f "$(KINDLE)/documents/claude_dashboard.sh" "$(KINDLE)/ai-dashboard/claude-dash"
	@if [ ! -f "$(KINDLE)/ai-dashboard/config.json" ]; then \
		sed 's|"timezone": "[^"]*"|"timezone": "$(TZNAME)"|' kindle/ai-dashboard/config.example.json > "$(KINDLE)/ai-dashboard/config.json"; \
		echo "wrote config.json (timezone $(TZNAME))"; \
	fi
	find "$(KINDLE)/ai-dashboard" "$(KINDLE)/documents" "$(KINDLE)/extensions/ai-dashboard" -name '._*' -delete 2>/dev/null || true
	@echo "Installed $(VERSION). Eject the Kindle, then open 'AI Dashboard' from your library or KUAL."

clean:
	rm -rf build
