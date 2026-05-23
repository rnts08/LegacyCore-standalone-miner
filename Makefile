BINARY = legacy-miner
PURE_TAG = legacycoin_experimental_pure_yespower
GPU_DIR = gpu

default: cpu

# Baseline CGO — x86-64 ASM enabled but no -mavx2
cpu:
	go build -o $(BINARY) .

# Enable AVX2 code path in yespower-opt.c (128-bit SIMD, 3-op instrs)
avx2:
	CGO_CFLAGS="-mavx2" go build -o $(BINARY) .

# Target this specific CPU
native:
	CGO_CFLAGS="-march=native" go build -o $(BINARY) .

# Disable x86-64 inline ASM (force pure C path in yespower-opt.c)
purec:
	CGO_CFLAGS="-DNO_X86_64_ASM" go build -o $(BINARY) .

# Pure-Go fallback (no C at all)
pure:
	go build -tags $(PURE_TAG) -o $(BINARY) .

# Static binary (baseline)
static:
	CGO_ENABLED=1 go build -ldflags '-extldflags "-static"' -o $(BINARY)-static .

# ---- GPU targets (require CUDA toolkit or OpenCL runtime) ----

# CUDA: compile bridge .o then build Go with cuda tag
cuda:
	nvcc -arch=sm_61 -O3 -c $(GPU_DIR)/cuda_bridge.cu -o $(GPU_DIR)/cuda_bridge.o
	go build -tags cuda -o $(BINARY) .

# OpenCL: build Go with opencl tag (CGO compiles opencl_bridge.c)
opencl:
	go build -tags opencl -o $(BINARY) .

# Clean GPU build artifacts
gpu-clean:
	rm -f $(GPU_DIR)/cuda_bridge.o

# ---- CPU Detection ----

# Detect CPU features and recommend optimal build flags
detect:
	@echo "=== CPU Detection ==="
	@echo ""
	@echo "Model:"
	@cat /proc/cpuinfo | grep "model name" | head -1 | sed 's/.*: //'
	@echo ""
	@echo "Cores: $$(grep -c ^processor /proc/cpuinfo)"
	@echo ""
	@echo "Features:"
	@FLAGS=$$(cat /proc/cpuinfo | head -64 | grep flags | head -1); \
		echo "  AVX:     $$(echo $$FLAGS | grep -o avx | head -1 || echo 'no')"; \
		echo "  AVX2:    $$(echo $$FLAGS | grep -o avx2 | head -1 || echo 'no')"; \
		echo "  AVX512VL: $$(echo $$FLAGS | grep -o avx512vl | head -1 || echo 'no')"; \
		echo "  SSE4_1:  $$(echo $$FLAGS | grep -o sse4_1 | head -1 || echo 'no')"; \
		echo "  POPCNT:  $$(echo $$FLAGS | grep -o popcnt | head -1 || echo 'no')"; \
		echo ""
	@echo "Recommended build:"
	@FLAGS=$$(cat /proc/cpuinfo | head -64 | grep flags | head -1); \
		if echo $$FLAGS | grep -q avx512vl; then \
			echo "  make native    (or CGO_CFLAGS=\"-mavx512vl\" make)"; \
		elif echo $$FLAGS | grep -q avx2; then \
			echo "  make native    (or make avx2)"; \
		else \
			echo "  make           (baseline)"; \
		fi
	@echo ""
	@echo "To benchmark all variants:"
	@echo "  make bench-cpu bench-avx2 bench-native bench-purec"

# ---- Benchmarks ----

bench-%: B=$(shell echo $@ | sed 's/bench-//')
bench-%:
	$(MAKE) $(B)
	./$(BINARY)

clean:
	rm -f $(BINARY) $(BINARY)-static
	rm -f $(GPU_DIR)/cuda_bridge.o

.PHONY: default cpu avx2 native purec pure static cuda opencl gpu-clean bench-cpu bench-avx2 bench-native clean
