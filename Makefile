# PushGate / PushBench.
#
#   make build        build every consumer release into bin/
#   make test         Rust, Go and producer tests
#   make seed         regenerate seed/feature.json and seed/policy.json
#   make fleet        run the 500-instance fleet in the foreground
#   make smoke        start the fleet, push a good and a bad file, check results
#   make demo-week1   the Cloudflare-shaped push against B0 (push-all)
#   make bench        the full benchmark: reproduces bench/results/week7.md (about 2 hours)
#   make bench-long   the slow scenarios: memory leak and flapping producer (about an hour)
#   make bench-quick  the same with one fleet layout (about 25 minutes)
#   make demo         the demo run: two bad files under B0, B2 and B3 (make demo-video renders it)
#   make bench-lkg    the three incidents with the last-known-good library in every consumer

VERSIONS   := 41 42 43 44
PY         := .venv/bin/python
CARGO_DIR  := consumers/rust-proxy

.PHONY: build build-rust build-go tools test seed producer-deps fleet smoke demo-week1 bench bench-quick bench-long bench-incidents bench-lkg demo demo-video clean

build: build-rust build-go tools

build-rust:
	@mkdir -p bin/bot-proxy
	@for v in $(VERSIONS); do \
	  cargo build --quiet --release --manifest-path $(CARGO_DIR)/Cargo.toml --features v$$v \
	    --target-dir $(CARGO_DIR)/target/v$$v && \
	  cp $(CARGO_DIR)/target/v$$v/release/bot-proxy bin/bot-proxy/v$$v || exit 1; \
	  echo "built bin/bot-proxy/v$$v"; \
	done

build-go:
	@mkdir -p bin/go-policy
	@for v in $(VERSIONS); do \
	  go build -ldflags "-X main.buildVersion=$$v" -o bin/go-policy/v$$v ./consumers/go-policy || exit 1; \
	  echo "built bin/go-policy/v$$v"; \
	done

tools:
	go build -o bin/fleet ./fleet/cmd/fleet
	go build -o bin/pgctl ./fleet/cmd/pgctl
	go build -o bin/loadgen ./fleet/cmd/loadgen
	go build -o bin/rollout ./rollout/cmd/rollout
	go build -o bin/pushbench ./bench/cmd/pushbench
	go build -o bin/gate ./gate/cmd/gate
	go build -o bin/goodcorpus ./bench/cmd/goodcorpus
	go build -o bin/report ./bench/cmd/report

producer-deps: $(PY)
$(PY):
	python3 -m venv .venv
	.venv/bin/pip install -q -r producer/requirements.txt

test: producer-deps
	cargo test --quiet --manifest-path $(CARGO_DIR)/Cargo.toml
	cargo test --quiet --manifest-path lib/rust/lkg/Cargo.toml
	go test ./...
	$(PY) -m unittest discover -s producer -p 'test_*.py'

seed: producer-deps
	$(PY) producer/producer.py init --db state/producer.duckdb
	$(PY) producer/producer.py features --db state/producer.duckdb --out seed/feature.json
	$(PY) producer/producer.py policy --out seed/policy.json

fleet: build
	bin/fleet -config deploy/fleet.json -fresh

smoke: build producer-deps
	$(PY) scripts/smoke.py

demo-week1: build producer-deps
	scripts/demo_week1.sh

# The 90-second demo: a live run (about 1 minute), then the video from its
# recording (needs ffmpeg).
demo: build producer-deps
	$(PY) scripts/demo.py

demo-video: demo
	$(PY) -m pip install -q pillow
	$(PY) scripts/render_demo.py -o docs/demo.mp4

# PushBench. `make bench` reproduces bench/results/week7.md in one command:
# the 500-file good-push corpus, every scenario under B0 to B3 on five fleet
# layouts, then tables and charts. About 2 hours on 4 cores. Results go to
# $(RESULTS), so the committed ones are left alone; compare the two, or run
# `make bench RESULTS=bench/results` to replace them.
#
# `make bench-quick` is the same with one fleet layout and 100 good files
# (about 25 minutes, including the build). `make bench-long` runs the slow
# scenarios (memory leak, flapping producer) on three layouts (about an hour).
SEEDS      ?= 1,2,3,4,5
GOOD       ?= 500
STRATEGIES ?= b0,b1,b2,b3
RESULTS    ?= state/results
MEMLIMIT   ?= 64

bench: build producer-deps
	@mkdir -p $(RESULTS)
	bin/goodcorpus -n $(GOOD) -out $(RESULTS)/week7-good.json -md $(RESULTS)/week7-good.md
	bin/pushbench -scale 0.2 -rate 1000 -seeds $(SEEDS) -strategies $(STRATEGIES) -mem-limit-mb $(MEMLIMIT) \
	  -window 25s -bake 5s -out $(RESULTS)/week7.json -md $(RESULTS)/week7-raw.md
	bin/report -title "week 7" -runs $(RESULTS)/week7.json -good $(RESULTS)/week7-good.json \
	  -out $(RESULTS)/week7 -md $(RESULTS)/week7.md
	@echo "Results: $(RESULTS)/week7.md"

bench-long: build producer-deps
	@mkdir -p $(RESULTS)
	bin/pushbench -scale 0.2 -rate 1000 -seeds 1,2,3 -strategies $(STRATEGIES) -mem-limit-mb $(MEMLIMIT) -long \
	  -scenarios memory-leak,flapping-producer -bake 5s -out $(RESULTS)/week7-long.json -md $(RESULTS)/week7-long.md
	@echo "Results: $(RESULTS)/week7-long.md"

bench-quick:
	$(MAKE) bench SEEDS=1 GOOD=100

# Weeks 2 to 4: the three incidents on the full 500-instance fleet.
bench-incidents: build producer-deps
	bin/pushbench -window 45s -bake 10s -scenarios cloudflare-duplicate-rows,crowdstrike-extra-field,google-blank-quota

bench-lkg: build producer-deps
	bin/pushbench -window 45s -bake 10s -on-bad-file keep_last_good -strategies b0,b2,b3 \
	  -scenarios cloudflare-duplicate-rows,crowdstrike-extra-field,google-blank-quota

clean:
	rm -rf bin state
