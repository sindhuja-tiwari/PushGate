# PushGate / PushBench.
#
#   make build        build every consumer release into bin/
#   make test         Rust, Go and producer tests
#   make seed         regenerate seed/feature.json and seed/policy.json
#   make fleet        run the 500-instance fleet in the foreground
#   make smoke        start the fleet, push a good and a bad file, check results
#   make demo-week1   the Cloudflare-shaped push against B0 (push-all)
#   make bench        B0 to B3 on every incident scenario (about 15 minutes)
#   make bench-lkg    the same with the last-known-good library in every consumer

VERSIONS   := 41 42 43 44
PY         := .venv/bin/python
CARGO_DIR  := consumers/rust-proxy

.PHONY: build build-rust build-go tools test seed producer-deps fleet smoke demo-week1 bench bench-lkg bench-week5 clean

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

bench: build producer-deps
	bin/pushbench -window 45s -bake 10s

bench-lkg: build producer-deps
	bin/pushbench -window 45s -bake 10s -on-bad-file keep_last_good -strategies b0,b2,b3

# Week 5: good-push corpus, then every scenario under B0 to B3 on five fleet
# layouts, then tables and charts. About 2 hours on 4 cores.
bench-week5: build producer-deps
	bin/goodcorpus -n 500 -out bench/results/week5-good.json -md bench/results/week5-good.md
	bin/pushbench -scale 0.2 -rate 1000 -seeds 1,2,3,4,5 -strategies b0,b1,b2,b3 \
	  -window 25s -bake 5s -out bench/results/week5.json -md state/bench/week5-raw.md
	bin/report

clean:
	rm -rf bin state
