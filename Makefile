KINDLE ?= /Volumes/Kindle
BIN     = build/ai-dash
TZNAME := $(shell readlink /etc/localtime | sed 's|.*/zoneinfo/||')

.PHONY: kindle mac preview test install clean

# Static ARMv7 hard-float binary for Kindle firmware 5.16.3+.
kindle:
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/ai-dash

mac:
	go build -o build/ai-dash-mac ./cmd/ai-dash

preview: mac
	./build/ai-dash-mac -dir build -out build/preview preview
	cp build/preview/dashboards.gif docs/dashboards.gif

test:
	go vet ./... && go test ./...

# Copies everything onto the Kindle mounted over USB. Keeps an existing config, logins and history.
# macOS still writes ._ metadata files on FAT volumes; they're removed at the end
# (in documents/ they'd show up as junk in the Kindle library).
install: kindle
	@test -d "$(KINDLE)/documents" || (echo "Kindle not mounted at $(KINDLE)"; exit 1)
	mkdir -p "$(KINDLE)/ai-dashboard"
	cp -X $(BIN) kindle/ai-dashboard/start.sh "$(KINDLE)/ai-dashboard/"
	cp -X kindle/documents/ai_dashboard.sh "$(KINDLE)/documents/"
	rm -f "$(KINDLE)/documents/claude_dashboard.sh" "$(KINDLE)/ai-dashboard/claude-dash"
	@if [ ! -f "$(KINDLE)/ai-dashboard/config.json" ] || ! grep -q '"providers"' "$(KINDLE)/ai-dashboard/config.json"; then \
		sed 's|"timezone": "[^"]*"|"timezone": "$(TZNAME)"|' kindle/ai-dashboard/config.example.json > "$(KINDLE)/ai-dashboard/config.json"; \
		echo "wrote config.json (timezone $(TZNAME))"; \
	fi
	rm -f "$(KINDLE)"/ai-dashboard/._* "$(KINDLE)/documents/._ai_dashboard.sh"
	@echo "Installed. Eject the Kindle, then open 'AI Dashboard' from your library."

clean:
	rm -rf build
