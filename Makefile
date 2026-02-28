BINARY      := chs-onboard
DIST_DIR    := dist
BUILD_FLAGS := -ldflags="-s -w"

# Set this to your org-issued cert, for example:
# SIGN_IDENTITY="Developer ID Application: Example Corp, Inc. (TEAMID1234)"
SIGN_IDENTITY ?=

ARM64_BIN     := $(DIST_DIR)/$(BINARY)-darwin-arm64
AMD64_BIN     := $(DIST_DIR)/$(BINARY)-darwin-amd64
UNIVERSAL_BIN := $(DIST_DIR)/$(BINARY)-darwin-universal

.PHONY: help build build-arm64 build-amd64 build-universal sign verify release clean

help:
	@echo "Targets:"
	@echo "  make build            - Build universal macOS binary (arm64 + amd64)"
	@echo "  make sign SIGN_IDENTITY=\"<Apple cert>\" - Sign universal binary"
	@echo "  make verify           - Verify code signature and Gatekeeper assessment"
	@echo "  make release SIGN_IDENTITY=\"<Apple cert>\" - Build + sign + verify"
	@echo "  make clean            - Remove build artifacts"

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

build-arm64: $(DIST_DIR)
	GOOS=darwin GOARCH=arm64 go build $(BUILD_FLAGS) -o $(ARM64_BIN) .
	@echo "Built $(ARM64_BIN)"

build-amd64: $(DIST_DIR)
	GOOS=darwin GOARCH=amd64 go build $(BUILD_FLAGS) -o $(AMD64_BIN) .
	@echo "Built $(AMD64_BIN)"

build-universal: build-arm64 build-amd64
	lipo -create -output $(UNIVERSAL_BIN) $(ARM64_BIN) $(AMD64_BIN)
	cp $(UNIVERSAL_BIN) $(BINARY)
	@echo "Built universal binary: $(UNIVERSAL_BIN)"
	@echo "Copied to project root: $(BINARY)"

build: build-universal

sign: build-universal
	@test -n "$(SIGN_IDENTITY)" || (echo "ERROR: SIGN_IDENTITY is required for signing" && exit 1)
	codesign --force --timestamp --options runtime --sign "$(SIGN_IDENTITY)" $(UNIVERSAL_BIN)
	cp $(UNIVERSAL_BIN) $(BINARY)
	@echo "Signed $(UNIVERSAL_BIN)"

verify:
	codesign --verify --deep --strict --verbose=2 $(UNIVERSAL_BIN)
	codesign --display --verbose=2 $(UNIVERSAL_BIN)
	spctl --assess --type execute --verbose=4 $(UNIVERSAL_BIN)
	@echo "Signature verification complete"

release: sign verify

clean:
	rm -rf $(DIST_DIR) $(BINARY)
