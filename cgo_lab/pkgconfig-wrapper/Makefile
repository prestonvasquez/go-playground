REAL_PREFIX := /opt/homebrew/Cellar/libmongocrypt/1.17.0

demo:
	@echo "--- broken prefix (build should fail) ---"
	-PKG_CONFIG_PATH=./pkg go build . 2>&1 || true
	@echo ""
	@echo "--- wrapper fixes prefix via --define-variable (build should succeed) ---"
	PKG_CONFIG_PATH=./pkg PKG_CONFIG=./wrapper.sh go build .
	@echo "build succeeded"
